# Read Resource Baseline

## Existing verified latency baseline

2026-07-15 在 Apple M5、macOS、Go 1.26.4、arm64 和本地 fixture 上测得：

| Scenario | Browser slots | Concurrency | Success | Mean | p95 |
|---|---:|---:|---:|---:|---:|
| HTTP article | 3 | 8 | 80/80 | 56.116 ms | 58.900 ms |
| JS fallback, wait 2 s | 3 | 6 | 12/12 | 4440.816 ms | 5538.212 ms |
| JS fallback, wait 100 ms | 3 | 6 | 12/12 | 1112.080 ms | 1816.705 ms |

固定 post-load wait 是主要延迟来源；增加 Browser slots 会提高内存压力，不能替代条件式等待。原始完整报告保留在工作区 [Query and Fetch Benchmark](../../../docs/benchmarks/2026-07-15-query-fetch.md)。

## Recommended initial Deployment resources

新 Read app 本机实测：

| Workload | Concurrency | Peak RSS | Peak sampled CPU | Result |
|---|---:|---:|---:|---|
| `https://go.dev/doc/` HTTP read | 8 | 40,864 KiB | 32.5% | 成功 |
| 本地 JS shell + Chromium fallback | 4 | 1,501,200 KiB（Read + Chrome 进程树） | 97.6% | 16/16 成功 |

Chromium 测试使用 4 slots、100 ms post-load wait，单次样本耗时 1,699 ms；负载结束后浏览器进程树仍约 955,424 KiB RSS。由此 Read Deployment 使用 requests `500m/2Gi`、limits `2 CPU/4Gi`。目标 Linux 容器和真实站点仍需再次复测。

## Micro benchmark

```bash
go test ./internal/benchmark -run '^$' -bench . -benchmem -count=3
```
