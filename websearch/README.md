# WebSearch API

独立部署的原子 Web Search 服务。它提供 Provider 路由、Profile Pool、缓存和 opaque cursor，不读取搜索结果正文，也不依赖 Read API。

## API Market 测试接口

```http
POST https://tapi.insmtx.com/v6/se/general/search
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

```json
{"request":{"query":"golang","limit":10,"timeout":"20s","routing":{"providers":["baidu","bing"]},"filters":{"region":"CN"}}}
```

业务响应位于 API Market 返回的 `response` 字段。完整契约见 [OpenAPI](openapi/openapi.yaml)。公开接口只支持单个 query；`limit` 默认 10，范围 1–20。下一页使用 `response.page.next_cursor`，客户端不解析 cursor。

Executor 调用的内部后端路由仍为 `POST http://127.0.0.1:8080/v1/websearch`，它接收不带 `request` 外层的业务 JSON，不作为本地 curl 的默认测试入口。

## 运行

```bash
go run ./cmd/websearch-api -config config.yaml
```

默认监听 `:8080`。生产多副本必须通过同一个 Secret 提供 `WEBSEARCH_CURSOR_KEY`，确保任意 Pod 都能解析其他 Pod 生成的 cursor。

## Provider 配置

```text
WEBSEARCH_ENABLED_PROVIDERS=baidu,bing,brave,duckduckgo
WEBSEARCH_ALLOW_REQUEST_PROVIDERS=true
WEBSEARCH_RESPONSE_PROVIDER_VISIBILITY=public
```

`routing.providers` 是有序、无重复的已启用 Provider 子集。服务严格按请求顺序降级；若请求未传则使用 YAML 中 `enabled_providers` 的默认顺序。第一页成功后 cursor 会同时绑定完整链并固定实际 Provider，后续页不跨 Provider 降级。`id` 仅对完全相同的返回 URL 字符串稳定，不表示规范化网页身份。

## 主要配置

```text
WEBSEARCH_ADDR=:8080
WEBSEARCH_REQUEST_TIMEOUT=20s
WEBSEARCH_MAX_REQUEST_TIMEOUT=60s
WEBSEARCH_CURSOR_TTL=15m
WEBSEARCH_CACHE_BYPASS=false
WEBSEARCH_DIAGNOSTICS_ENABLED=false
WEBSEARCH_LOG_STORE_QUERY=false
WEBSEARCH_LOG_FILE=./log/websearch-api.log
WEBSEARCH_LOG_LEVEL=info
WEBSEARCH_CORS_ALLOWED_ORIGINS=http://127.0.0.1:8090
```

日志同时写 stdout 和滚动文件。默认不记录完整 query。SQLite、spool 和共享 Profile 存储已移除；每个 Pod 使用自己的临时 Profile 和内存缓存。

配置优先级：代码默认值 < [`config.yaml`](config.yaml) < 环境变量。YAML 使用严格字段检查，未知字段或错误类型会阻止启动。请求中的 `timeout` 只接受整数 `ms`/`s`，范围 100ms–配置上限（上限不超过 60s），并覆盖 YAML 默认值。

## 文档与示例

- [OpenAPI](openapi/openapi.yaml)
- [统一接口说明](../docs/api-reference.md)
- [curl](examples/curl/websearch.sh)
- [Python](examples/python/websearch.py)
- [Go](examples/go/main.go)
- [本地 Demo](../demo/README.md)
- [Kubernetes](deploy/k8s/deployment.yaml)
- [售卖商契约调研](../docs/research/vendor-contract-compatibility-2026-07-16.md)

运行示例前：

```bash
export API_KEY='<测试环境 API Key>'
```

## 验证

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/websearch-api
```

资源结果见 [resource baseline](docs/benchmarks/resource-baseline.md)。
逐 Provider 实测见 [provider smoke test](docs/provider-smoke-test.md)。
