package chromebrowser

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"web-search-backend/websearch/internal/domain"
	"web-search-backend/websearch/internal/headerprofile"
)

func TestNewRejectsMissingProfileDir(t *testing.T) {
	if _, err := New(Config{Timeout: 10 * time.Second}, testURLBuilder); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("err=%v", err)
	}
}

func TestClientUsesInjectedURLBuilder(t *testing.T) {
	builder := func(request domain.SearchRequest) (string, error) {
		return "https://www.bing.com/search?q=" + url.QueryEscape(request.Query), nil
	}
	client, err := New(Config{
		ProfileDir: t.TempDir(),
		Timeout:    10 * time.Second,
		Headless:   true,
	}, builder)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	got, err := client.buildURL(domain.SearchRequest{Query: "go language", Limit: 10, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://www.bing.com/search?q=go+language" {
		t.Fatalf("url=%s", got)
	}
}

func TestNewCreatesReusableClientWithoutLaunchingChrome(t *testing.T) {
	client, err := New(Config{
		ProfileDir: t.TempDir(),
		Timeout:    10 * time.Second,
		Headless:   true,
	}, testURLBuilder)
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
}

func TestNewUsesConfiguredTabCapacity(t *testing.T) {
	client, err := New(Config{
		ProfileDir:        t.TempDir(),
		Timeout:           10 * time.Second,
		Headless:          true,
		MaxConcurrentTabs: 3,
	}, testURLBuilder)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if cap(client.semaphore) != 3 {
		t.Fatalf("tab capacity = %d", cap(client.semaphore))
	}
}

func TestClientSelectsStickyHeaderProfile(t *testing.T) {
	pool, err := headerprofile.NewStaticPool([]headerprofile.Profile{{
		Name: "browser-profile", UserAgent: "browser-agent", AcceptLanguage: "zh-CN",
		Platform: "MacIntel", ViewportWidth: 1440, ViewportHeight: 900, DeviceScaleFactor: 2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		ProfileDir: t.TempDir(), Timeout: 10 * time.Second, Headless: true,
		HeaderProfiles: pool,
	}, testURLBuilder)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	profile, err := client.selectHeaderProfile(domain.SearchRequest{RequestID: "request-42", Query: "go"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "browser-profile" || profile.Platform != "MacIntel" || profile.ViewportWidth != 1440 {
		t.Fatalf("profile=%+v", profile)
	}
}

func TestConcurrentColdStartFailureDoesNotPanic(t *testing.T) {
	client, err := New(Config{
		ProfileDir:        t.TempDir(),
		ExecPath:          "/bin/false",
		Timeout:           time.Second,
		Headless:          true,
		MaxConcurrentTabs: 3,
	}, testURLBuilder)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	const requestCount = 3
	var waitGroup sync.WaitGroup
	errors := make(chan error, requestCount)
	for index := 0; index < requestCount; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, fetchErr := client.FetchURL(context.Background(), "https://example.com")
			errors <- fetchErr
		}()
	}
	waitGroup.Wait()
	close(errors)
	for fetchErr := range errors {
		if fetchErr == nil {
			t.Fatal("expected browser startup failure")
		}
	}
}

func TestClientInitializesBrowserOnceConcurrently(t *testing.T) {
	const callerCount = 8
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	client := &Client{
		browserCtx: context.Background(),
		browserInit: func(context.Context) error {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-release
			return nil
		},
	}

	errors := make(chan error, callerCount)
	for index := 0; index < callerCount; index++ {
		go func() {
			errors <- client.ensureBrowser()
		}()
	}
	<-started
	close(release)
	for index := 0; index < callerCount; index++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("browser initialization calls = %d", calls.Load())
	}
}

func TestClientCachesBrowserInitializationFailure(t *testing.T) {
	wantErr := errors.New("browser unavailable")
	var calls atomic.Int32
	client := &Client{
		browserCtx: context.Background(),
		browserInit: func(context.Context) error {
			calls.Add(1)
			return wantErr
		},
	}

	for index := 0; index < 3; index++ {
		if err := client.ensureBrowser(); !errors.Is(err, wantErr) {
			t.Fatalf("ensureBrowser() error = %v, want %v", err, wantErr)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("browser initialization calls = %d, want 1", calls.Load())
	}
}

func TestNewRejectsMissingURLBuilder(t *testing.T) {
	_, err := New(Config{ProfileDir: t.TempDir(), Timeout: 10 * time.Second}, nil)
	if err == nil || !strings.Contains(err.Error(), "builder") {
		t.Fatalf("err=%v", err)
	}
}

func testURLBuilder(_ domain.SearchRequest) (string, error) {
	return "https://example.com/search", nil
}
