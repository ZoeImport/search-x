# Parity-Plus Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a native Go `POST /v1/search` mode that selects the best search provider, oversamples candidates, reads enough usable bodies, preserves original and selected ranks, and remains compatible with the existing lightweight search and read APIs.

**Architecture:** Keep HTTP routing thin and introduce replaceable quality, provider-selection, read-scheduling, and orchestration interfaces. Reuse `SearchService` and `ReadService`; the combined service coordinates them without duplicating provider transports, SSRF policy, extraction, conversion, caching, or debug behavior.

**Tech Stack:** Go 1.26, Gin 1.12, Chromedp 0.15, goquery 1.12, standard-library worker pools and contexts.

## Global Constraints

- Keep `GET /v1/search` and `POST /v1/read` backward compatible.
- Add advanced behavior at `POST /v1/search`; do not add `/v1/search/content`.
- Use custom business types plus typed `const` declarations; do not propagate magic business strings.
- Keep Provider, scorer, scheduler, reader, extractor, converter, and selector implementations behind interfaces.
- Do not bypass CAPTCHA, login, paywall, SSRF policy, or redirect validation.
- PDF, Office, image, and OCR extraction remain out of scope.
- Total combined request budget defaults to 30 seconds; HTTP read concurrency is 8 and browser tab concurrency is 3.
- Automatic candidate count is \(C=\min(2N+2,20)\), where \(N\) is the requested usable-body count.
- Use TDD and commit each independently testable task with `misszoe@gmail.com` as the commit email.

---

### Task 1: Combined-search domain contract and candidate policy

**Files:**
- Create: `internal/domain/search_content.go`
- Create: `internal/app/candidate_policy.go`
- Test: `internal/domain/search_content_test.go`
- Test: `internal/app/candidate_policy_test.go`

**Interfaces:**
- Consumes: existing `domain.ProviderName`, `domain.OutputFormat`, `domain.SearchResult`, `domain.ReadResponse`, and `domain.ErrorCode`.
- Produces: `domain.SearchContentRequest.Normalize()`, `domain.SearchContentResponse`, `domain.SearchContentResult`, `domain.SearchContentFailure`, `domain.ProviderSelection`, and `app.CandidateLimit(int, int) (int, error)`.

- [ ] **Step 1: Write failing normalization and candidate-count tests**

```go
func TestSearchContentRequestNormalize(t *testing.T) {
	request, err := (SearchContentRequest{Query: "  Go 语言 ", Limit: 5, Content: ContentOptions{Enabled: true}}).Normalize()
	if err != nil { t.Fatal(err) }
	if request.Query != "Go 语言" || request.Provider != ProviderNameAuto { t.Fatalf("unexpected request: %+v", request) }
	if request.Content.Format != OutputFormatMarkdown || request.Content.MaxChars != DefaultReadMaxChars { t.Fatalf("unexpected content: %+v", request.Content) }
}

func TestCandidateLimit(t *testing.T) {
	got, err := CandidateLimit(5, 0)
	if err != nil || got != 12 { t.Fatalf("got=%d err=%v", got, err) }
	got, err = CandidateLimit(10, 0)
	if err != nil || got != 20 { t.Fatalf("got=%d err=%v", got, err) }
	if _, err = CandidateLimit(5, 4); err == nil { t.Fatal("expected explicit candidate limit validation") }
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

Run: `go test ./internal/domain ./internal/app -run 'Test(SearchContentRequestNormalize|CandidateLimit)$' -count=1`

Expected: FAIL because the new request types and `CandidateLimit` do not exist.

- [ ] **Step 3: Add typed contracts and validation**

```go
type ReadStatus string

const (
	ReadStatusSuccess ReadStatus = "success"
	ReadStatusFailed  ReadStatus = "failed"
)

type ContentOptions struct {
	Enabled        bool         `json:"enabled"`
	CandidateLimit int          `json:"candidate_limit"`
	Format         OutputFormat `json:"format"`
	MaxChars       int          `json:"max_chars"`
}

type SearchContentRequest struct {
	Query     string         `json:"query"`
	Provider  ProviderName   `json:"provider"`
	Limit     int            `json:"limit"`
	Content   ContentOptions `json:"content"`
	Refresh   bool           `json:"refresh"`
	Debug     bool           `json:"debug"`
	RequestID string         `json:"-"`
}

