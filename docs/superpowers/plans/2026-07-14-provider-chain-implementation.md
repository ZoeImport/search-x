# Provider Chain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add typed `auto`, `baidu`, `duckduckgo`, and `bing` providers with provider-level fallback while preserving strict explicit-provider behavior.

**Architecture:** `ProviderChain` implements the existing `Provider` contract and is registered as `auto`. DuckDuckGo uses bounded HTTP HTML parsing; Bing uses the generic Chromedp transport with an engine-specific URL builder and parser. `SearchService` remains responsible for cache and singleflight only.

**Tech Stack:** Go 1.26, Gin, goquery, chromedp, `net/http`, existing in-memory cache.

## Global Constraints

- Default provider is `auto`; explicit provider selection never crosses providers.
- Auto order is `baidu -> duckduckgo -> bing`.
- Business enums use custom types plus typed `const`; no magic strings or mutable enum variables.
- Every exported Go symbol has a complete Go Doc comment beginning with its own name.
- Production code is written only after the corresponding test fails for the expected reason.
- Debug responses preserve provider and transport attempts; normal responses do not expose raw errors.
- No paid API, API key, CAPTCHA bypass, result fusion, or concurrent provider racing.

---

### Task 1: Typed Provider Domain

**Files:**
- Modify: `internal/domain/search.go`
- Modify: `internal/domain/errors.go`
- Modify: `internal/provider/provider.go`
- Modify: `internal/provider/registry.go`
- Test: `internal/provider/registry_test.go`

**Interfaces:**
- Produces: `domain.ProviderName`, `domain.TransportName`, typed provider fields, and `Provider.Name() domain.ProviderName`.

- [ ] **Step 1: Write the failing typed-registry test**

```go
func TestRegistryUsesTypedProviderNames(t *testing.T) {
	registry := NewRegistry()
	stub := stubProvider{name: domain.ProviderNameBaidu}
	if err := registry.Register(stub); err != nil {
		t.Fatal(err)
	}
	got, ok := registry.Get(domain.ProviderNameBaidu)
	if !ok || got.Name() != domain.ProviderNameBaidu {
		t.Fatalf("got=%v ok=%v", got, ok)
	}
}
```

- [ ] **Step 2: Run the test and verify RED**

Run: `go test ./internal/provider -run TestRegistryUsesTypedProviderNames -count=1`

Expected: FAIL because `domain.ProviderName` and typed constants do not exist.

- [ ] **Step 3: Implement typed domain values and update signatures**

```go
// ProviderName identifies a registered search source or strategy.
type ProviderName string

const (
	// ProviderNameAuto selects the configured provider chain.
	ProviderNameAuto ProviderName = "auto"
	// ProviderNameBaidu selects Baidu search only.
	ProviderNameBaidu ProviderName = "baidu"
	// ProviderNameDuckDuckGo selects DuckDuckGo search only.
	ProviderNameDuckDuckGo ProviderName = "duckduckgo"
	// ProviderNameBing selects Bing search only.
	ProviderNameBing ProviderName = "bing"
)

// TransportName identifies the concrete transport used by a provider.
type TransportName string

const (
	// TransportNameDesktopHTTP identifies Baidu desktop HTTP.
	TransportNameDesktopHTTP TransportName = "desktop_http"
	// TransportNameMobileHTTP identifies Baidu mobile HTTP.
	TransportNameMobileHTTP TransportName = "mobile_http"
	// TransportNameChromedp identifies a Chromedp browser transport.
	TransportNameChromedp TransportName = "chromedp"
	// TransportNameDuckDuckGoHTTP identifies DuckDuckGo HTML HTTP.
	TransportNameDuckDuckGoHTTP TransportName = "duckduckgo_http"
	// TransportNameBingChromedp identifies Bing browser search.
	TransportNameBingChromedp TransportName = "bing_chromedp"
)
```

Change request, response, meta, and attempt provider/transport fields to these types. Change `Registry.Get` and `Provider.Name` to use `ProviderName`; normalize external strings once in the HTTP boundary.

- [ ] **Step 4: Run affected tests and verify GREEN**

Run: `go test ./internal/domain ./internal/provider ./internal/provider/baidu ./internal/app ./internal/api/httpapi -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain internal/provider internal/app internal/api/httpapi
git commit -m "refactor: type provider domain values"
```

### Task 2: ProviderChain

**Files:**
- Create: `internal/provider/chain.go`
- Create: `internal/provider/chain_test.go`
- Modify: `internal/domain/search.go`

**Interfaces:**
- Consumes: `provider.Provider`, typed `domain.ProviderName`, `domain.SearchError`.
- Produces: `provider.NewChain(name domain.ProviderName, members ...Provider) (*Chain, error)`.

