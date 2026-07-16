# WebSearch / WebFetch API Reference

本文件描述 API Market 测试环境的公开契约。请求必须使用 `Authorization: Bearer <API_KEY>`，业务参数放在 `request` 字段中，业务响应位于网关返回的 `response` 字段。

`127.0.0.1:8080/8081` 是 Executor 调用后端的内部地址，不作为本地 curl 的测试入口。

API Market 队列分别为 `se/general/search`、`se/general/fetch`，vhost 为 `apigw_se`。

完整机器可读定义：[`websearch/openapi/openapi.yaml`](../websearch/openapi/openapi.yaml)、[`webfetch/openapi/openapi.yaml`](../webfetch/openapi/openapi.yaml)。

## WebSearch

`POST https://tapi.insmtx.com/v6/se/general/search`

```json
{
  "request": {
    "query": "golang context package",
    "limit": 5,
    "timeout": "20s",
    "routing": {"providers": ["baidu", "bing"]},
    "filters": {"region": "CN"}
  }
}
```

| 字段 | 类型 | 约束 |
|---|---|---|
| `query` | string | 必填，去除多余空白后 1–256 个 Unicode 字符 |
| `limit` | integer | 可选，1–20，默认 10 |
| `timeout` | string | 可选，仅整数 `ms`/`s`；100ms–配置上限，上限最多 60s；覆盖 YAML 默认值 |
| `routing.providers` | string[] | 可选，有序、无重复的已启用 Provider 子集；默认 `baidu,bing,brave,duckduckgo` |
| `filters.region` | string | 可选，ISO 3166-1 alpha-2 提示 |
| `cursor` | string | 可选，上一页返回的 opaque cursor，最长 4096 |

第一页按 `routing.providers` 严格顺序失败降级，空结果也进入下一 Provider。cursor 同时绑定 query、limit 和完整有序链，并固定第一页实际成功的 Provider；分页不跨 Provider 降级。Provider visibility 为 `hidden` 时，结果、meta 与 warning 均不会泄漏 Provider 名。

业务成功响应固定包含 `usage: {"units": 1}`。`results[].id` 是“返回 URL 原字符串”的 hash：相同 URL 字符串得到相同 ID，但它不是 canonical page identity。下方展示的是 API Market 外层响应：

```json
{
  "code": 0,
  "message": "success",
  "response": {
    "request_id": "req_xxx",
    "query": "golang context package",
    "results": [{"id":"res_xxx","url":"https://go.dev/","title":"Go","snippet":"...","rank":1,"provider":"bing"}],
    "page": {"next_cursor":"cur_v1_xxx","has_more":true},
    "meta": {"cached":false,"took_ms":120,"provider":"bing"},
    "warnings": [],
    "usage": {"units":1}
  }
}
```

## WebFetch

`POST https://tapi.insmtx.com/v6/se/general/fetch`

```json
{
  "request": {
    "url": "https://go.dev/doc/",
    "timeout": "20s",
    "output": {"format":"markdown","max_chars":30000}
  }
}
```

| 字段 | 类型 | 约束 |
|---|---|---|
| `url` | string | 必填，公网 HTTP/HTTPS URL，最长 2048 bytes；受 SSRF policy 限制 |
| `timeout` | string | 可选，仅整数 `ms`/`s`；100ms–配置上限，上限最多 60s |
| `output.format` | string | `markdown` 或 `text`，默认 `markdown` |
| `output.max_chars` | integer | 1000–100000，默认 30000，以 Unicode code point 计数 |

响应 `meta.content_length` 同样是 Unicode code point 数。`document.content_type` 是标准化 MIME，`document.status_code` 是最终上游 HTTP status；成功固定计费 1 unit。

```json
{
  "code": 0,
  "message": "success",
  "response": {
    "request_id": "req_xxx",
    "document": {
      "url":"https://go.dev/doc/",
      "final_url":"https://go.dev/doc/",
      "title":"Documentation",
      "source_type":"html",
      "content_type":"text/html",
      "status_code":200,
      "content":"...",
      "format":"markdown",
      "retrieved_at":"2026-07-16T00:00:00Z"
    },
    "meta":{"cached":false,"transport":"http","truncated":false,"content_length":1234,"took_ms":80},
    "warnings":[],
    "usage":{"units":1}
  }
}
```

## 错误

内部服务使用 `application/problem+json`，包含 `status`、`code`、`detail`、`request_id`、`retryable` 和可选 `parameter`。Executor 会把它映射为 API Market 的 `code/message`。常见内部 HTTP status：参数错误 400，SSRF 拒绝 403，资源过大 413，不支持内容 415，无法提取 422，上游失败 502，不可用 503，超时 504。

API Market executor 映射为：本地 400/403/413/415 → `ProcessCodeParamError`；408/504 或 context deadline → `ProcessCodeTimeout`；包括 422/429 在内的其他上游失败 → `ProcessCodeExecuteError`。

## 可运行示例

- WebSearch：[curl](../websearch/examples/curl/websearch.sh) · [Python](../websearch/examples/python/websearch.py) · [Go](../websearch/examples/go/main.go)
- WebFetch：[curl](../webfetch/examples/curl/webfetch.sh) · [Python](../webfetch/examples/python/webfetch.py) · [Go](../webfetch/examples/go/main.go)
- [本地 Demo](../demo/README.md)
