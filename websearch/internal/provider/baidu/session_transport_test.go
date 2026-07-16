package baidu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/headerprofile"
)

type immediateSessionPacer struct {
	mu       sync.Mutex
	previous []time.Time
}

func (pacer *immediateSessionPacer) Wait(ctx context.Context, previous time.Time, _ time.Duration) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	pacer.mu.Lock()
	pacer.previous = append(pacer.previous, previous)
	pacer.mu.Unlock()
	return 0, nil
}

func fixedTestProfile() headerprofile.Profile {
	return headerprofile.Profile{
		Name: "fixed-primary", UserAgent: "fixed-agent", AcceptLanguage: "zh-CN,zh;q=0.9",
		Headers: map[string]string{"Upgrade-Insecure-Requests": "1"},
	}
}

func fixedSessionConfig(serverURL string) SessionConfig {
	return SessionConfig{
		BootstrapURL: serverURL + "/", SearchURL: serverURL + "/s",
		RequestTimeout: time.Second, MinInterval: time.Millisecond, MaxJitter: 0,
		CaptchaCooldown: 30 * time.Minute, RateLimitCooldown: 5 * time.Minute,
		FallbackReserve: 0, MaxBodyBytes: 1 << 20,
	}
}

func normalBaiduHTML(query string) string {
	return fmt.Sprintf(`<html><head><title>%s_百度搜索</title></head><body><div id="content_left"><div class="result c-container" mu="https://go.dev/"><h3><a href="https://go.dev/">Go</a></h3></div></div></body></html>`, query)
}

func TestSessionTransportBootstrapsCookieAndKeepsFixedHeaders(t *testing.T) {
	var bootstrapCalls atomic.Int32
	var searchCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != "fixed-agent" || request.Header.Get("Accept-Language") != "zh-CN,zh;q=0.9" {
			t.Errorf("unexpected fixed headers: %#v", request.Header)
		}
		if request.URL.Path == "/" {
			bootstrapCalls.Add(1)
			http.SetCookie(writer, &http.Cookie{Name: "BAIDUID", Value: "session-one", Path: "/"})
			_, _ = io.WriteString(writer, `<html><head><title>百度一下</title></head><body></body></html>`)
			return
		}
		searchCalls.Add(1)
		cookie, err := request.Cookie("BAIDUID")
		if err != nil || cookie.Value != "session-one" {
			t.Errorf("search cookie=%v err=%v", cookie, err)
		}
		if request.Referer() != server.URL+"/" {
			t.Errorf("referer=%q", request.Referer())
		}
		_, _ = io.WriteString(writer, normalBaiduHTML(request.URL.Query().Get("wd")))
	}))
	defer server.Close()

	pacer := &immediateSessionPacer{}
	transportValue, err := NewSessionTransport(
		fixedSessionConfig(server.URL), fixedTestProfile(),
		NewHTTPSessionClientFactory(server.Client().Transport), pacer, BaiduResponseClassifier{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"go", "gin"} {
		response, fetchErr := transportValue.Fetch(context.Background(), domain.SearchRequest{Query: query, Limit: 10, Page: 1})
		if fetchErr != nil {
			t.Fatal(fetchErr)
		}
		if response.Classification != domain.ClassificationNormal || response.SessionState != domain.BaiduSessionStateWarm || response.SessionGeneration != 1 {
			t.Fatalf("response=%+v", response)
		}
	}
	if bootstrapCalls.Load() != 1 || searchCalls.Load() != 2 {
		t.Fatalf("bootstrap=%d search=%d", bootstrapCalls.Load(), searchCalls.Load())
	}
	if len(pacer.previous) != 3 || !pacer.previous[0].IsZero() || pacer.previous[1].IsZero() || pacer.previous[2].IsZero() {
		t.Fatalf("pacer previous=%v", pacer.previous)
	}
}

func TestHTTPSessionClientFactoryCreatesIndependentCookieJars(t *testing.T) {
	factory := NewHTTPSessionClientFactory(http.DefaultTransport)
	first, err := factory.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := factory.New()
	if err != nil {
		t.Fatal(err)
	}
	urlValue, _ := url.Parse("https://www.baidu.com/")
	first.Jar.SetCookies(urlValue, []*http.Cookie{{Name: "BAIDUID", Value: "first"}})
	if cookies := second.Jar.Cookies(urlValue); len(cookies) != 0 {
		t.Fatalf("cookie leaked: %v", cookies)
	}
	second.Jar.SetCookies(urlValue, []*http.Cookie{{Name: "BAIDUID", Value: "second"}})
	if got := first.Jar.Cookies(urlValue)[0].Value; got != "first" {
		t.Fatalf("first jar mutated: %s", got)
	}
}