- [ ] **Step 1: Write failing chain behavior tests**

```go
func TestChainFallsBackOnRetryableProviderError(t *testing.T) {
	baidu := &chainStub{name: domain.ProviderNameBaidu, err: &domain.SearchError{
		Code: domain.ErrCaptchaRequired, Retryable: true, Original: errors.New("baidu captcha"),
	}}
	duck := &chainStub{name: domain.ProviderNameDuckDuckGo, response: domain.SearchResponse{
		Provider: domain.ProviderNameDuckDuckGo,
		Results: []domain.SearchResult{{Title: "Go"}},
	}}
	chain, err := NewChain(domain.ProviderNameAuto, baidu, duck)
	if err != nil { t.Fatal(err) }
	got, err := chain.Search(context.Background(), domain.SearchRequest{Provider: domain.ProviderNameAuto})
	if err != nil { t.Fatal(err) }
	if got.Provider != domain.ProviderNameDuckDuckGo || got.Meta.ProviderFallbackCount != 1 || !got.Meta.Degraded {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestChainStopsOnNonRetryableError(t *testing.T) {
	first := &chainStub{name: domain.ProviderNameBaidu, err: &domain.SearchError{
		Code: domain.ErrInvalidRequest, Retryable: false, Original: errors.New("bad input"),
	}}
	second := &chainStub{name: domain.ProviderNameDuckDuckGo}
	chain, _ := NewChain(domain.ProviderNameAuto, first, second)
	_, err := chain.Search(context.Background(), domain.SearchRequest{})
	if err == nil || second.calls != 0 { t.Fatalf("err=%v calls=%d", err, second.calls) }
}
```