type SearchContentResult struct {
	OriginalRank  int            `json:"original_rank"`
	SelectedRank  int            `json:"selected_rank,omitempty"`
	Provider      ProviderName   `json:"provider"`
	Title         string         `json:"title"`
	URL           string         `json:"url"`
	Snippet       string         `json:"snippet"`
	Relevance     float64        `json:"relevance_score"`
	ReadStatus    ReadStatus     `json:"read_status,omitempty"`
	ReadTransport ReadTransport  `json:"read_transport,omitempty"`
	Content       string         `json:"content,omitempty"`
	ContentFormat OutputFormat   `json:"content_format,omitempty"`
	ContentLength int            `json:"content_length,omitempty"`
	Truncated     bool           `json:"truncated,omitempty"`
}
```

Implement `Normalize` with the existing 256-rune query bound, `limit` default 5 and range `1..10`, content defaults from `ReadRequest.Normalize`, and debug forcing refresh. Implement `CandidateLimit` as `min(2*limit+2, 20)` when explicit is zero, otherwise require `limit <= explicit <= 20`.

- [ ] **Step 4: Run domain and policy tests**

Run: `go test ./internal/domain ./internal/app -run 'Test(SearchContentRequest|CandidateLimit)' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the domain increment**

```bash
git add internal/domain/search_content.go internal/domain/search_content_test.go internal/app/candidate_policy.go internal/app/candidate_policy_test.go
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: define combined search contract"
```

### Task 2: Unicode/CJK search-quality evaluator

**Files:**
- Create: `internal/searchquality/evaluator.go`
- Test: `internal/searchquality/evaluator_test.go`

**Interfaces:**
- Consumes: `domain.SearchResult`.
- Produces: `searchquality.Evaluator`, `searchquality.Config`, `searchquality.Result`, and `Evaluate(query string, results []domain.SearchResult) Result`.

- [ ] **Step 1: Write failing English, Chinese, duplicate, and empty-field tests**

```go
func TestEvaluatorScoresChineseIntent(t *testing.T) {
	e := NewEvaluator(DefaultConfig())
	good := e.Evaluate("Go 语言并发模型", []domain.SearchResult{{Title: "Go 语言并发模型详解", URL: "https://go.dev/doc", Snippet: "goroutine channel 并发"}})
	bad := e.Evaluate("Go 语言并发模型", []domain.SearchResult{{Title: "Python 安装教程", URL: "https://example.com/python", Snippet: "pip"}})
	if good.Score <= bad.Score || good.Items[0].Score < 0.5 { t.Fatalf("good=%+v bad=%+v", good, bad) }
}

func TestEvaluatorPenalizesDuplicateAndIncompleteResults(t *testing.T) {
	e := NewEvaluator(DefaultConfig())
	result := e.Evaluate("golang", []domain.SearchResult{
		{Title: "Golang", URL: "https://example.com/a", Snippet: "golang"},
		{Title: "Golang copy", URL: "https://example.com/a", Snippet: ""},
	})
	if result.UniqueDomainRatio != 0.5 || result.Items[1].Score >= result.Items[0].Score { t.Fatalf("unexpected: %+v", result) }
}
```

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./internal/searchquality -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement Unicode tokenization and configured weights**

```go
type Config struct {
	TitleWeight, SnippetWeight, URLWeight, PhraseWeight float64
	TopWeight, DiversityWeight, CompletenessWeight      float64
	EmptyTitlePenalty, EmptyURLPenalty, DuplicatePenalty float64
}

type ItemScore struct { Rank int; Score float64 }
type Result struct {
	Score             float64
	Items             []ItemScore
	UniqueDomainRatio float64
	Completeness      float64
}

type Evaluator interface {
	Evaluate(query string, results []domain.SearchResult) Result
}
```

Tokenize Unicode letters and digits, retain full CJK spans, and add CJK unigrams and adjacent bigrams. Compute per-item and provider-set scores with the exact weights from the design and clamp all scores to `[0,1]`. Use `net/url` plus `publicsuffix.EffectiveTLDPlusOne` for diversity and canonical URL duplicate detection.

- [ ] **Step 4: Run evaluator tests**

