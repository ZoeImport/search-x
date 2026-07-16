# WebFetch API

独立部署的单 URL 正文读取服务。它对公网 HTTP/HTTPS URL 执行 SSRF 校验，优先 HTTP 获取，必要时使用 Chromium 渲染，然后提取 HTML 或纯文本正文。

## API Market 测试接口

```http
POST https://tapi.insmtx.com/v6/se/general/fetch
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

```json
{"request":{"url":"https://example.com/article","timeout":"20s","output":{"format":"markdown","max_chars":30000}}}
```

业务响应位于 API Market 返回的 `response` 字段。完整契约见 [OpenAPI](openapi/openapi.yaml)。第一版只支持单个 URL、`text/html` 和 `text/plain`，不支持 PDF、Office、图片、OCR、批量读取、登录、付费墙或验证码求解。

Executor 调用的内部后端路由仍为 `POST http://127.0.0.1:8081/v1/webfetch`，它接收不带 `request` 外层的业务 JSON，不作为本地 curl 的默认测试入口。

## 运行

```bash
go run ./cmd/webfetch-api -config config.yaml
```

默认监听 `:8081`。

## 主要配置

```text
WEBFETCH_ADDR=:8081
WEBFETCH_REQUEST_TIMEOUT=20s
WEBFETCH_MAX_REQUEST_TIMEOUT=60s
WEBFETCH_HTTP_TIMEOUT=6s
WEBFETCH_BROWSER_ENABLED=true
WEBFETCH_BROWSER_TIMEOUT=12s
WEBFETCH_BROWSER_SLOTS=4
WEBFETCH_MAX_BODY_BYTES=5242880
WEBFETCH_CACHE_BYPASS=false
WEBFETCH_DIAGNOSTICS_ENABLED=false
WEBFETCH_ROBOTS_POLICY=ignore
WEBFETCH_LOG_FILE=./log/webfetch-api.log
WEBFETCH_LOG_LEVEL=info
WEBFETCH_LOG_STORE_URL_QUERY=false
WEBFETCH_CORS_ALLOWED_ORIGINS=http://127.0.0.1:8090
```

`WEBFETCH_ROBOTS_POLICY=respect` 是后续扩展点，当前配置该值会拒绝启动，避免产生已经执行 robots 检查的错误预期。日志默认移除 URL query、fragment 和凭据；只有后端显式设置 `WEBFETCH_LOG_STORE_URL_QUERY=true` 才保留 query，外部请求无法覆盖。

域名特化由两层接口预留：`SiteStrategyResolver` 按最长域名后缀及路径前缀选择策略，`SiteStrategy` 负责准备具体读取行为。当前仅启用不改变流程的 `GenericStrategy`；验证码只返回 `captcha_required`，不尝试求解。

配置优先级：代码默认值 < [`config.yaml`](config.yaml) < 环境变量。YAML 未知字段或错误类型会阻止启动。请求 `timeout` 只接受整数 `ms`/`s`，范围 100ms–配置上限（上限不超过 60s）。`max_chars` 与响应 `content_length` 都以 Unicode code point 计数；成功响应会返回上游 `content_type`、`status_code` 和固定 `usage.units=1`。

## 文档与示例

- [OpenAPI](openapi/openapi.yaml)
- [统一接口说明](../docs/api-reference.md)
- [curl](examples/curl/webfetch.sh)
- [Python](examples/python/webfetch.py)
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
go build ./cmd/webfetch-api
```

资源结果见 [resource baseline](docs/benchmarks/resource-baseline.md)。
