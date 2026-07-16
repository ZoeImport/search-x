# Search Resource Baseline

## Existing verified latency baseline

2026-07-15 在 Apple M5、macOS、Go 1.26.4、arm64 上使用本地 fixture 测得：

| Scenario | Concurrency | Success | Mean | p95 |
|---|---:|---:|---:|---:|
| DuckDuckGo 4 profiles x 1 | 8 | 80/80 | 116.121 ms | 189.741 ms |
| Baidu 6 profiles x 1 | 8 | 48/48 | 89.942 ms | 148.924 ms |
| Bing 3 profiles x 1 | 6 | 18/18 | 603.044 ms | 1361.603 ms |
| Brave 3 profiles x 1 | 6 | 18/18 | 599.137 ms | 1825.674 ms |

这些结果隔离了公网反爬波动，不构成生产 SLA。原始完整报告保留在工作区 [Query and Fetch Benchmark](../../../docs/benchmarks/2026-07-15-query-fetch.md)。

SQLite strict trace 已在本次重构中删除，路由观测改为结构化日志，不再存在逐事件 fsync 对请求延迟的影响。

## Recommended initial Deployment resources

新 POST 接口使用 DuckDuckGo、并发 8、唯一 query 的本机采样中，Search 进程峰值约 `26,256 KiB RSS`，采样 CPU 峰值约 `1.3%`；双实例使用同一 cursor key 的跨 Pod 模拟验证成功，实例 A 生成的 cursor 可由实例 B 继续读取下一页。

该采样不包含 Bing/Brave Chromium Provider 的完整进程树，因此 Deployment 仍使用保守起点：requests `500m/1Gi`，limits `2 CPU/4Gi`。生产定值前还需在目标 Linux 容器环境复测 Browser Provider。

## Micro benchmark

```bash
go test ./internal/benchmark -run '^$' -bench . -benchmem -count=3
```
