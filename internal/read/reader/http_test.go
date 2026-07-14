package reader

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/read/safeurl"
)

type localResolver struct{}

func (localResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestHTTPReaderReadsPinnedHTMLAndPreservesOriginalHost(t *testing.T) {
	t.Parallel()

	var receivedHost string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedHost = request.Host
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write([]byte("<html><body>article</body></html>"))
	}))
	defer server.Close()

	policy, target := localTarget(t, server.URL, "/article")
	httpReader := mustHTTPReader(t, policy, Config{Timeout: time.Second, MaxBodyBytes: 1024, MaxRedirects: 2})

	resource, err := httpReader.Read(context.Background(), target)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got, want := receivedHost, target.URL.Host; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}
	if resource.ContentType != "text/html" || resource.Charset != "utf-8" {
		t.Fatalf("content type = %q charset = %q", resource.ContentType, resource.Charset)
	}
	if resource.Transport != domain.ReadTransportHTTP || resource.FinalURL != target.URL.String() {
		t.Fatalf("resource = %#v", resource)
	}
	if httpReader.Name() != domain.ImplementationNameHTTPReader {
		t.Fatalf("Name() = %q", httpReader.Name())
	}
}

func TestHTTPReaderAcceptsPlainTextAndDetectsMissingContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		header      string
		body        string
		contentType string
	}{
		{name: "plain text", header: "text/plain; charset=gb18030", body: "article", contentType: "text/plain"},
		{name: "detected html", body: "<!doctype html><title>article</title>", contentType: "text/html"},
		{name: "mislabeled html", header: "application/octet-stream", body: "<!doctype html><title>article</title>", contentType: "text/html"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.header != "" {
					writer.Header().Set("Content-Type", test.header)
				}
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			policy, target := localTarget(t, server.URL, "/article")
			resource, err := mustHTTPReader(t, policy, Config{Timeout: time.Second, MaxBodyBytes: 1024}).Read(context.Background(), target)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if resource.ContentType != test.contentType {
				t.Fatalf("ContentType = %q, want %q", resource.ContentType, test.contentType)
			}
		})
	}
}

func TestHTTPReaderRejectsUnsupportedContentType(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/pdf")
		_, _ = writer.Write([]byte("%PDF-1.7"))
	}))
	defer server.Close()
	policy, target := localTarget(t, server.URL, "/document.pdf")

	_, err := mustHTTPReader(t, policy, Config{Timeout: time.Second, MaxBodyBytes: 1024}).Read(context.Background(), target)
	assertReaderCode(t, err, domain.ErrUnsupportedContentType)
}

func TestHTTPReaderBoundsDecompressedBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writer.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(writer)
		_, _ = compressed.Write([]byte(strings.Repeat("a", 2048)))
		_ = compressed.Close()
	}))
	defer server.Close()
	policy, target := localTarget(t, server.URL, "/large")

	_, err := mustHTTPReader(t, policy, Config{Timeout: time.Second, MaxBodyBytes: 128}).Read(context.Background(), target)
	assertReaderCode(t, err, domain.ErrContentTooLarge)
}

func TestHTTPReaderPreservesTimeoutError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = writer.Write([]byte("late"))
	}))
	defer server.Close()
	policy, target := localTarget(t, server.URL, "/slow")

	_, err := mustHTTPReader(t, policy, Config{Timeout: 20 * time.Millisecond, MaxBodyBytes: 1024}).Read(context.Background(), target)
	assertReaderCode(t, err, domain.ErrFetchTimeout)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Read() error = %v, want wrapped context deadline", err)
	}
}

func TestHTTPReaderRevalidatesUnsafeRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer server.Close()
	policy, target := localTarget(t, server.URL, "/redirect")

	_, err := mustHTTPReader(t, policy, Config{Timeout: time.Second, MaxBodyBytes: 1024, MaxRedirects: 2}).Read(context.Background(), target)
	assertReaderCode(t, err, domain.ErrUnsafeURL)
	if !errors.Is(err, safeurl.ErrUnsafeURL) {
		t.Fatalf("Read() error = %v, want wrapped safeurl.ErrUnsafeURL", err)
	}
}

func TestHTTPReaderPreservesRedirectResolverClassification(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "https://unresolved.example/article", http.StatusFound)
	}))
	defer server.Close()
	_, target := localTarget(t, server.URL, "/redirect")
	original := errors.New("DNS unavailable")
	policy := redirectErrorPolicy{err: &safeurl.PolicyError{Code: domain.ErrFetchFailed, URL: "https://unresolved.example/article", Original: original}}
	httpReader, err := NewHTTPReader(policy, Config{Timeout: time.Second, MaxBodyBytes: 1024, MaxRedirects: 2})
	if err != nil {
		t.Fatal(err)
	}

	_, err = httpReader.Read(context.Background(), target)
	assertReaderCode(t, err, domain.ErrFetchFailed)
	if !errors.Is(err, original) {
		t.Fatalf("Read() error = %v, want wrapped resolver error", err)
	}
}

func TestHTTPReaderCapsRedirectCount(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/again", http.StatusFound)
	}))
	defer server.Close()
	policy, target := localTarget(t, server.URL, "/again")

	_, err := mustHTTPReader(t, policy, Config{Timeout: time.Second, MaxBodyBytes: 1024, MaxRedirects: 1}).Read(context.Background(), target)
	assertReaderCode(t, err, domain.ErrFetchFailed)
}

func TestHTTPReaderDoesNotForwardSensitiveHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Referer"} {
			if request.Header.Get(name) != "" {
				t.Errorf("received sensitive header %s", name)
			}
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()
	policy, target := localTarget(t, server.URL, "/headers")

	_, err := mustHTTPReader(t, policy, Config{Timeout: time.Second, MaxBodyBytes: 1024}).Read(context.Background(), target)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
}

func localTarget(t *testing.T, serverURL, path string) (*safeurl.Policy, domain.SafeTarget) {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	policy := safeurl.NewPolicy(localResolver{}, safeurl.Config{AllowedHosts: []string{"demo.local"}})
	target, err := policy.ValidateAndResolve(context.Background(), fmt.Sprintf("http://demo.local:%s%s", port, path))
	if err != nil {
		t.Fatal(err)
	}
	return policy, target
}

func mustHTTPReader(t *testing.T, policy *safeurl.Policy, cfg Config) *HTTPReader {
	t.Helper()
	httpReader, err := NewHTTPReader(policy, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return httpReader
}

func assertReaderCode(t *testing.T, err error, want domain.ErrorCode) {
	t.Helper()
	var readerError interface {
		ReadErrorCode() domain.ErrorCode
	}
	if !errors.As(err, &readerError) {
		t.Fatalf("error = %v, want typed read error", err)
	}
	if readerError.ReadErrorCode() != want {
		t.Fatalf("error code = %q, want %q", readerError.ReadErrorCode(), want)
	}
}

type redirectErrorPolicy struct {
	err error
}

func (p redirectErrorPolicy) ValidateAndResolve(context.Context, string) (domain.SafeTarget, error) {
	return domain.SafeTarget{}, p.err
}