Run: `go test ./internal/searchquality -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the evaluator**

```bash
git add internal/searchquality
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: add unicode search quality scoring"
```

### Task 3: Quality-based Provider selector

**Files:**
- Create: `internal/app/provider_selector.go`
- Test: `internal/app/provider_selector_test.go`

**Interfaces:**
- Consumes: `provider.Provider`, `searchquality.Evaluator`, `domain.SearchRequest`.
- Produces: `app.ProviderSelector.Select(context.Context, domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error)`.

- [ ] **Step 1: Write failing best-result-set and partial-failure tests**

```go
func TestQualityProviderSelectorChoosesBestSet(t *testing.T) {
	selector := newSelectorForTest(
		stubProvider{name: domain.ProviderNameBaidu, results: []domain.SearchResult{{Title: "天气", URL: "https://a.test"}}},
		stubProvider{name: domain.ProviderNameDuckDuckGo, results: []domain.SearchResult{{Title: "Go 语言并发模型", URL: "https://go.dev"}}},
	)
	response, selection, err := selector.Select(context.Background(), domain.SearchRequest{Query: "Go 语言并发模型", Provider: domain.ProviderNameAuto, Limit: 12})
	if err != nil { t.Fatal(err) }
	if response.Provider != domain.ProviderNameDuckDuckGo || selection.SelectedProvider != domain.ProviderNameDuckDuckGo { t.Fatalf("selection=%+v", selection) }
}

