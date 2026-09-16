# Web Search / Fetch 商用 API 契约兼容性审查

> 调研日期：2026-07-16（Asia/Shanghai）
>
> 比较对象：`websearch/openapi/openapi.yaml`、`webfetch/openapi/openapi.yaml`
>
> 来源范围：仅 Exa、Tavily、Firecrawl 的官方 API Reference。

## 结论

当前 Search 与 Read schema 的总体方向是合理的：请求使用直接业务 JSON、Search 返回 `results[]`、单 URL Read 返回一个结构化文档，并且统一提供 `request_id`。这些都与主流商用 API 的核心形态接近，不需要为了接入 API 市场而复制一层 `request` / `response`；该 envelope 应只存在于 API 市场协议层。

审查提出的阻塞项现已按以下边界实现：

1. `SearchResult.id` 由本服务根据“实际返回的 URL 原字符串”生成；相同字符串稳定，但不声明 canonical page identity。这样不要求上游提供 ID，也不误导为规范 URL。[Exa Search](https://exa.ai/docs/reference/search) [Tavily Search](https://docs.tavily.com/documentation/api-reference/endpoint/search) [Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search)
2. 当前四个 Provider `baidu`、`bing`、`brave`、`duckduckgo` 均已有实现并默认启用。公开请求改为 `routing.providers` 有序子集，默认严格 fallback 顺序为 `baidu -> bing -> brave -> duckduckgo`；cursor 分页固定首屏实际 Provider。

此外已定义必填 `usage.units`，明确 `content_length` 为 Unicode code point，并为 WebFetch 增加必填 `content_type`、`status_code`。

## 官方契约横向比较

| 能力 | Exa | Tavily | Firecrawl v2 | 本项目当前契约 |
|---|---|---|---|---|
| Search endpoint | `POST /search` | `POST /search` | `POST /v2/search` | `POST /v1/websearch` |
| Search 请求主体 | 直接 JSON；`query`、`numResults`、domain/date filters、可选 `contents` | 直接 JSON；`query`、`max_results`、domain/date/topic filters、可选 raw content | 直接 JSON；`query`、`limit`、domain/location/source filters、可选 `scrapeOptions` | 直接 JSON；`query`、`limit`、`cursor`、`routing`、`filters.region` |
| Search 结果 | `results[]`，含 URL/title/id/作者/发布时间；顶层 `requestId`、`costDollars` | `results[]`，含 URL/title/content/score；顶层 `query`、`request_id`、`response_time`、可选 usage | `data.web[]`，含 URL/title/description；顶层 `id`、`creditsUsed` | `results[]`，含 URL/title/snippet/rank/id；顶层 `request_id`、`query`、`page`、`meta` |
| URL 内容 endpoint | `POST /contents` | `POST /extract` | `POST /v2/scrape` | `POST /v1/webfetch` |
| URL 输入 | `urls[]` / `ids[]`，1..100 | `urls` 可为 string 或 array，最多 20 | 单个 `url` | 单个 `url` |
| 内容响应 | `results[]` + 每 URL `statuses[]` + `requestId` | `results[]` + `failed_results[]` + `request_id` | `success` + `data`，正文按 formats 返回 | `request_id` + 单个 `document` + `meta` + `warnings` |

官方依据：[Exa Search](https://exa.ai/docs/reference/search)、[Exa Contents](https://exa.ai/docs/reference/contents-api-guide-for-coding-agents)、[Tavily Search](https://docs.tavily.com/documentation/api-reference/endpoint/search)、[Tavily Extract](https://docs.tavily.com/documentation/api-reference/endpoint/extract)、[Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search)、[Firecrawl Scrape](https://docs.firecrawl.dev/api-reference/endpoint/scrape)。

## 兼容性判断

### Search request

- `query` + `limit` 是三家厂商都能自然映射的最小公分母。当前默认 10、上限 20 偏保守但友好；Exa/Firecrawl 的公开上限为 100，Tavily 的 `max_results` 上限为 20，因此本项目上限 20 无需修改。[Exa Search](https://exa.ai/docs/reference/search) [Tavily Search](https://docs.tavily.com/documentation/api-reference/endpoint/search) [Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search)
- `cursor` 不是三家 Search 的共同公开能力。保留它可以作为本项目的 provider-neutral 抽象，但只有真正实现跨 provider 的 opaque continuation token 后才能对外承诺；否则第一版应暂时移除，或明确当前不返回 `next_cursor`。
- 当前过滤条件只有 `region`。三家均提供 domain filter，Exa/Tavily 还提供发布时间过滤，Firecrawl 提供 time-based search。建议后续增加 `include_domains`、`exclude_domains`、`published_after`、`published_before`，但只有 Baidu/Bing 两端能给出一致、可验证语义时再开放，不应在 adapter 中静默忽略。[Exa Search](https://exa.ai/docs/reference/search) [Tavily Search](https://docs.tavily.com/documentation/api-reference/endpoint/search) [Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search)

### Search response

- `request_id`、`query`、`results[]`、URL、title、snippet 是友好的稳定核心。`rank` 可以由聚合层按最终顺序可靠生成。
- 建议新增 optional `published_at`、`author`、`favicon`。它们在 Exa、Tavily 或 Firecrawl 中已有先例，但必须 optional，因为 Baidu/Bing 并不保证都有值。
- 不建议公开 provider 原始 score。Tavily 有 relevance `score`，Exa/Firecrawl 的普通结果没有同语义字段；多 provider 分值不可直接比较。
- `usage` 与 `warnings` 当前是无明确字段的 object，生成 SDK 时价值有限。建议定义稳定最小结构，例如 `usage.units`；warning 至少定义 `code`、`message`。Exa 暴露 `costDollars`，Tavily 暴露可选 credits usage，Firecrawl 暴露 `creditsUsed`，说明成本可观察性是成熟商用接口的共同做法。[Exa Search](https://exa.ai/docs/reference/search) [Tavily Search](https://docs.tavily.com/documentation/api-reference/endpoint/search) [Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search)

### Fetch request / response

- 当前单 URL、同步 `/v1/webfetch` 与 Firecrawl `/v2/scrape` 的产品边界一致；Exa/Tavily 支持批量并不意味着当前版本必须批量化。本期保持单 URL 能降低超时和部分失败复杂度。[Exa Contents](https://exa.ai/docs/reference/contents-api-guide-for-coding-agents) [Tavily Extract](https://docs.tavily.com/documentation/api-reference/endpoint/extract) [Firecrawl Scrape](https://docs.firecrawl.dev/api-reference/endpoint/scrape)
- `output.format: markdown|text` 与 Tavily Extract 一致；`max_chars` 是本项目有价值的响应预算控制，可以保留。
- 单文档响应使用 `document` 而不是 `results[]` 是合理且强类型的，不需要为了表面相似改成数组。未来若增加批量，应新增 batch endpoint 或新版本，并像 Exa/Tavily 一样表达逐 URL 成功与失败，不能把现有 `url: string` 改成 `string|string[]`。
- 建议为 `document` 增加 optional `content_type` 和 `status_code`；Firecrawl 在 `metadata` 中公开这两项，有助于客户判断实际读取内容。[Firecrawl Scrape](https://docs.firecrawl.dev/api-reference/endpoint/scrape)
- `published_at` 当前只是 string。由于供应商页面日期可能不完整或不可靠，不建议强制 `date-time`；应在描述中写清楚“能规范化时使用 RFC 3339，否则省略”。
- `meta.content_length` 必须注明单位是 UTF-8 bytes、Unicode code points，还是实际返回字符数；它应与 `max_chars` 的计算口径一致。

## API 市场接入边界

Exa、Tavily、Firecrawl 的 endpoint 都接收直接业务 JSON，并未使用 API 市场式的 `{ "request": ... }` body。因而本项目本地接口保持直接请求最符合行业惯例：

```json
{"query":"golang","limit":10}
```

API 市场的 Executor 负责消费 MQ 中的业务 request、直接调用本地 `websearch` / `webfetch` HTTP endpoint，再把本地业务响应写入 `ProcessResponse.Response`。API Key、余额、权限、计费 envelope 与 MQ 配置仍属于 `apigateway` / Runner，不进入本地接口 schema。

## 实施优先级

1. **已完成**：定义 ID 稳定性、`usage.units`、Unicode 字符口径、cursor 绑定和 WebFetch MIME/status 元数据。
2. **已完成**：四 Provider 默认有序 fallback 与请求级有序子链。
3. **兼容增强**：增加 optional `published_at`、`author`、`favicon`。
4. **后续版本**：domain/date filters、批量 fetch、Search+Fetch 组合能力；这些不阻塞本次 API 市场 Executor 接入。
