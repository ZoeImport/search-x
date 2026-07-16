# CLAUDE.md

## 项目

该 workspace 包含三个独立 Go module：`websearch`、`webfetch`、`runtime`。WebSearch 与 WebFetch 可独立部署且不互相 import；根目录 `go.work` 只用于本地联合开发。

权威入口是根目录 [`README.md`](README.md)，公开 contract 是 [`docs/api-reference.md`](docs/api-reference.md) 与两个 OpenAPI 文件。

## 命令

```bash
make run              # 并行启动 WebSearch、WebFetch 与 Demo
make run-websearch
make run-webfetch
make run-demo
make check            # test + race + vet + build
```

单服务运行都应显式传入 YAML：

```bash
cd websearch && go run ./cmd/websearch-api -config config.yaml
cd webfetch && go run ./cmd/webfetch-api -config config.yaml
```

## 架构

- `websearch/internal/api/httpapi`：`POST /v1/websearch`、严格 JSON、timeout、ordered provider chain、cursor。
- `websearch/internal/app`：cache、singleflight、动态有序 Provider chain。
- `websearch/internal/provider`：Baidu、Bing、Brave、DuckDuckGo 实现和失败降级。
- `websearch/internal/profilepool`、`routing`：Provider profile 的容量和健康管理。
- `webfetch/internal/api/httpapi`：`POST /v1/webfetch`。
- `webfetch/internal/app`、`fetch`：SSRF policy → HTTP/browser → detect → extract → quality → convert → cache。
- `runtime`：环境变量、HTTP problem、logging 和公共 request timeout parser。

## 约束

- 配置优先级固定为代码默认值 < `config.yaml` < 环境变量；YAML 未知字段必须拒绝启动。
- 环境变量分别使用 `WEBSEARCH_`、`WEBFETCH_` 前缀，不保留旧别名。
- WebSearch 默认链是 `baidu → bing → brave → duckduckgo`；请求可用 `routing.providers` 指定有序已启用子集。cursor 页固定首屏实际 Provider。
- 两个接口的 `timeout` 只接受整数 `ms`/`s`，是整个请求预算。
- WebFetch SSRF policy 必须拒绝私网、回环、link-local、CGNAT 和 metadata 地址；`WEBFETCH_HOST_ALLOWLIST` 只用于受控本地测试。
- 修改公开字段时同步更新 OpenAPI、统一 API reference、README、curl/Python/Go examples 和 Demo。