func TestQualityProviderSelectorToleratesProviderFailure(t *testing.T) {
	selector := newSelectorForTest(
		stubProvider{name: domain.ProviderNameBaidu, err: errors.New("captcha")},
		stubProvider{name: domain.ProviderNameDuckDuckGo, results: []domain.SearchResult{{Title: "Go", URL: "https://go.dev"}}},
	)
	if _, _, err := selector.Select(context.Background(), domain.SearchRequest{Query: "Go", Provider: domain.ProviderNameAuto, Limit: 5}); err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Verify the selector tests fail**

Run: `go test ./internal/app -run TestQualityProviderSelector -count=1`

Expected: FAIL because the selector types do not exist.

- [ ] **Step 3: Implement bounded concurrent selection**

```go
type ProviderSelector interface {
	Select(context.Context, domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error)
}
```

For an explicit Provider, invoke only that Provider. For `auto`, launch at most two provider calls concurrently in configured order under a 10-second search budget, retain the highest scored successful whole result set, and tie-break by valid result count, unique-domain count, then configured priority. Preserve every provider failure in authorized diagnostics and warnings without failing a successful selection.

- [ ] **Step 4: Run selector tests including cancellation and stable tie order**

Run: `go test ./internal/app -run 'TestQualityProviderSelector' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the selector**

```bash
git add internal/app/provider_selector.go internal/app/provider_selector_test.go
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: select providers by result quality"
```

### Task 4: Brave browser Provider

**Files:**
- Create: `internal/provider/brave/url_builder.go`
- Create: `internal/provider/brave/parser.go`
- Create: `internal/provider/brave/provider.go`
- Test: `internal/provider/brave/url_builder_test.go`
- Test: `internal/provider/brave/parser_test.go`
- Test: `internal/provider/brave/provider_test.go`
- Create: `testdata/brave/normal.html`
- Create: `testdata/brave/empty.html`
- Create: `testdata/brave/captcha.html`
- Modify: `internal/domain/search.go`

**Interfaces:**
- Consumes: `transport.SearchTransport`, `debugartifact.Store`, and the existing typed search error model.
- Produces: `domain.ProviderNameBrave`, `domain.TransportNameBraveChromedp`, `brave.BuildSearchURL`, `brave.Parse`, and `brave.New`.

- [ ] **Step 1: Add fixture-driven failing parser and URL tests**

```go
func TestParseNormal(t *testing.T) {
	body := mustReadFixture(t, "../../../testdata/brave/normal.html")
	results, err := Parse(body, 10)
	if err != nil { t.Fatal(err) }
	if len(results) != 2 || results[0].Rank != 1 || results[0].URL == "" { t.Fatalf("results=%+v", results) }
}

func TestBuildSearchURL(t *testing.T) {
	got, err := BuildSearchURL("https://search.brave.com/search", domain.SearchRequest{Query: "Go 语言", Limit: 10})
	if err != nil || !strings.Contains(got, "q=Go+%E8%AF%AD%E8%A8%80") { t.Fatalf("url=%q err=%v", got, err) }
}
```

- [ ] **Step 2: Verify Brave tests fail**

Run: `go test ./internal/provider/brave -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement the Provider with isolated constants and selectors**

Add typed constants:

```go
const (
	ProviderNameBrave ProviderName = "brave"
	TransportNameBraveChromedp TransportName = "brave_chromedp"
)
```

Build the public Brave Web Search URL, parse title/direct URL/snippet/rank from committed fixtures, detect empty and CAPTCHA pages, and map transport/parser errors into `domain.SearchError` with debug attempts and artifacts. Do not reuse Bing selectors or profile paths.

- [ ] **Step 4: Run Brave and domain tests**

Run: `go test ./internal/provider/brave ./internal/domain -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the Brave Provider**

```bash
git add internal/domain/search.go internal/provider/brave testdata/brave
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: add Brave browser provider"
```

### Task 5: Configurable Chromedp tab concurrency

**Files:**
- Modify: `internal/transport/chromebrowser/client.go`
- Test: `internal/transport/chromebrowser/client_test.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `chromebrowser.Config`.
- Produces: `chromebrowser.Config.MaxConcurrentTabs` and config fields for search/read browser slots.

- [ ] **Step 1: Write a failing concurrency-capacity test**

```go
func TestNewUsesConfiguredTabCapacity(t *testing.T) {
	client := mustNewClient(t, Config{ProfileDir: t.TempDir(), Timeout: time.Second, MaxConcurrentTabs: 3}, testURLBuilder)
	if cap(client.semaphore) != 3 { t.Fatalf("capacity=%d", cap(client.semaphore)) }
}
```

Add config tests asserting `SEARCH_READ_BROWSER_SLOTS=3` and `SEARCH_PROVIDER_BROWSER_SLOTS=2` parse as positive integers and reject zero.

- [ ] **Step 2: Verify focused tests fail**

Run: `go test ./internal/transport/chromebrowser ./internal/config -run 'Test(NewUsesConfiguredTabCapacity|Load.*BrowserSlots)' -count=1`

Expected: FAIL because the slot fields do not exist.

- [ ] **Step 3: Add slot configuration**

Default `MaxConcurrentTabs` to 1 in the transport when omitted. Configure search browser clients with 2 tabs and the read browser client with 3 tabs. Keep each engine and reader on its own profile and client.

- [ ] **Step 4: Run transport and config tests with the race detector**

Run: `go test -race ./internal/transport/chromebrowser ./internal/config -count=1`

Expected: PASS.

- [ ] **Step 5: Commit browser concurrency**

```bash
git add internal/transport/chromebrowser/client.go internal/transport/chromebrowser/client_test.go internal/config/config.go internal/config/config_test.go
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: configure browser tab concurrency"
```

### Task 6: Rank-preserving read scheduler and combined service

**Files:**
- Create: `internal/app/read_scheduler.go`
- Create: `internal/app/search_content_service.go`
- Test: `internal/app/read_scheduler_test.go`
- Test: `internal/app/search_content_service_test.go`

**Interfaces:**
- Consumes: `ProviderSelector`, `Reader.Read(context.Context, domain.ReadRequest)`, candidate policy, and quality item scores.
- Produces: `ReadScheduler.Read(context.Context, []domain.SearchResult, domain.ContentOptions) []domain.CandidateReadResult` and `SearchContentService.Search(context.Context, domain.SearchContentRequest)`.

- [ ] **Step 1: Write failing oversampling, stable-rank, and partial-result tests**

```go
func TestSearchContentServiceOversamplesAndKeepsOriginalRank(t *testing.T) {
	selector := &selectorStub{response: searchResponseWithRanks(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12)}
	reader := &readerStub{failRanks: map[int]bool{1:true, 2:true, 3:true}}
	service := NewSearchContentService(selector, NewReadScheduler(reader, 8), fixedClock)
	response, err := service.Search(context.Background(), domain.SearchContentRequest{Query:"go", Limit:5, Content:domain.ContentOptions{Enabled:true}})
	if err != nil { t.Fatal(err) }
	if response.CandidateCount != 12 || len(response.Results) != 5 { t.Fatalf("response=%+v", response) }
	if response.Results[0].OriginalRank != 4 || response.Results[0].SelectedRank != 1 { t.Fatalf("first=%+v", response.Results[0]) }
}

func TestReadSchedulerIgnoresCompletionOrder(t *testing.T) {
	results := schedulerFixtureWithReverseDelays()
	got := NewReadScheduler(reverseDelayReader{}, 8).Read(context.Background(), results, domain.ContentOptions{Enabled:true})
	if got[0].OriginalRank != 1 || got[len(got)-1].OriginalRank != len(got) { t.Fatalf("got=%+v", got) }
}
```

- [ ] **Step 2: Verify service tests fail**

Run: `go test ./internal/app -run 'Test(SearchContentService|ReadScheduler)' -count=1`

Expected: FAIL because the scheduler and service do not exist.

- [ ] **Step 3: Implement bounded reads, canonical URL dedupe, and selection**

```go
type ContentReader interface {
	Read(context.Context, domain.ReadRequest) (domain.ReadResponse, error)
}

type ReadScheduler interface {
	Read(context.Context, []domain.SearchResult, domain.ContentOptions) []domain.CandidateReadResult
}

type SearchContentOrchestrator interface {
	Search(context.Context, domain.SearchContentRequest) (domain.SearchContentResponse, error)
}
```

Start at most 8 `ReadService.Read` calls at once, canonicalize fragment/default-port/host case before dedupe, preserve the first original rank, record stable typed failures, and sort collected candidates by original rank. Select the first `N` successful bodies and assign continuous `selected_rank`. Set HTTP 200 partial semantics when at least one body succeeds and return `insufficient_readable_results` when all candidates fail. Stop only after `N` successes exist and every higher-priority candidate has completed.

- [ ] **Step 4: Run app tests and race detector**

Run: `go test -race ./internal/app -run 'Test(SearchContentService|ReadScheduler)' -count=1`

Expected: PASS with no race reports.

- [ ] **Step 5: Commit orchestration**

```bash
git add internal/app/read_scheduler.go internal/app/read_scheduler_test.go internal/app/search_content_service.go internal/app/search_content_service_test.go
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: orchestrate readable search results"
```

### Task 7: Advanced `POST /v1/search` handler and bootstrap wiring

**Files:**
- Create: `internal/api/httpapi/search_post_handler.go`
- Test: `internal/api/httpapi/search_post_handler_test.go`
- Modify: `internal/api/httpapi/router.go`
- Modify: `internal/bootstrap/app.go`
- Modify: `internal/bootstrap/app_test.go`
- Modify: `internal/config/config.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `SearchContentOrchestrator` and the existing debug-token/error helpers.
- Produces: `POST /v1/search` in light and content-enabled modes.

- [ ] **Step 1: Write failing handler tests**

```go
func TestPostSearchWithoutContentUsesLightSearch(t *testing.T) {
	light := &fakeSearcher{response: domain.SearchResponse{Results: []domain.SearchResult{}}}
	combined := &fakeSearchContentOrchestrator{}
	router := testRouterWithCombined(t, light, combined, Options{})
	request := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"go","limit":5}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder(); router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || light.calls != 1 || combined.calls != 0 { t.Fatalf("code=%d light=%d combined=%d", response.Code, light.calls, combined.calls) }
}

