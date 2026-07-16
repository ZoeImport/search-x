# Query and Fetch Benchmark Report

## Scope and methodology

- Date: 2026-07-15; host: Apple M5, macOS, Go 1.26.4, arm64.
- The API was started on an independent test port, `127.0.0.1:18180`.
- Search upstream fixtures ran locally at `127.0.0.1:18183` with a fixed 50 ms upstream delay. Provider/client rate limits were raised, jitter was fixed at 1 ms, trace was disabled, and each request used a unique query with `refresh=true` so cache and `singleflight` could not hide live cost.
- Read upstream fixtures ran locally at `127.0.0.1:18184`: `/article` is HTTP-readable long content, `/js` is a JavaScript shell that requires browser fallback. Each read used a unique URL with `refresh=true`.
- The profile matrix is a service-capacity benchmark. It intentionally removes public-network and anti-bot variance; therefore its "best" values are the best local service defaults, not a guarantee that public Baidu/Bing/Brave will tolerate the same load.

## Provider Profile Matrix

Internal Search benchmark endpoint: `POST /v1/websearch`, local 50 ms upstream delay, trace off. This is not the API Market client route.

| Provider | Profile setting | Concurrency | Requests | Success | Mean | p50 | p95 | p99 | Verdict |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| DuckDuckGo | 1 x 1 | 8 | 80 | 80/80 | 475.071 ms | 369.863 ms | 1132.273 ms | 1714.308 ms | Too small |
| DuckDuckGo | 2 x 1 | 8 | 80 | 80/80 | 232.172 ms | 184.929 ms | 429.608 ms | 553.556 ms | Better |
| DuckDuckGo | 4 x 1, current default | 8 | 80 | 80/80 | 116.121 ms | 109.284 ms | 189.741 ms | 246.814 ms | Good safe default |
| DuckDuckGo | 8 x 1 | 8 | 80 | 80/80 | 95.314 ms | 94.781 ms | 121.267 ms | 145.695 ms | Best local latency |
| Baidu | 1 x 1 | 8 | 48 | 48/48 | 470.321 ms | 362.644 ms | 1455.162 ms | 1897.188 ms | Too small |
| Baidu | 3 x 1 | 8 | 48 | 48/48 | 160.360 ms | 128.690 ms | 250.890 ms | 367.873 ms | Acceptable |
| Baidu | 6 x 1, current default | 8 | 48 | 48/48 | 89.942 ms | 77.140 ms | 148.924 ms | 184.700 ms | Best local default |
| Baidu | 12 x 1 | 8 | 48 | 48/48 | 107.382 ms | 99.473 ms | 151.572 ms | 170.875 ms | No useful gain |
| Bing | 1 x 1 | 4 | 16 | 16/16 | 736.367 ms | 275.332 ms | 3060.601 ms | 3060.601 ms | Too small |
| Bing | 3 x 1 | 6 | 18 | 18/18 | 603.044 ms | 288.904 ms | 1361.603 ms | 1361.603 ms | Best tested p95 |
| Bing | 5 x 2, current default | 10 | 30 | 30/30 | 704.091 ms | 269.105 ms | 1669.381 ms | 1672.542 ms | More capacity, worse tail |
| Bing | 8 x 2 | 12 | 36 | 36/36 | 884.517 ms | 357.256 ms | 2168.277 ms | 2176.493 ms | Too much Chrome pressure |
| Brave | 1 x 1 | 3 | 12 | 12/12 | 572.630 ms | 255.503 ms | 2445.962 ms | 2445.962 ms | Too small |
| Brave | 3 x 1, current default | 6 | 18 | 18/18 | 599.137 ms | 257.742 ms | 1825.674 ms | 1825.674 ms | Best tested balance |
| Brave | 6 x 1 | 8 | 24 | 24/24 | 711.925 ms | 239.322 ms | 1757.889 ms | 1868.376 ms | Slight tail gain, worse mean |

Recommended search defaults from this run:

- Safe production defaults: keep Baidu `6 x 1`, keep Brave `3 x 1`, reduce Bing from `5 x 2` to `3 x 1` if tail latency matters more than burst capacity, keep DuckDuckGo `4 x 1`.
- Performance-biased defaults for controlled/local or trusted upstreams: DuckDuckGo `8 x 1`, Baidu `6 x 1`, Bing `3 x 1`, Brave `3 x 1`.
- Do not raise Baidu above `6 x 1` by default. The measured mean regressed at `12 x 1`, and public Baidu has the highest anti-bot risk.

## Auto Mode Matrix

Same workload for all rows: concurrency 16, 64 unique auto requests, trace off.

| Auto setting | Effective logical slots | Success | Mean | p50 | p95 | p99 | Selected providers |
|---|---:|---:|---:|---:|---:|---:|---|
| low: Baidu 1, Bing 1 x 1, Brave 1, Duck 1 | 4 | 64/64 | 405.282 ms | 285.581 ms | 1159.417 ms | 1603.577 ms | baidu 25, bing 6, brave 6, duck 27 |
| current default: Baidu 6, Bing 5 x 2, Brave 3, Duck 4 | 23 | 64/64 | 343.586 ms | 90.855 ms | 1658.906 ms | 1664.871 ms | baidu 54, bing 10 |
| high: Baidu 12, Bing 8 x 2, Brave 6, Duck 8 | 42 | 64/64 | 230.636 ms | 125.188 ms | 1321.317 ms | 1382.797 ms | baidu 60, bing 4 |