Also test first-provider success, context cancellation, duplicate/empty members, and all-provider failure preserving ordered attempts.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/provider -run Chain -count=1`

Expected: FAIL because `Chain`, `NewChain`, and provider fallback metadata do not exist.

- [ ] **Step 3: Implement the minimal chain**

Implement a sequential loop with early return. Continue only for the five retryable upstream codes in the design. Annotate every inherited attempt with the current provider, aggregate artifacts and original errors with provider names, add one `provider_fallback` warning per skipped provider, and set `RequestedProvider`, `ProviderFallbackCount`, and `Degraded` on success.

- [ ] **Step 4: Run tests and verify GREEN**

Run: `go test ./internal/provider ./internal/app -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/chain.go internal/provider/chain_test.go internal/domain/search.go
git commit -m "feat: add provider fallback chain"
```

### Task 3: DuckDuckGoProvider

**Files:**
- Create: `internal/provider/duckduckgo/provider.go`
- Create: `internal/provider/duckduckgo/provider_test.go`
- Create: `internal/provider/duckduckgo/parser.go`
- Create: `internal/provider/duckduckgo/parser_test.go`
- Create: `testdata/duckduckgo/normal.html`
- Create: `testdata/duckduckgo/empty.html`

**Interfaces:**
- Produces: `duckduckgo.New(config Config, client *http.Client) (*Provider, error)` implementing `provider.Provider`.

- [ ] **Step 1: Add parser fixtures and failing tests**

```go
func TestParseResultsAndResolveRedirectURL(t *testing.T) {
	body := fixture(t, "normal.html")
	got, err := Parse(body, 10)
	if err != nil { t.Fatal(err) }
	if len(got) != 2 { t.Fatalf("len=%d", len(got)) }
	if got[0].URL != "https://go.dev/" || got[0].Rank != 1 {
		t.Fatalf("first=%+v", got[0])
	}
}
```

The fixture must contain `.result`, `.result__a`, `.result__snippet`, and a `uddg=https%3A%2F%2Fgo.dev%2F` redirect.

- [ ] **Step 2: Verify parser RED**

Run: `go test ./internal/provider/duckduckgo -run Parse -count=1`

Expected: FAIL because the package and parser do not exist.

- [ ] **Step 3: Implement parser and provider**

Use goquery selectors from the design. Build `q` and `s=(page-1)*limit`, enforce timeout and body limit, classify 429, 5xx, timeout, unexpected roots, and network errors into `domain.SearchError`. Return one typed attempt with `ProviderNameDuckDuckGo` and `TransportNameDuckDuckGoHTTP`.

- [ ] **Step 4: Add HTTP server behavior tests**

Use `httptest.NewServer` to assert encoded query, offset, headers, raw status classification, and response body limit. Test normal, empty, 429, 500, timeout, and changed markup.

- [ ] **Step 5: Run package tests and verify GREEN**

Run: `go test ./internal/provider/duckduckgo -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/duckduckgo testdata/duckduckgo
git commit -m "feat: add DuckDuckGo provider"
```

### Task 4: Generic Chromedp URL Builder and BingProvider

**Files:**
- Modify: `internal/transport/chromebrowser/client.go`
- Modify: `internal/transport/chromebrowser/client_test.go`
- Create: `internal/provider/bing/provider.go`
- Create: `internal/provider/bing/provider_test.go`
- Create: `internal/provider/bing/parser.go`
- Create: `internal/provider/bing/parser_test.go`
- Create: `testdata/bing/normal.html`
- Create: `testdata/bing/captcha.html`

**Interfaces:**
- Produces: `chromebrowser.URLBuilder`, `chromebrowser.New(config, builder)`, and `bing.New(transport, artifacts) *Provider`.

- [ ] **Step 1: Write failing URL builder tests**

```go
func TestClientUsesInjectedURLBuilder(t *testing.T) {
	builder := func(request domain.SearchRequest) (string, error) {
		return "https://www.bing.com/search?q=" + url.QueryEscape(request.Query), nil
	}
	client, err := New(testConfig(t), builder)
	if err != nil { t.Fatal(err) }
	defer client.Close()
	if got, _ := client.buildURL(domain.SearchRequest{Query: "go language"}); got != "https://www.bing.com/search?q=go+language" {
		t.Fatalf("url=%s", got)
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/transport/chromebrowser -run InjectedURLBuilder -count=1`

Expected: FAIL because `New` does not accept a URL builder.

- [ ] **Step 3: Generalize Chromedp transport without changing lifecycle behavior**

Define `type URLBuilder func(domain.SearchRequest) (string, error)`. Require it in `New`; move the current Baidu URL builder into the Baidu bootstrap construction. Keep semaphore, profile, screenshot, DOM capture, timeout, and Close behavior unchanged.

- [ ] **Step 4: Write Bing parser/provider tests**

```go
func TestParseBingResults(t *testing.T) {
	got, err := Parse(fixture(t, "normal.html"), 10)
	if err != nil { t.Fatal(err) }
	if len(got) != 2 || got[0].URL != "https://go.dev/" || got[0].Snippet == "" {
		t.Fatalf("results=%+v", got)
	}
}
```

Test URL pagination with `first=(page-1)*limit+1`, `.b_algo h2 a`, `.b_caption p`, CAPTCHA markup, empty results, changed markup, and transport errors.

- [ ] **Step 5: Implement BingProvider and verify GREEN**

Run: `go test ./internal/transport/chromebrowser ./internal/provider/bing ./internal/provider/baidu -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/transport/chromebrowser internal/provider/bing internal/provider/baidu testdata/bing
git commit -m "feat: add Bing browser provider"
```

### Task 5: Configuration, Bootstrap, API, Cache, and Documentation

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/bootstrap/app.go`
- Modify: `internal/bootstrap/app_test.go`
- Modify: `internal/api/httpapi/search_handler.go`
- Modify: `internal/api/httpapi/search_handler_test.go`
- Modify: `internal/app/search_service.go`
- Modify: `internal/app/search_service_test.go`
- Modify: `README.md`
- Modify: `compose.yaml`

**Interfaces:**
- Consumes: all concrete providers and `provider.NewChain`.
- Produces: live registry entries `baidu`, `duckduckgo`, `bing`, `auto`; default request provider `auto`.

- [ ] **Step 1: Write failing API and bootstrap tests**

```go
func TestSearchDefaultsToAutoProvider(t *testing.T) {
	searcher := &fakeSearcher{}
	router := newTestRouter(t, searcher)
	request := httptest.NewRequest(http.MethodGet, "/v1/search?q=golang", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if searcher.request.Provider != domain.ProviderNameAuto {
		t.Fatalf("provider=%s", searcher.request.Provider)
	}
}
```

Add bootstrap assertions that all four provider names resolve and that explicit `baidu` never invokes the chain.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/api/httpapi ./internal/bootstrap ./internal/app -count=1`

Expected: FAIL because default is still Baidu and only Baidu is registered.

- [ ] **Step 3: Add configuration and wire providers**

Add the five design environment variables for DuckDuckGo and Bing. Construct separate Bing profile and Chromedp client, register concrete providers first, then register `auto`. Track both browser clients in `App.Close`. Generalize stale-cache text and error metadata provider; preserve actual provider in cached auto responses and requested provider in meta.

- [ ] **Step 4: Update README and Compose**

Document provider selection, auto order, actual/requested provider fields, fallback counts, environment variables, and curl examples for all four values.

- [ ] **Step 5: Run full verification**

Run: `go test ./... -count=1`

Expected: PASS.

Run: `make build`

Expected: server binary builds successfully through the project Makefile.

- [ ] **Step 6: Commit**

```bash
git add internal README.md compose.yaml
git commit -m "feat: wire automatic search providers"
```
