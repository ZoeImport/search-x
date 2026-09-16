# 商用 Web Search 与 URL Read/Extract API 设计对比

> 调研日期：2026-07-15（Asia/Shanghai）
>
> 范围：仅使用厂商官方 API Reference、官方产品文档和官方变更记录；未用二手评测。文档描述的是公开契约，不推断未公开的内部实现。

## 1. 结论摘要

主流产品普遍把“搜索”和“按 URL 读取/提取”设计为独立 endpoint，但 Exa、Tavily、Firecrawl 又允许 Search 请求可选地附带正文或抓取参数。这说明商业 API 常采用“两层产品面”：稳定的原子 Search/Read 能力用于组合，同时提供减少客户端往返的便捷组合能力。[Exa Search](https://exa.ai/docs/reference/search) [Exa Contents](https://exa.ai/docs/reference/get-contents) [Tavily Search](https://docs.tavily.com/documentation/api-reference/endpoint/search) [Tavily Extract](https://docs.tavily.com/documentation/api-reference/endpoint/extract) [Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search) [Firecrawl Scrape](https://docs.firecrawl.dev/api-reference/endpoint/scrape)

对本项目而言，Search 与 Read 要能够独立仓库、独立部署且禁止互相依赖，因此第一版只公开两个原子接口是合理的。将来若市场要求“一次搜索并取正文”，应由 API Gateway/BFF 或独立 orchestration 产品实现，不能反向让 Search app import Read app。

商业化接口最值得借鉴的共同点是：稳定 `request_id`、逐结果来源 URL、显式 usage/cost、批量读取的部分成功模型、可枚举错误码、严格的字段默认值与上限。最不应照抄的是厂商历史包袱，例如同一选项同时接受 boolean/string、把大量浏览器底层参数直接暴露给普通调用方，或者把 offset 当结果条数而实际表示“页号”。

## 2. 横向比较

| 产品 | Search / Read 分离 | Search 可附带正文 | Read 批量/异步 | 分页 | 请求追踪与计费 | 来源与格式 |
|---|---|---|---|---|---|---|
| Exa | `POST /search`、`POST /contents` | `contents` 可请求 text/highlights/summary 等 | `/contents` 接受 `urls[]`；公开参考页展示同步返回及逐 URL `statuses` | Search 以 `numResults` 限制，公开参考页未定义 cursor/offset | `requestId`、`costDollars` | 每项有 `url`、title、author、publishedDate；正文、highlights、summary 可选 |
| Tavily | `POST /search`、`POST /extract` | `include_raw_content` 可返回 markdown/text | `/extract` 接受单 URL 或数组（文档错误示例说明最多 20）；同步并返回 `results` 与 `failed_results` | Search 以 `max_results` 限制，公开参考页未定义 cursor/offset | `request_id`、`response_time`，`include_usage` 控制 `usage.credits` | 每项保留 `url`；Search 返回 relevance `score`，Extract 支持 markdown/text |
| Firecrawl v2 | `POST /v2/search`、`POST /v2/scrape` | `scrapeOptions` 可给结果附加正文/截图等 | `POST /v2/batch/scrape` 创建批任务，另有状态、取消、错误 endpoint 与 webhook | Search 以 `limit` 限制，公开参考页未定义 cursor/offset | Search 返回 `id`、`creditsUsed`；通用 HTTP 状态与产品错误码 | 结果含 URL/title/description；Scrape 支持 markdown、HTML、截图、links、JSON 等丰富格式 |
| Jina | `s.jina.ai` Search、`r.jina.ai` Reader | Search 返回 LLM-friendly 搜索结果内容 | 官方产品页公开 GET/POST；未在本次官方参考中确认稳定批任务契约 | 未确认公开 cursor/offset 契约 | 按 token 计量并按 API key/IP 限流 | Reader 以目标 URL 为 provenance，可返回适合 LLM 的文本/Markdown，支持 PDF |
| Brave | Web Search 独立；不是通用 URL Reader 产品 | Web Search 返回 snippet/额外 snippet，不等价于任意 URL 全文读取 | 无通用 URL Read 批处理 | `count` + `offset`（offset 是从 0 开始的页号，最大 9），并有 `query.more_results_available` | API key 与套餐计费；公开响应模型重点是结构化 result types | `web.results[].url` 等；web/news/video/location 等结果分区及 mixed 排序 |

## 3. 各产品公开契约

### 3.1 Exa Search / Contents

Exa 的 `POST https://api.exa.ai/search` 接受 `query`、domain/date filters、`numResults`、search `type`、`contents` 等。`numResults` 默认 10，公开上限 100；`contents` 允许在搜索结果中直接带 text、highlights、summary 等内容。[官方 Search Reference](https://exa.ai/docs/reference/search)

Search 响应以 `results[]` 为主体，每项包含 `title`、`url`、`publishedDate`、`author`、`id` 等来源元数据；顶层有唯一 `requestId`，并可返回 `costDollars` 估算分解。官方同时标记了 `context`、`resolvedSearchType` 与 crawl-date filters 等 deprecated 字段，说明“字段级废弃 + changelog”是其兼容演进方式。[官方 Search Reference](https://exa.ai/docs/reference/search) [官方 Changelog](https://exa.ai/docs/changelog)

`POST https://api.exa.ai/contents` 的核心输入是 `urls[]`，用于批量取得完整页面、摘要和元数据；官方说明优先即时返回缓存，未缓存时自动 live crawl fallback。响应包含 `requestId`、`results[]`、逐 URL `statuses[]`（如 `status: success`、`source: cached`）及 `costDollars`，因此批量读取可以表达来源与每项状态，而不是只给一个全局成功布尔值。[官方 Contents Reference](https://exa.ai/docs/reference/get-contents)

值得借鉴：请求级 ID、成本元数据、逐 URL 状态、字段级 deprecation。需要警惕：Search 同时承担内容增强，若直接照搬会破坏本项目物理隔离目标。

### 3.2 Tavily Search / Extract

Tavily 使用 `POST https://api.tavily.com/search`。请求包括 `query`、`search_depth`、`max_results`、topic/time/domain filters，以及 `include_answer`、`include_raw_content`、`include_images`、`include_usage` 等能力开关。`include_raw_content` 接受 `false`、`true`/`markdown` 或 `text`，响应每项有 `title`、`url`、`content`、`score`、可选 `raw_content`，顶层有 `response_time`、`request_id` 和可选 `usage.credits`。[官方 Search Reference](https://docs.tavily.com/documentation/api-reference/endpoint/search)

`POST https://api.tavily.com/extract` 接受一个 URL 或 URL 数组，并支持 `query` 对提取片段重排、`chunks_per_source`、`extract_depth`、`format: markdown|text`、timeout 与 `include_usage`。响应把成功项放入 `results[]`、失败项放入 `failed_results[]`，并返回 `response_time`、`request_id` 和可选 usage。官方 400 示例明确给出最多 20 个 URL。[官方 Extract Reference](https://docs.tavily.com/documentation/api-reference/endpoint/extract)

Tavily 的错误体采用 `detail.error`，并列出 400、401、429、432、433、500；其中套餐额度与 pay-as-you-go 上限使用自定义 432/433。对外 API 可以借鉴“额度耗尽与限流分开”，但更适合使用标准 402/403/429 并在 body 内放稳定业务 code，避免客户端依赖非标准状态码。[官方 Search Reference](https://docs.tavily.com/documentation/api-reference/endpoint/search)

值得借鉴：部分成功数组、请求 ID、响应耗时、usage opt-in、明确的参数范围。需要警惕：同一字段兼容 boolean 与 enum string 会提高 SDK 类型与文档成本。

### 3.3 Firecrawl Search / Scrape

Firecrawl v2 用 `POST https://api.firecrawl.dev/v2/search`，请求有 `query`、`limit`、sources/categories、domain/time/location filters、timeout 与 `scrapeOptions`。不提供 `scrapeOptions` 时结果主要是 `url/title/description`；提供后可在同一次 Search 中获取 markdown、HTML、links、截图等内容。响应顶层有 `success`、按来源分类的 `data`、可选 `warning`、job/request 标识 `id` 与 `creditsUsed`。[官方 Search Reference](https://docs.firecrawl.dev/api-reference/endpoint/search)

单 URL 使用 `POST https://api.firecrawl.dev/v2/scrape`。它暴露的抓取控制非常丰富：`formats`、main-content filter、include/exclude tags、cache age、headers、wait、mobile、PDF parser、browser actions、location、proxy、广告阻断、PII/威胁保护和 zero-data-retention 等。[官方 Scrape Reference](https://docs.firecrawl.dev/api-reference/endpoint/scrape)

批量使用 `POST /v2/batch/scrape`，返回 job 后配套 `GET` 状态、`DELETE` 取消、错误列表和 webhook；请求支持 `urls[]` 与 `maxConcurrency`。这是一种清晰的同步单项 + 异步批量边界，避免单个 HTTP 请求被任意数量 URL 长时间占用。[官方 Batch Scrape Reference](https://docs.firecrawl.dev/api-reference/endpoint/batch-scrape) [官方 Batch Status Reference](https://docs.firecrawl.dev/api-reference/endpoint/batch-scrape-status) [官方 Batch Webhook](https://docs.firecrawl.dev/webhooks/events/batch-scrape)

Firecrawl 使用常规 2xx/4xx/5xx，同时维护产品级错误代码，例如 scrape 的 URL、超时、token usage 与 ZDR 冲突类错误。[官方 API Introduction](https://docs.firecrawl.dev/api-reference/introduction) [官方 Errors](https://docs.firecrawl.dev/api-reference/errors)

值得借鉴：单项同步、批量异步、取消/状态/webhook 完整闭环，以及显式 `creditsUsed`。需要警惕：把大量底层浏览器参数放进第一版公开接口会迅速锁死实现。

### 3.4 Jina Reader / Search

Jina 明确用不同 host 表达两个原子能力：`r.jina.ai` 读取目标 URL，`s.jina.ai` 搜索 Web 并返回适合 LLM 消费的结果；官方产品页同时列出 GET/POST。Reader 的最简调用是把目标 URL 拼接到 `https://r.jina.ai/` 后，产品页也提供 browser engine、内容格式、移除图片、执行 JavaScript 等选项，并明确支持 PDF。[官方 Reader](https://jina.ai/reader/)

Jina 的商业计量以 token 为核心：Reader 按输出 token，Search 每次有固定 token 起步量；限流按 IP/API key，并区分无 key、free、paid、premium 档位。当前官方产品页适合验证能力与计量，但其公开页面不像 Exa/Tavily/Firecrawl 的 OpenAPI reference 那样完整定义 JSON schema、分页与错误 envelope，因此本项目不应据此臆造 Jina 的 cursor、批量或 request-id 行为。[官方 API 与 Rate Limit 表](https://jina.ai/embeddings/)

值得借鉴：Search/Read 的产品边界极其清楚、URL-to-content 使用简单。需要警惕：把 URL 编进路径适合便捷工具，不适合作为本项目唯一商用 POST JSON 契约，因为日志、网关规则和 URL 长度控制更麻烦。

### 3.5 Brave Search

Brave Web Search 是 `GET https://api.search.brave.com/res/v1/web/search`（官方也提供 POST reference），以 `X-Subscription-Token` 鉴权。请求支持 query、country/language、freshness、safesearch、result filter、`count` 与 `offset`。`count` 最大 20；`offset` 是从 0 开始要跳过的“结果页数”而非结果条数，最大 9，官方提示分页间可能重叠，应读取 `query.more_results_available` 决定是否继续。[官方 Getting Started](https://api-dashboard.search.brave.com/app/documentation/web-search/get-started) [官方 Web Search Reference](https://api-dashboard.search.brave.com/api-reference/web/search/get)

响应按 `web`、`news`、`videos`、`locations`、`discussions`、`faq`、`infobox` 等类型分区，并可用 `mixed` 表达跨类型首选排序；每条 Web 结果保留 URL/title/description 等 provenance。Brave 不是通用“输入任意 URL 并提取全文”的 Reader API，因此只能作为 Search schema 与分页设计参考。[官方 Web Search Reference](https://api-dashboard.search.brave.com/api-reference/web/search/get)

值得借鉴：显式多类型结果与 `more_results_available`。需要警惕：offset 命名与实际“页号”语义不直观，且有限 offset 不适合作为可扩展的深分页模型。

## 4. 对本项目公开商用契约的建议

### 4.1 Endpoint 与版本

第一版建议保持：

```text
POST /v1/websearch
POST /v1/webfetch
```

公共测试路由与集群内 Service 使用同一个直接 JSON 契约，避免维护额外网关包装。版本演进应维护 changelog、OpenAPI、deprecation date 与 sunset header。Exa 的公开参考中已经展示字段级 deprecated，这是值得采用的演进信号。[Exa Search Reference](https://exa.ai/docs/reference/search)

### 4.2 Search request

```json
{
  "query": "golang structured logging",
  "limit": 10,
  "cursor": null,
  "filters": {
    "include_domains": ["go.dev"],
    "exclude_domains": [],
    "published_after": "2026-01-01T00:00:00Z",
    "published_before": null,
    "language": "en",
    "region": "US",
    "safe_search": "moderate"
  },
  "routing": {
    "providers": ["baidu", "bing", "brave", "duckduckgo"]
  }
}
```

公开契约应使用 opaque cursor，不直接承诺内部 provider 的 page/offset。即使第一版底层还只能翻页，也可以返回签名 cursor；未来换 provider、去重或融合排序时客户端无需改变。`limit` 建议默认 10、上限 50。filters/options 用对象预留扩展空间，但不要允许未知字段静默生效；未知字段应返回 validation error，避免拼写错误被忽略。

不要把 `refresh` 作为普通商用客户的默认能力，它可能绕过缓存并放大成本。若确需提供，应进入受权限控制的 `cache.mode: default|bypass`，计费与限流策略单独定义。

### 4.3 Search response

```json
{
  "request_id": "req_01J...",
  "results": [
    {
      "id": "res_...",
      "url": "https://go.dev/blog/slog",
      "title": "Structured Logging with slog",
      "snippet": "...",
      "published_at": "2023-08-22T00:00:00Z",
      "language": "en",
      "source": {
        "domain": "go.dev"
      }
    }
  ],
  "page": {
    "next_cursor": "cur_...",
    "has_more": true
  },
  "usage": {
    "units": 1
  },
  "warnings": []
}
```

`url` 是必须存在的 provenance；`id` 只能用于本系统结果关联，不能替代 URL。不要承诺 provider 原始 rank 或内部 profile。`usage` 从第一版就保留稳定对象，即使统计由上层业务实现，也应允许网关注入或服务返回；避免将来从无到有改变计费可观察性。厂商实践分别有 Exa `costDollars`、Tavily `usage.credits`、Firecrawl `creditsUsed`。[Exa Search](https://exa.ai/docs/reference/search) [Tavily Search](https://docs.tavily.com/documentation/api-reference/endpoint/search) [Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search)

### 4.4 Read request

```json
{
  "url": "https://example.com/article",
  "output": {
    "format": "markdown",
    "max_chars": 30000
  },
  "render": {
    "mode": "auto"
  }
}
```

第一版只支持单 URL 同步读取。`format` 用稳定 enum：`markdown|text`；未来 HTML、links、screenshot、structured JSON 可新增 enum 或新对象，不要让 boolean 与 string 共用同一字段类型。`render.mode` 只暴露 `auto|http|browser` 是否开放需要商业策略决定；默认 `auto`，站点 adapter 自行选择。headers/cookies/custom JavaScript/proxy 等高风险能力不进入通用第一版。

验证码识别后返回 typed error，不尝试求解。未来 challenge handler 仍属于内部策略扩展，不需要改变公开 request；若以后允许人工/第三方挑战处理，应单独设计异步 session API，不能给现有同步 `/read` 悄悄增加不可预测的长等待。

### 4.5 Read response 与部分失败

```json
{
  "request_id": "req_01J...",
  "document": {
    "url": "https://example.com/article",
    "final_url": "https://www.example.com/article",
    "title": "Article title",
    "content": "# Article title\n...",
    "format": "markdown",
    "content_type": "text/html",
    "language": "en",
    "retrieved_at": "2026-07-15T12:00:00Z"
  },
  "meta": {
    "cached": false,
    "transport": "browser",
    "truncated": false
  },
  "usage": {
    "units": 1
  },
  "warnings": []
}
```

`url` 与 `final_url` 必须分开，便于调用方审计重定向后的真实来源。`transport` 可保留为低基数稳定 enum，但不要暴露 profile、浏览器实例或内部 adapter 名。正文被 `max_chars` 截断时不能静默，必须 `meta.truncated=true` 并可增加 warning。

若后续增加批量，建议另设 `POST /v1/webfetch-batches`，返回 `202` 与 job id，再提供 `GET /v1/webfetch-batches/{id}`、取消与 webhook；不要把 `url` 改成 `string|string[]`，这会破坏强类型 SDK。Firecrawl 的 batch scrape 生命周期是更适合商用的参考；若只做小批同步，则应像 Tavily 一样明确 `results[]` 与 `failed_results[]`，不能因一个 URL 失败让其他成功结果丢失。[Firecrawl Batch Scrape](https://docs.firecrawl.dev/api-reference/endpoint/batch-scrape) [Tavily Extract](https://docs.tavily.com/documentation/api-reference/endpoint/extract)

### 4.6 统一错误模型

建议采用 RFC 9457 Problem Details 的 `application/problem+json` 思路，并增加稳定 `code` 与 `request_id`：

```json
{
  "type": "https://api.example.com/problems/captcha-required",
  "title": "Captcha required",
  "status": 422,
  "code": "captcha_required",
  "detail": "The target page requires an interactive challenge.",
  "request_id": "req_01J...",
  "retryable": false
}
```

建议基础映射：400 `invalid_request`，401 `unauthorized`，403 `forbidden`，404 `not_found`（仅用于本系统资源），409 `conflict`，422 `unsupported_content`/`captcha_required`/`login_required`，429 `rate_limited`，502 `upstream_failed`，503 `temporarily_unavailable`，504 `deadline_exceeded`。SSRF 被拒绝建议 422 或 403，公开 detail 不回显解析出的内网 IP。每个错误响应及成功响应都包含同一个 `request_id`，HTTP header 同时返回 `X-Request-Id`。

`retryable` 不能只按 HTTP status 推导：captcha 当前不自动处理，通常不应机器重试；provider 503 或 deadline 才可能重试。若返回 429，应提供标准 `Retry-After`。错误 `code` 一旦公开不可复用为其他语义。

## 5. 必须在 OpenAPI 与商用文档中写死的约束

- 每个字符串、数组和数字的 min/max/default，以及未知字段处理方式。
- 时间统一 RFC 3339 UTC；国家用 ISO 3166-1 alpha-2，语言建议 BCP 47。
- URL scheme 只允许 HTTP/HTTPS；重定向次数、响应体上限、超时和支持的 MIME。
- Search 排名是否稳定、分页是否可能重复；即使用 cursor，也要提示 Web 索引变化可能带来跨页重复。
- Read 是 best-effort，不承诺绕过登录、付费墙、验证码或站点反自动化措施。
- cache 语义、内容新鲜度和 `retrieved_at`；是否可能返回过期内容必须可观察。
- usage 的单位、失败是否计费、批量部分成功如何计费。Tavily 明确按成功 URL 计 Extract credits，Firecrawl/Exa 也在响应暴露成本/credits，说明计费可观察性应属于契约而非后台隐含规则。[Tavily Credits](https://docs.tavily.com/documentation/api-credits) [Firecrawl Search](https://docs.firecrawl.dev/api-reference/endpoint/search) [Exa Search](https://exa.ai/docs/reference/search)
- rate-limit headers、429 与 Retry-After；API key 权限范围、轮换与撤销。
- 数据保留、日志、ZDR/隐私策略与内容版权责任边界。
- SDK 生成所需的稳定 schema；不要用 `any`、同字段多类型或依赖 undocumented fields。

## 6. 最终建议

本项目第一版应坚持 Search/Read 两个原子 POST JSON API，独立部署、独立配置、独立缓存，不公开 Search+Read 聚合。Schema 设计上采用 Exa/Tavily 的 `request_id + provenance + usage`，采用 Tavily 的批量部分成功思想与 Firecrawl 的异步批任务边界，分页则优先 opaque cursor 而不是 Brave 式有限 page-offset。

接口层现在就应预留 `usage`、`warnings`、`page`/cursor、typed error 与 `request_id`；这些是商业化后很难无痛补上的横切契约。浏览器 profile、站点 adapter、验证码 handler、provider attempts 和原始调试产物属于内部实现，不应进入公开响应。未来扩展内容类型、批量与 challenge 处理时，通过新增 enum、optional object 或独立资源 endpoint 演进，而不是修改现有字段类型。
