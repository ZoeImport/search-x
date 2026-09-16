# Search-X API Reference

Search-X 在 JX-LAN 测试环境提供两个独立 HTTP JSON 接口。公共测试入口由 `tapi.juxonmedia.com` 的 Traefik ingress 转发；JXX 生产代码应优先使用同 namespace 的 ClusterIP DNS，避免不必要地绕过 ingress。

当前服务自身不校验 Cookie 或 Bearer Token。调用方必须把它们视为 JXX 内部能力；公共 `tapi` 路由只用于受控测试，后续如需开放给不受信任客户端，应在边缘增加独立的鉴权、配额与审计。

所有请求都必须发送 `Content-Type: application/json`。请求体就是下文所示业务对象，不存在 `request` 外层；成功响应也不存在 `code` 或 `response` 外层。

## 地址

| 服务 | 公共测试地址 | JXX 集群内地址 |
| --- | --- | --- |
| WebSearch | `POST https://tapi.juxonmedia.com/v1/websearch` | `POST http://websearch-api.jxx.svc.cluster.local:8080/v1/websearch` |
| WebFetch | `POST https://tapi.juxonmedia.com/v1/webfetch` | `POST http://webfetch-api.jxx.svc.cluster.local:8081/v1/webfetch` |

请求 ID 由服务生成或透传 `X-Request-ID`，并在响应头 `X-Request-ID` 与 JSON 字段 `request_id` 中返回。JXX 应记录该值，用于跨服务排障。

## WebSearch

请求示例：

```json
{
  "query": "golang context package",
  "limit": 5,
  "timeout": "20s",
  "routing": {"providers": ["brave", "duckduckgo"]},
  "filters": {
    "region": "CN",
    "include_domains": ["go.dev"],
    "exclude_domains": ["example.com"]
  },
  "query_options": {
    "exact_phrases": ["context package"],
    "title_terms": ["documentation"],
    "file_types": ["html"]
  }
}
```

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `query` | string | 必填；规范化空白后 1–256 个 Unicode 字符 |
| `limit` | integer | 可选；1–20，默认 10 |
| `timeout` | string | 可选；整数 `ms` 或 `s`，100ms 到配置上限，最大 60s |
| `routing.providers` | string[] | 可选；有序、无重复的已启用 Provider 子集 |
| `filters.region` | string | 可选；ISO 3166-1 alpha-2 地区提示 |
| `filters.include_domains` | string[] | 可选；最多 20 个域名，匹配域名及其子域名 |
| `filters.exclude_domains` | string[] | 可选；最多 20 个域名，不能与 include 重复 |
| `query_options.*_terms` | string[] | 可选；每组最多 20 项 |
| `query_options.file_types` | string[] | 可选；最多 10 个扩展名 |
| `cursor` | string | 可选；上一页的 opaque cursor，最长 4096 字符 |

第一页按兼容 Provider 链严格顺序降级。Cursor 会绑定 query、filters、query options、limit 和 Provider 链，并固定第一页实际成功的 Provider；客户端必须把它当作不可解析的字符串。

成功响应示例：

```json
{
  "request_id": "req_xxx",
  "query": "golang context package",
  "results": [
    {
      "id": "res_xxx",
      "url": "https://go.dev/?utm_source=search",
      "canonical_url": "https://go.dev/",
      "domain": "go.dev",
      "title": "Go",
      "snippet": "...",
      "rank": 1,
      "provider": "brave"
    }
  ],
  "page": {"next_cursor": "cur_v2_xxx", "has_more": true},
  "meta": {"cached": false, "took_ms": 120, "provider": "brave"},
  "warnings": [],
  "usage": {"units": 1}
}
```

## WebFetch

请求示例：

```json
{
  "url": "https://go.dev/doc/",
  "timeout": "20s",
  "output": {"format": "markdown", "max_chars": 30000}
}
```

| 字段 | 类型 | 约束 |
| --- | --- | --- |
| `url` | string | 必填；公网 HTTP/HTTPS URL，最长 2048 bytes；受 SSRF policy 限制 |
| `timeout` | string | 可选；整数 `ms` 或 `s`，100ms 到配置上限，最大 60s |
| `output.format` | string | `markdown` 或 `text`，默认 `markdown` |
| `output.max_chars` | integer | 1000–100000，默认 30000，按 Unicode code point 计数 |

WebFetch 会拒绝私网、回环、link-local、CGNAT、云 metadata 地址，以及重定向后落入这些地址的请求。第一版只处理 `text/html` 与 `text/plain`，不支持 PDF、Office、图片、OCR、登录、付费墙或验证码求解。

成功响应示例：

```json
{
  "request_id": "req_xxx",
  "document": {
    "url": "https://go.dev/doc/",
    "final_url": "https://go.dev/doc/",
    "title": "Documentation",
    "source_type": "html",
    "content_type": "text/html",
    "status_code": 200,
    "content": "...",
    "format": "markdown",
    "retrieved_at": "2026-07-16T00:00:00Z"
  },
  "meta": {"cached": false, "transport": "http", "truncated": false, "content_length": 1234, "took_ms": 80},
  "warnings": [],
  "usage": {"units": 1}
}
```

## 错误与重试

错误使用 `application/problem+json`，包含 `status`、`code`、`detail`、`request_id`、`retryable` 和可选 `parameter`。常见状态码：参数错误 400，SSRF 拒绝 403，响应过大 413，不支持内容 415，无法提取 422，上游失败 502，暂时不可用 503，超时 504。

调用方只应在 `retryable: true` 时进行有界重试，并沿用自己的调用链 request ID。WebSearch 的 cursor 失败不能自动回退到其他 Provider；WebFetch 的超时也不能被解释为上游未收到请求。

机器可读契约见 [WebSearch OpenAPI](../websearch/openapi/openapi.yaml) 与 [WebFetch OpenAPI](../webfetch/openapi/openapi.yaml)。
