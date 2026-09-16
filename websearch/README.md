# WebSearch API

WebSearch 是独立部署的原子搜索服务，提供 Provider 路由、Profile Pool、缓存和 opaque cursor，不读取结果正文，也不依赖 WebFetch。

```http
POST https://tapi.juxonmedia.com/v1/websearch
Content-Type: application/json
```

```json
{"query":"golang","limit":10,"timeout":"20s","routing":{"providers":["brave","duckduckgo"]}}
```

JXX 集群内地址为 `http://websearch-api.jxx.svc.cluster.local:8080/v1/websearch`。完整字段、错误与重试契约见 [统一接口说明](../docs/api-reference.md) 和 [OpenAPI](openapi/openapi.yaml)。

## 运行

```bash
go run ./cmd/websearch-api -config config.yaml
```

默认监听 `:8080`。多副本必须通过同一个 Secret 提供 `WEBSEARCH_CURSOR_KEY`，确保任意 Pod 都能解析其他 Pod 生成的 cursor。

配置优先级固定为代码默认值、`config.yaml`、环境变量。YAML 未知字段会阻止启动。主要变量见 [`config.yaml`](config.yaml)，环境变量使用 `WEBSEARCH_` 前缀。

默认 Provider 链为 `baidu,bing,brave,duckduckgo`。`routing.providers` 可提供有序、无重复的已启用子集。第一页成功后，cursor 固定实际 Provider；后续页不跨 Provider 降级。

## 验证

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/websearch-api
```

- [curl](examples/curl/websearch.sh)
- [Python](examples/python/websearch.py)
- [Go](examples/go/main.go)
- [JXX 集成](../docs/jxx-integration.md)