func TestPostSearchContentRequiresDebugToken(t *testing.T) {
	router := testRouterWithCombined(t, &fakeSearcher{}, &fakeSearchContentOrchestrator{}, Options{DebugToken:"secret"})
	request := httptest.NewRequest(http.MethodPost, "/v1/search", strings.NewReader(`{"query":"go","debug":true,"content":{"enabled":true}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder(); router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized { t.Fatalf("code=%d body=%s", response.Code, response.Body.String()) }
}
```

- [ ] **Step 2: Verify handler tests fail**

Run: `go test ./internal/api/httpapi -run TestPostSearch -count=1`

Expected: FAIL because the POST route and orchestrator option do not exist.

- [ ] **Step 3: Bind, authenticate, route, and map errors**

Add `SearchContentOrchestrator` to router options. Bind JSON with unknown-field rejection, attach request ID, authorize debug using `X-Debug-Token`, and force refresh for debug. Route content-disabled requests to the existing `Searcher`; route content-enabled requests to the orchestrator. Use existing stable error bodies and include original errors/attempts only for authorized debug.

In bootstrap, register Baidu, Browser Bing, Browser Brave, and HTTP DuckDuckGo with the quality selector, create `ReadScheduler` over the existing `ReadService`, and pass the composed service into the router. Add exact `curl` examples for light POST and body-enabled POST to README.

- [ ] **Step 4: Run HTTP and bootstrap tests**

Run: `go test ./internal/api/httpapi ./internal/bootstrap -count=1`

Expected: PASS, while existing GET and read handler tests remain unchanged.

- [ ] **Step 5: Commit API wiring**

```bash
git add internal/api/httpapi/search_post_handler.go internal/api/httpapi/search_post_handler_test.go internal/api/httpapi/router.go internal/bootstrap/app.go internal/bootstrap/app_test.go internal/config/config.go README.md
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: expose advanced search endpoint"
```

### Task 8: Local UI combined mode and structured output

**Files:**
- Modify: `webui/index.html`
- Modify: `webui/app.js`
- Modify: `webui/app.css`
- Modify: `internal/api/httpapi/webui_test.go`

**Interfaces:**
- Consumes: `POST /v1/search` request and response contract.
- Produces: a combined-search toggle, content options, readable-result display, expandable clean JSON, Markdown preview, and per-result Provider/rank metadata.

- [ ] **Step 1: Add a failing embedded-asset test**

```go
func TestWebUIContainsCombinedSearchControls(t *testing.T) {
	body := getUIIndex(t)
	for _, marker := range []string{"content-enabled", "candidate-limit", "result-json", "original_rank", "selected_rank"} {
		if !strings.Contains(body, marker) { t.Fatalf("missing %q", marker) }
	}
}
```

- [ ] **Step 2: Verify the UI test fails**

Run: `go test ./internal/api/httpapi -run TestWebUIContainsCombinedSearchControls -count=1`

Expected: FAIL because the controls are absent.

- [ ] **Step 3: Add the minimal API-driven UI behavior**

Keep the UI a static client: it must only call the backend API and must not implement browser automation. Add inputs for Provider, result limit, content enabled, candidate limit, format, max chars, refresh, and debug token. Render Provider, original rank, selected rank, read transport, Markdown content, failures, and an expandable syntax-highlighted JSON tree. Escape all source HTML before rendering Markdown output.

- [ ] **Step 4: Run UI and HTTP tests**

Run: `go test ./internal/api/httpapi -count=1`

Expected: PASS.

- [ ] **Step 5: Commit UI support**

```bash
git add webui/index.html webui/app.js webui/app.css internal/api/httpapi/webui_test.go
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "feat: add combined search UI mode"
```

### Task 9: Full backend verification and live smoke evidence

**Files:**
- Modify: `scripts/live-smoke.sh`
- Create: `testdata/read/article-2.html`
- Modify: `README.md`

**Interfaces:**
- Consumes: the completed API.
- Produces: repeatable fixture smoke checks and documented local live commands.

- [ ] **Step 1: Extend smoke coverage**

Make the script call `GET /v1/search`, `POST /v1/search` with `content.enabled=false`, `POST /v1/search` with `content.enabled=true`, and `POST /v1/read`. Assert HTTP status, JSON parseability, Provider presence, and rank invariants with `jq`.

- [ ] **Step 2: Run the full automated verification gate**

Run: `make check`

Expected: all package tests, race tests, `go vet`, and build pass.

- [ ] **Step 3: Start the service and run live smoke checks**

Run in terminal 1: `SEARCH_DEBUG_TOKEN=local-development-secret make run`

Run in terminal 2: `Q='Go 语言并发模型' make smoke`

Expected: at least one search Provider succeeds; combined mode either returns readable results or a typed partial/all-failed error with raw attempts available only when the debug token is supplied.

- [ ] **Step 4: Record verified behavior in README**

Document the exact request bodies, fields, partial-result semantics, CAPTCHA boundary, timeout budget, logs at `log/server.log`, and current live observations without claiming universal success.

- [ ] **Step 5: Commit verification support**

```bash
git add scripts/live-smoke.sh testdata/read/article-2.html README.md
git -c user.name=Codex -c user.email=misszoe@gmail.com commit -m "test: verify combined search workflow"
```
