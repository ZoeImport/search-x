# WebFetch API

WebFetch 是独立部署的单 URL 正文读取服务。它先执行 SSRF 校验，优先使用 HTTP 获取，必要时使用 Chromium 渲染，再提取 HTML 或纯文本正文。

```http
POST https://tapi.juxonmedia.com/v1/webfetch
Content-Type: application/json
```

```json
{"url":"https://example.com/","timeout":"20s","output":{"format":"markdown","max_chars":30000}}
```

JXX 集群内地址为 `http://webfetch-api.jxx.svc.cluster.local:8081/v1/webfetch`。完整字段、错误与重试契约见 [统一接口说明](../docs/api-reference.md) 和 [OpenAPI](openapi/openapi.yaml)。

## 运行

```bash
go run ./cmd/webfetch-api -config config.yaml
```

默认监听 `:8081`。配置优先级固定为代码默认值、`config.yaml`、环境变量。YAML 未知字段会阻止启动；环境变量使用 `WEBFETCH_` 前缀。

第一版只支持单个公网 HTTP/HTTPS URL、`text/html` 和 `text/plain`。私网、回环、link-local、CGNAT 与 metadata 地址在初始解析和重定向阶段都会被拒绝。不支持 PDF、Office、图片、OCR、登录、付费墙或验证码求解。

## 验证

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/webfetch-api
```

- [curl](examples/curl/webfetch.sh)
- [Python](examples/python/webfetch.py)
- [Go](examples/go/main.go)
- [JXX 集成](../docs/jxx-integration.md)
