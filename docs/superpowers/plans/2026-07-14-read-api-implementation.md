# Read API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans task by task.

**Goal:** Add a safe, cacheable `POST /v1/read` API that turns public HTML or plain-text URLs into bounded Markdown or text. Production v1 registers only the SSRF-safe HTTP reader.

**Architecture:** `ReadService` orchestrates narrow interfaces for URL policy, readers, type detection, extraction, quality evaluation, conversion, and cache. Registries own source/output dispatch. A browser reader remains an optional interface injection point but is not registered in production v1.

**Tech Stack:** Go 1.26, Gin, `net/http`, `codeberg.org/readeck/go-readability/v2`, `html-to-markdown/v2`, goquery.

## Global Constraints

- Business branch values use custom types plus typed `const`.
- Every exported symbol has a Go Doc comment beginning with its name.
- Production code follows a failing test for the same behavior.
- Public URL validation is mandatory for initial URLs and redirects; no global SSRF bypass.
- Debug-only original errors never enter cached documents or normal responses.
- PDF and other binary formats remain explicitly unsupported in phase one.

### Task 1: Domain Models and Pipeline Interfaces

**Files:** `internal/domain/read.go`, `internal/read/pipeline.go`, their tests.

- [x] Define typed source types, output formats, read stages, classifications, quality actions, and implementation names.
- [x] Define `ReadRequest`, `Resource`, `ReadDocument`, `ReadResponse`, `ReadAttempt`, and read error codes.
- [x] Define narrow Reader, Detector, Extractor, Evaluator, Converter, Cache, and URL Policy interfaces.
- [x] Test validation helpers and Unicode-safe response truncation contracts.

### Task 2: Registries, Extractors, Quality, and Converters

**Files:** `internal/read/extractor/*`, `internal/read/converter/*`, `internal/read/quality/*`, fixtures.

- [x] Test duplicate and missing registrations, then implement immutable registries.
- [x] Implement replaceable article-like HTML and plain-text extraction behind `ContentExtractor`.
- [x] Implement Markdown and text conversion behind `ContentConverter`.
- [x] Implement typed quality results for accept, render, and reject.
- [x] Test article HTML, JS shell, captcha/login content, malformed text, and rune-safe paragraph truncation.

### Task 3: Safe URL Policy and HTTP Reader

**Files:** `internal/read/safeurl/*`, `internal/read/reader/http.go`, tests.

- [x] Test schemes, userinfo, loopback/private/link-local/CGNAT/metadata IPv4 and IPv6, mixed DNS answers, and explicit host allowlist.
- [x] Resolve and pin public addresses while preserving Host and TLS server name.
- [x] Revalidate every redirect and cap redirect count.
- [x] Bound decompressed body size, timeout, content type, and sensitive outbound headers.
- [x] Test HTML, text, PDF rejection, oversized body, timeout, and unsafe redirect.

### Task 4: Read Cache and Service Orchestration

**Files:** `internal/read/cache/*`, `internal/app/read_service.go`, tests.

- [x] Implement a bounded memory cache for canonical `ReadDocument` values.
- [x] Test fresh hit, refresh bypass, singleflight, stale fallback, and cache-write warnings.
- [x] Record ordered typed attempts at policy/read/detect/extract/quality/convert boundaries.
- [x] Return a stable extraction error for `QualityActionRender` when no policy-safe browser reader is registered; do not retry access-control or captcha failures.
- [x] Convert and truncate only after cache retrieval so cache is output-format independent.

### Task 5: Chromedp Reader（后续阶段，第一版不注册）

**Files:** `internal/read/reader/chromedp.go`, tests.

- [ ] Use a policy-aware proxy so validated DNS answers are pinned for every browser request.
- [ ] Use isolated browser contexts, bounded concurrency, and a request-scoped render deadline.
- [ ] Bound navigation, subresource and DOM memory before registering the implementation.
- [ ] Return rendered HTML through the same extraction pipeline and expose ordered attempts only in authorized debug.

### Task 6: HTTP API, Bootstrap, and Documentation

**Files:** `internal/api/httpapi/read_handler.go`, router/bootstrap/config tests, `README.md`, `compose.yaml`.

- [x] Bind and validate the JSON request, authorize `debug=true`, and force refresh for debug.
- [x] Map read error codes to stable HTTP status codes while retaining original errors in authorized debug.
- [x] Register `/v1/read`, assemble dependencies, configuration, cache, and closers.
- [x] Document request/response fields, curl examples, environment variables, limits, and phase-two PDF boundary.
- [x] Run `go test ./... -count=1`, `make build`, `git diff --check`, and ReviewCode checks.

### Task 7: Local Search and Reader UI

**Files:** `webui/index.html`, `webui/app.css`, `webui/app.js`, static route tests.

- [x] Provide search query/provider/page/limit/refresh/debug controls and session-only debug token storage.
- [x] Render result cards, cache/fallback/timing/warnings, and structured debug attempts.
- [x] Read a selected result through `/v1/read`, select Markdown/text and `max_chars`, and show metadata, truncation, warnings, and errors.
- [x] Add responsive loading, empty, captcha/rate-limit, stale/degraded, and retry states without external CDN dependencies.
- [x] Serve the UI locally from the same Gin origin and verify it against fixture-backed API tests.