func TestSessionTransportSerializesConcurrentSearches(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" {
			_, _ = io.WriteString(writer, `<title>百度一下</title>`)
			return
		}
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		_, _ = io.WriteString(writer, normalBaiduHTML(request.URL.Query().Get("wd")))
	}))
	defer server.Close()

	transportValue, err := NewSessionTransport(
		fixedSessionConfig(server.URL), fixedTestProfile(),
		NewHTTPSessionClientFactory(server.Client().Transport), &immediateSessionPacer{}, BaiduResponseClassifier{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, 2)
	for _, query := range []string{"go", "gin"} {
		waitGroup.Add(1)
		go func(value string) {
			defer waitGroup.Done()
			_, fetchErr := transportValue.Fetch(context.Background(), domain.SearchRequest{Query: value, Limit: 10, Page: 1})
			errorsChannel <- fetchErr
		}(query)
	}
	waitGroup.Wait()
	close(errorsChannel)
	for fetchErr := range errorsChannel {
		if fetchErr != nil {
			t.Fatal(fetchErr)
		}
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent Baidu searches=%d", maximum.Load())
	}
}

func TestSessionTransportCoolsAndRebuildsAfterCaptcha(t *testing.T) {
	now := time.Date(2026, 7, 14, 15, 0, 0, 0, time.UTC)
	var bootstrapCalls atomic.Int32
	var searchCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" {
			generation := bootstrapCalls.Add(1)
			http.SetCookie(writer, &http.Cookie{Name: "BAIDUID", Value: fmt.Sprintf("session-%d", generation), Path: "/"})
			_, _ = io.WriteString(writer, `<title>百度一下</title>`)
			return
		}
		attempt := searchCalls.Add(1)
		if attempt == 1 {
			_, _ = io.WriteString(writer, `<html><head><title>百度安全验证</title></head><body><form id="verify-form">请输入验证码</form></body></html>`)
			return
		}
		_, _ = io.WriteString(writer, normalBaiduHTML(request.URL.Query().Get("wd")))
	}))
	defer server.Close()

	transportValue, err := NewSessionTransport(
		fixedSessionConfig(server.URL), fixedTestProfile(),
		NewHTTPSessionClientFactory(server.Client().Transport), &immediateSessionPacer{}, BaiduResponseClassifier{}, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	first, firstErr := transportValue.Fetch(context.Background(), domain.SearchRequest{Query: "go", Limit: 10, Page: 1})
	var sessionErr *SessionError
	if !errors.As(firstErr, &sessionErr) || sessionErr.Classification != domain.ClassificationCaptcha {
		t.Fatalf("response=%+v err=%v", first, firstErr)
	}
	if first.SessionState != domain.BaiduSessionStateCooling || first.BlockedUntil.IsZero() {
		t.Fatalf("first response=%+v", first)
	}

	second, secondErr := transportValue.Fetch(context.Background(), domain.SearchRequest{Query: "gin", Limit: 10, Page: 1})
	if !errors.As(secondErr, &sessionErr) || sessionErr.Classification != domain.ClassificationCaptcha {
		t.Fatalf("second response=%+v err=%v", second, secondErr)
	}
	if bootstrapCalls.Load() != 1 || searchCalls.Load() != 1 {
		t.Fatalf("cooling accessed upstream: bootstrap=%d search=%d", bootstrapCalls.Load(), searchCalls.Load())
	}

	now = now.Add(31 * time.Minute)
	third, thirdErr := transportValue.Fetch(context.Background(), domain.SearchRequest{Query: "context", Limit: 10, Page: 1})
	if thirdErr != nil {
		t.Fatal(thirdErr)
	}
	if third.SessionGeneration != 2 || third.SessionState != domain.BaiduSessionStateWarm {
		t.Fatalf("third response=%+v", third)
	}
	if bootstrapCalls.Load() != 2 || searchCalls.Load() != 2 {
		t.Fatalf("rebuild calls: bootstrap=%d search=%d", bootstrapCalls.Load(), searchCalls.Load())
	}
}

func TestFixedSessionPacerRejectsInsufficientDeadlineAndWaits(t *testing.T) {
	pacer, err := newFixedSessionPacer(20*time.Millisecond, 0, time.Now, func(int64) (int64, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	shortContext, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, waitErr := pacer.Wait(shortContext, time.Now(), 10*time.Millisecond); !errors.Is(waitErr, context.DeadlineExceeded) {
		t.Fatalf("deadline error=%v", waitErr)
	}

	started := time.Now()
	waited, waitErr := pacer.Wait(context.Background(), time.Now(), 0)
	if waitErr != nil {
		t.Fatal(waitErr)
	}
	if waited < 15*time.Millisecond || time.Since(started) < 15*time.Millisecond {
		t.Fatalf("waited=%s elapsed=%s", waited, time.Since(started))
	}
}

func TestSessionTransportDelegatesPaginationWithoutAccessingUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("fixed session must not access upstream for page > 1")
	}))
	defer server.Close()
	transportValue, err := NewSessionTransport(
		fixedSessionConfig(server.URL), fixedTestProfile(),
		NewHTTPSessionClientFactory(server.Client().Transport), &immediateSessionPacer{}, BaiduResponseClassifier{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, fetchErr := transportValue.Fetch(context.Background(), domain.SearchRequest{Query: "go", Limit: 5, Page: 2})
	var sessionErr *SessionError
	if !errors.As(fetchErr, &sessionErr) || sessionErr.Classification != domain.ClassificationParseChanged || response.Classification != domain.ClassificationParseChanged {
		t.Fatalf("response=%+v err=%v", response, fetchErr)
	}
}