Auto mode can absorb more local load when all provider capacities are raised, but this result is not enough to make `42` the public default. It primarily shifts more work to Baidu. The safer default is still the current auto slot budget unless public-provider error-rate monitoring shows Baidu/Bing can tolerate more.

## Read Matrix

Internal benchmark endpoint: `POST /v1/webfetch`, unique URLs, local 50 ms upstream delay. This is not the API Market client route.

| Read mode | Slots | Wait | Concurrency | Requests | Success | Mean | p50 | p95 | Transport |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| HTTP-readable article | 3 | 2 s | 8 | 80 | 80/80 | 56.116 ms | 56.351 ms | 58.900 ms | http 80 |
| JS shell, browser fallback | 1 | 2 s | 3 | 9 | 9/9 | 6144.384 ms | 6550.506 ms | 7554.277 ms | chromedp 9 |
| JS shell, browser fallback | 3, current default | 2 s | 6 | 12 | 12/12 | 4440.816 ms | 4533.475 ms | 5538.212 ms | chromedp 12 |
| JS shell, browser fallback | 6 | 2 s | 8 | 16 | 16/16 | 3507.222 ms | 3308.684 ms | 5860.669 ms | chromedp 16 |
| JS shell, browser fallback | 3 | 100 ms | 6 | 12 | 12/12 | 1112.080 ms | 741.184 ms | 1816.705 ms | chromedp 12 |

Read conclusions:

- Search and read were tested separately. Search profile tuning does not affect standalone read.
- HTTP read is fast once the page is accepted by the extractor: about 56 ms with the 50 ms fixture delay.
- Browser fallback is dominated by the fixed post-load wait. Reducing `WEBFETCH_BROWSER_WAIT` from 2 s to 100 ms, with the same 3 slots, improved mean latency from 4440.816 ms to 1112.080 ms.
- Increasing `WEBFETCH_BROWSER_SLOTS` from 3 to 6 improves mean latency, but p95 stays high because Chrome startup/render cost and the fixed wait remain. The better default change is conditional/shorter wait before raising slots.

## Go Micro-Benchmarks

Run:

```bash
go test ./internal/benchmark -run '^$' -bench . -benchmem -count=3
```

| Benchmark | Result range | Bytes/op | Allocs/op |
|---|---:|---:|---:|
| `BenchmarkDuckDuckGoParse` | 7.575-7.629 us/op | 13,848 | 196 |
| `BenchmarkHTMLExtract` | 148.808-150.077 us/op | 118-119 KB | 1,022 |

The parser and extractor are not responsible for multi-second read latency. They become relevant only after browser fallback and trace fsync costs are reduced.

## Code Bottleneck Analysis

1. Strict trace is still the largest search-side risk. `SearchService` appends trace events before and after work in `internal/app/search_service.go`; `internal/searchtrace/spool.go` serializes appends on a mutex and calls `file.Sync()` while holding it. In the earlier isolated trace run, p95 rose from 30.609 ms to 203.394 ms at concurrency 4.
2. Search explicit-provider concurrency is bounded by `internal/profilepool.Pool`. `TryAcquireAuto`/`Acquire` reserve logical profile slots, so p95 growth at concurrency above slot count is expected queueing, not CPU saturation.
3. Browser providers are Chrome-bound. Bing and Brave do not improve monotonically with more profiles because each profile can create more Chrome contexts/tabs. The measured tail regressed for Bing `5 x 2` and `8 x 2` compared with `3 x 1`.
4. WebFetch browser fallback always sleeps `PostLoadWait` after body detection in `webfetch/internal/reader/browser.go`. Current default is `WEBFETCH_BROWSER_WAIT=2s`, which creates a latency floor even for a local page.
5. The HTTP reader clones a transport and creates a fresh `http.Client` per read in `internal/fetch/reader/http.go` to support pinned DNS. This preserves SSRF safety but limits connection reuse.

## Recommended Changes

1. Make strict trace fsync batched or async with drain-on-shutdown. Keep per-event fsync as an audit mode, not the normal default.
2. Change read browser wait from one global fixed sleep to conditional waiting: short default such as 100-300 ms plus domain/quality-triggered extension for JS-heavy pages.
3. Keep `WEBFETCH_BROWSER_SLOTS=3` as the safe default until wait is fixed. Raising to 6 helps mean latency but does not solve tail latency.
4. For search defaults, prefer: Baidu `6 x 1`, Bing `3 x 1`, Brave `3 x 1`, DuckDuckGo `4 x 1` safe / `8 x 1` performance-biased.
5. Add production counters for provider error rate, CAPTCHA/rate-limit classification, profile queue wait, selected provider in auto, read fallback reason, and read browser queue wait. Without those counters, public-provider "optimal" tuning is blind.

## Verification

- `go test ./internal/benchmark -run '^$' -bench . -benchmem -count=3`: PASS.
- `go test ./...`: PASS.
