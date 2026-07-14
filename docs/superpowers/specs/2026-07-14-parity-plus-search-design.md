# Parity-Plus 自实现搜索设计

## 1. 状态与目标

本设计于 2026-07-14 获得用户确认，选择方案 C：在现有 Go Base Backend 上构建原生 Parity-Plus 搜索能力，并以 `mrkrsl/web-search-mcp` 作为最低效果基线。

完成标准不是“接口存在”或“单次请求成功”，而是在同一查询集、相同正文目标数、相同总超时和重复观测条件下，搜索成功率、可读正文满足率和相关性达到或超过 `web-search-mcp`。任何核心门槛未达到时，项目保持未完成并继续迭代。

### 1.1 目标

- 保留人类、普通 HTTP client 和 AI Agent 共用的稳定 Base Backend API。
- 提供不依赖付费 SERP API 的搜索结果和正文组合能力。
- 覆盖 `web-search-mcp` 已有的 Browser Bing、Browser Brave、HTTP DuckDuckGo、超额搜索和正文并发能力。
- 额外提供 Baidu、中文相关性评分、SSRF 防护、结构化错误、缓存、鉴权、双排名和可重复效果基准。
- 对搜索、读取、评分、调度、选择、转换等多实现边界使用 interface 解耦。
- 业务枚举遵循自定义类型与 `const` 组合，禁止裸字符串业务状态传播。

### 1.2 非目标

- 不破解或自动完成验证码、人机验证、登录和付费墙。
- 不使用付费搜索 API。
- 第一阶段不解析 PDF、Office、图片和 OCR。
- 第一阶段不使用 LLM 或 embedding 作为在线相关性依赖。
- 不把不同 Provider 的名次混合成一个伪造的统一搜索排名。
- 不保证对整个互联网的每个查询都优于基线；保证范围限定为版本化、可复现的基准门禁。

## 2. 已验证基线与差距

### 2.1 `web-search-mcp` 已验证行为

- 搜索顺序为 Browser Bing、Browser Brave、Axios DuckDuckGo。
- 每个搜索源返回整组结果后计算平均相关性，记录当前最佳结果集。
- Bing 结果分数达到 `0.8` 时可提前返回；后续搜索源达到默认阈值 `0.3` 时可提前返回；否则返回已观察到的最佳整组结果。
- 需要正文时，搜索候选数量为：

\[
C_{search}=\min(2N+2,10)
\]

- 实际并发读取的非 PDF 候选数量为：

\[
C_{read}=\min(2N,10)
\]

- 所有候选通过 `Promise.all` 等待完成，成功结果保持原相对顺序并被放到失败结果之前。
- 上游结果类型没有排名字段，因此成功结果前移后不能区分原始名次与选择后名次。
- 相关性 tokenizer 使用 ASCII `\w`；中文查询会丢失中文关键词并退化为默认分数 `0.5`。
- HTTP 正文失败后会进入 Playwright；真实 CAPTCHA 页面仍会失败。

### 2.2 当前 Go 服务已验证行为

- `auto` Provider 顺序为 Baidu、DuckDuckGo、Bing，返回第一个无错误的 Provider 结果集，不比较相关性。
- 已支持 Browser Bing、HTTP DuckDuckGo 和 Baidu 多 transport，没有 Brave Provider。
- `/v1/search` 与 `/v1/read` 相互独立，没有超额搜索和“返回 N 条可读正文”的组合编排。
- HTTPReader 对普通静态正文有效；BrowserReader 已能通过腾讯云 JS challenge 获取最终正文。
- 知乎会把浏览器导航到 `/account/unhuman`，最终仍为 CAPTCHA；延长渲染等待不能产生正文。
- BrowserReader 当前单并发槽使多篇浏览器正文排队，5 篇腾讯云正文实测完成时间延伸至约 21 秒。
- 一次 `go语言开发` 实时样本中，10 个 DuckDuckGo 候选首轮正文成功 7 个、两个知乎稳定 CAPTCHA、一个 CSDN 临时 HTTP 521 后重试成功。

## 3. 方案选择

### 3.1 采用方案

采用原生 Parity-Plus：保留现有 Go 服务和 Provider 模式，补齐 Brave、中文质量门控、超额候选、组合正文调度与同条件效果基准。

默认组合搜索源顺序用于展示和诊断：

1. Baidu
2. Browser Bing
3. Browser Brave
4. HTTP DuckDuckGo

组合服务不会简单返回第一个成功结果，而是在搜索阶段预算内收集可用结果集并选择质量最高的一组。原 `/v1/search` 的明确 Provider 语义保持不变，避免破坏现有调用方。

### 3.2 未采用方案

- 不完全复刻 `web-search-mcp`，因为其中文评分、排名语义和错误模型不足。
- 不把 SearXNG 作为核心依赖，因为目标是原生自实现替代；SearXNG 可保留为未来可插拔 Provider。

## 4. API 设计

### 4.1 统一搜索接口

现有轻量查询接口保持不变：

```http
GET /v1/search
```

新增同资源路径的高级请求形式：

```http
POST /v1/search
Content-Type: application/json
```

请求：

```json
{
  "query": "Go 语言并发模型",
  "provider": "auto",
  "limit": 5,
  "content": {
    "enabled": true,
    "candidate_limit": 0,
    "format": "markdown",
    "max_chars": 30000
  },
  "refresh": false,
  "debug": false
}
```

字段约定：

- `query`：必填，沿用 `/v1/search` 的长度与规范化规则。
- `provider`：默认 `auto`；指定具体 Provider 时不执行跨 Provider 质量选择。
- `limit`：期望返回的结果数量，范围 `1..10`，默认 `5`；启用正文时表示期望返回的可读正文数量。
- `content`：可选；缺省或 `enabled=false` 时只执行搜索，不读取正文。
- `content.candidate_limit`：`0` 表示自动计算；显式值范围为 `limit..20`。
- `content.format`：`markdown` 或 `text`。
- `content.max_chars`：每条正文的 Unicode 字符上限，沿用 `/v1/read` 规则。
- `refresh`：同时跳过搜索 fresh cache 和正文 fresh cache。
- `debug`：需要 `X-Debug-Token`，自动隐含 `refresh=true`。

请求模式：

- `content` 缺省或 `enabled=false`：调用轻量搜索服务，响应语义与现有 `GET /v1/search` 一致。
- `content.enabled=true`：调用组合编排服务，执行 Provider 质量选择、超额候选和正文读取。
- 不在 `GET /v1/search` 增加 `include_content` 一类参数。正文读取可能触发浏览器、并发调度和 30 秒预算，不适合被中间缓存、预取或自动重试为普通幂等查询。

成功响应：

```json
{
  "query": "Go 语言并发模型",
  "requested_provider": "auto",
  "selected_provider": "baidu",
  "candidate_count": 12,
  "readable_count": 5,
  "results": [
    {
      "original_rank": 7,
      "selected_rank": 3,
      "provider": "baidu",
      "title": "...",
      "url": "https://example.com/article",
      "snippet": "...",
      "relevance_score": 0.86,
      "read_status": "success",
      "read_transport": "chromedp",
      "content": "...",
      "content_format": "markdown",
      "content_length": 10832,
      "truncated": false
    }
  ],
  "failures": [
    {
      "original_rank": 2,
      "provider": "baidu",
      "url": "https://example.com/blocked",
      "code": "captcha_required",
      "retryable": true
    }
  ],
  "meta": {
    "partial": false,
    "search_took_ms": 3120,
    "read_took_ms": 8410,
    "took_ms": 11530,
    "request_id": "req_..."
  },
  "warnings": []
}
```

语义约定：

- `original_rank` 是所选 Provider 原始结果集中的名次，永不因正文成功与否改写。
- `selected_rank` 是成功正文在本次响应中的顺序，从 1 开始连续编号。
- `results` 只包含达到正文质量门槛的成功项。
- `failures` 保留未入选候选的稳定错误码，但普通请求不返回上游原始错误。
- 成功数少于 `limit` 时返回 HTTP 200、`meta.partial=true` 和 warning；所有候选都失败时返回结构化非 2xx 错误。
- Provider 原始排名不跨 Provider 混排；`selected_provider` 明确搜索结果集来源。

### 4.2 兼容性

- 保留 `GET /v1/search` 的现有参数、响应和轻量语义。
- `POST /v1/search` 作为同一搜索资源的高级调用入口，不新增 `/v1/search/content` 子资源。
- 保留 `POST /v1/read`。
- 现有 UI 可继续分别调用两个接口；后续增加组合搜索开关，不要求重写为浏览器应用。

### 4.3 内部编排边界

路由统一不合并业务职责。HTTP handler 只负责参数绑定、鉴权和响应映射：

```go
type SearchContentOrchestrator interface {
    Search(ctx context.Context, request domain.SearchContentRequest) (domain.SearchContentResponse, error)
}
```

- `content.enabled=false` 时调用现有 `SearchService`。
- `content.enabled=true` 时调用 `SearchContentOrchestrator`。
- `SearchContentOrchestrator` 组合 `ProviderSelector`、`ReadScheduler`、`ContentQualityEvaluator` 和排名选择器，不直接实现 Provider、HTTP、Chromedp、正文提取或格式转换。
- `POST /v1/read` 继续直接暴露单 URL 读取能力，组合搜索不得复制其安全校验、缓存、提取和转换实现。

## 5. 搜索质量选择

### 5.1 接口边界

```go
type SearchQualityEvaluator interface {
    Name() domain.ImplementationName
    Evaluate(query string, results []domain.SearchResult) domain.SearchQualityResult
}

type ProviderSelector interface {
    Select(ctx context.Context, request domain.SearchRequest) (domain.SearchResponse, domain.ProviderSelection, error)
}
```

`SearchQualityEvaluator` 只负责评分；`ProviderSelector` 负责 Provider 调度、预算、错误聚合和选择。二者禁止耦合 HTTP、Chromedp 或具体 Provider parser。

### 5.2 Unicode/CJK tokenizer

- 英文和数字按连续 Unicode letter/digit token 处理，统一小写。
- 中文按连续 CJK 文本保留完整短语，同时生成单字和相邻双字 token。
- 中英文停用词分开维护为 typed configuration，不在评分函数中散布魔法字符串。
- 标题、snippet 和 URL 分开计算覆盖率。

单条结果分数：

\[
R_i=\operatorname{clamp}(0.55C_t+0.30C_s+0.05C_u+0.10B-P,0,1)
\]

参数含义：

- \(C_t\)：标题关键词覆盖率。
- \(C_s\)：snippet 关键词覆盖率。
- \(C_u\)：URL 关键词覆盖率。
- \(B\)：完整查询短语或连续双字短语命中奖励。
- \(P\)：空标题、空 URL、明显广告、重复 URL 和无关模式惩罚。

Provider 结果集分数：

\[
R_p=0.70\overline{R}_{top}+0.20D+0.10K
\]

参数含义：

- \(\overline{R}_{top}\)：原始前 5 条结果的平均相关性。
- \(D\)：前 10 条结果的唯一可注册域名比例。
- \(K\)：title、URL、snippet 字段完整率。

所有权重和阈值集中在不可变配置中，并通过 fixture 测试锁定行为。

### 5.3 Provider 调度

- `provider != auto`：只调用指定 Provider，仍计算结果质量用于响应和基准。
- `provider == auto`：在 10 秒搜索阶段预算内，以最多 2 个 Provider 并发执行 Baidu、Bing、Brave、DuckDuckGo。
- 每个 Provider 使用自身 timeout、rate limiter、breaker 和 debug attempts。
- 分数达到 `0.80` 的结果集可提前成为候选最佳集，但只有在不存在尚未完成且原始优先级更高的健康 Provider 时才提前结束。
- 未达到 `0.80` 时，在预算结束或 Provider 全部完成后选择分数最高的成功结果集。
- 分数相同时依次比较有效结果数、唯一域名数、Provider 配置优先级。
- 失败 Provider 不使成功响应失败；错误进入 selection diagnostics 和 warning。

## 6. 超额候选与正文调度

### 6.1 候选数量

自动候选数量：

\[
C=\min(2N+2,20)
\]

参数含义：

- \(N\)：请求的可读正文数量。
- \(C\)：实际请求和处理的候选数量。
- `20`：当前 Base Backend 的搜索候选硬上限。

该策略在 `N=5` 时读取 12 个候选，比 `web-search-mcp` 的 10 个候选多 2 个；在调用方显式设置 `candidate_limit` 时使用合法显式值。

### 6.2 去重与排名

- 在读取前规范化 URL fragment、默认端口和 host case。
- 完全相同 canonical URL 只读取一次，保留第一次出现的 `original_rank`。
- 不因正文先完成而改变选择顺序。
- 成功正文最终按 `original_rank` 升序选择前 `N` 条，再赋值 `selected_rank`。
- 不同 URL 的近重复正文第一阶段只记录 warning，不自动删除，避免误删转载或版本差异。

### 6.3 两级读取调度

```text
候选结果
  → HTTPReader worker pool
  → 直接接受的正文
  → 需要渲染的候选
  → BrowserReader worker pool
  → ContentQualityEvaluator
  → RankPreservingSelector
```

默认并发与预算：

- 组合请求总预算：30 秒。
- HTTP 读取并发：8。
- Browser 读取并发：3 个 tab slot。
- HTTP 单候选 timeout：6 秒。
- Browser 单候选 timeout：12 秒。
- 每个正文最大解压或 DOM 大小：5 MiB。

`ReadScheduler` 负责 worker pool、context cancellation 和结果收集；`ReadService` 继续负责单 URL 安全读取、缓存、提取、质量和转换。

允许提前停止的唯一条件：已经获得 `N` 个成功正文，并且所有 `original_rank` 小于当前第 `N` 个成功结果的候选都已完成。这样可以取消更低排名的未开始任务，同时不会因完成顺序改变排名。

### 6.4 浏览器边界

- BrowserReader 只处理 HTTP 403/429/503、JS shell、HTTP extractor challenge 和质量过短等明确 eligible 状态。
- CAPTCHA、login-required 和 paywall 在浏览器渲染后仍被拒绝。
- 浏览器池使用独立正文 Profile，不复用搜索 Profile。
- 公网生产部署前，BrowserReader 必须放入无内网路由的隔离 worker 或具备等价 egress policy；当前主 URL 和最终 URL 校验不能替代所有子资源的 DNS pinning。

## 7. Brave Provider

- 新增 `ProviderNameBrave`、`TransportNameBraveChromedp` 和 Brave parser fixture。
- 搜索 URL 为 Brave Web Search 公共结果页，不使用付费 Brave Search API。
- 浏览器 transport 使用独立 Profile、timeout、并发槽和 debug artifact。
- Parser 至少提取 title、direct URL、snippet、原始 rank。
- CAPTCHA、空结果、DOM 变化、timeout 和 network error 使用现有稳定错误模型。
- Brave Provider 不与 Bing Provider 共用 selector 常量或 DOM fixture。

## 8. 缓存、错误与观测

### 8.1 缓存

- 组合服务复用现有 SearchService cache 和 ReadService canonical document cache。
- 第一阶段不额外缓存完整组合响应，避免一条正文失败使整个组合结果长期固化。
- `refresh=true` 跳过搜索和所有候选的 fresh cache；stale fallback 仍按单服务规则执行并标记 degraded。

### 8.2 错误

新增 typed error/warning 仅在确有新语义时创建：

- `insufficient_readable_results`：有搜索结果但没有任何正文达到质量门槛。
- `partial_readable_results`：成功数小于请求数，作为 warning。
- `provider_quality_below_threshold`：Provider 成功但质量不足，作为 selection diagnostics。

所有开发阶段 Debug 响应继续保留原始错误和 attempts；普通响应只返回稳定码和安全描述。

### 8.3 指标

组合响应至少记录：

- Provider attempts、评分、选择原因和搜索耗时。
- 候选数、HTTP 成功数、浏览器 fallback 数、正文成功数和失败分类计数。
- HTTP pool wait、Browser pool wait、每候选耗时和组合总耗时。
- cache hit、stale hit、partial 和 early-stop 状态。

## 9. 效果基准

### 9.1 基准系统

- Baseline：固定 commit 与依赖版本的 `web-search-mcp`，通过 MCP stdio 调用 `full-web-search`，而不是“摘要搜索加单页抓取”。
- Candidate：本项目 `POST /v1/search`，并设置 `content.enabled=true`。
- 已有 `/Users/zoe/Documents/daily/web-search-comparison-demo` MCP client 和 stdout-to-stderr bootstrap 继续复用并扩展。

### 9.2 查询集

首版包含 20 个版本化查询：

- 14 个中文查询。
- 6 个英文查询。
- 覆盖编程技术、官方文档、中文教程、产品比较、时效信息、歧义查询和长尾问题。
- 每个查询定义语言、类别、必需意图词、可接受同义词和明显无关模式。
- 查询定义进入版本控制，基准运行不得临时替换失败查询。

### 9.3 公平执行

- 每个系统每个查询请求 5 条可读正文，每条最多 30000 字符。
- 每次调用总预算 30 秒。
- 每个查询至少运行 3 轮，总计每个系统至少 60 次组合搜索。
- 每轮随机交替系统先后顺序，查询之间至少冷却 2 秒。
- 记录 cache policy、网络环境、Provider/engine、Git commit、依赖版本、开始时间和原始输出。
- Candidate 使用 `refresh=true`；Baseline 不复用跨调用业务 cache。
- CAPTCHA、timeout 和上游空结果计入失败，不人工挑选替代 URL。

### 9.4 指标

搜索成功率：

\[
S=\frac{Q_{search\_ok}}{Q_{total}}
\]

可读正文满足率：

\[
F=\frac{\sum_q\min(R_q,N)}{Q_{total}\cdot N}
\]

参数含义：

- \(Q_{search\_ok}\)：返回至少一条有效搜索结果的查询运行数。
- \(Q_{total}\)：查询运行总数。
- \(R_q\)：查询运行实际返回的合格正文数。
- \(N\)：目标正文数，固定为 5。

还需统计：

- 中文和英文分组的 `S`、`F`。
- 平均相关性、前 5 条 intent coverage、明显无关结果比例。
- 唯一域名数、重复 URL 比例、CAPTCHA 比例和失败分类。
- 总耗时 P50、P95；搜索耗时与正文耗时分别统计。
- Blind pairwise 人工抽检：隐藏系统名后比较同查询结果集。

### 9.5 通过门槛

所有条件必须同时满足：

1. Candidate 总搜索成功率不低于 Baseline。
2. Candidate 总正文满足率不低于 Baseline。
3. Candidate 中文正文满足率高于 Baseline；若 Baseline 已为 100%，则 Candidate 必须同为 100% 且中文相关性不低于 Baseline。
4. Candidate 自动相关性平均分不低于 Baseline。
5. Blind pairwise 中 Candidate 的 win 加 tie 比例至少为 80%，lose 比例不得超过 20%。
6. Candidate P95 总耗时不超过 Baseline 的 1.25 倍；如果超出，只能通过降低并发成本或提高调度效率修复，不能删除困难查询。
7. Candidate 每个结果均有 Provider、`original_rank` 和 `selected_rank`；Baseline 缺失字段不降低 Candidate 要求。
8. Candidate 的稳定错误分类和 Debug 原始信息测试全部通过。

任一门槛失败时，报告必须标记 `FAIL` 并列出逐查询差距；不得以单个成功案例或平均结果掩盖失败类别。

## 10. 测试策略

### 10.1 单元测试

- Unicode/CJK tokenizer、短语匹配、权重、惩罚和 batch quality。
- 候选公式、显式上限、URL 去重和双排名。
- Provider selection 的并发、预算、tie-break、partial success 和错误聚合。
- ReadScheduler 的 HTTP/Browser 并发上限、提前停止条件、取消和稳定顺序。
- Brave parser fixture 的正常、空结果、CAPTCHA 和 DOM 变化。
- API binding、debug token、partial 与 all-failed HTTP 语义。

### 10.2 集成测试

- 使用本地 fixture Provider 和 fixture page 完整验证 `POST /v1/search` 的正文模式。
- 验证 `content` 缺省与 `enabled=false` 时不触发任何正文读取，并保持轻量搜索响应语义。
- 构造前 3 条读取失败、第 4 至第 8 条成功的结果，证明能够补足 5 条并保留双排名。
- 构造完成顺序与原始排名相反的读取结果，证明响应不按完成时间排序。
- 构造 Browser pool 排队和总 deadline，证明没有 goroutine 泄漏。
- 运行 `make check`，覆盖普通测试、Race Detector、`go vet` 和 build。

### 10.3 实时基准

- 基准不替代自动化测试，自动化测试也不替代实时基准。
- 实时基准输出原始 JSON、汇总 JSON 和 Markdown 报告。
- 每轮结果携带系统版本与时间，历史结果不覆盖。

## 11. 交付顺序

1. 定义组合 API domain contract、候选策略和双排名。
2. 实现 CJK quality evaluator 与 QualityProviderSelector。
3. 增加 Brave Provider。
4. 实现两级 ReadScheduler 和 Browser 并发池。
5. 注册 `POST /v1/search`，补齐轻量模式、正文模式、UI 组合开关和文档。
6. 扩展 comparison demo，使其调用 `full-web-search` 与组合 API。
7. 运行首轮 20 查询、3 轮基准并生成 PASS/FAIL 报告。
8. 对失败指标迭代实现，直到全部门槛通过。

## 12. 完成审计

只有以下证据同时存在时才能声明目标完成：

- 生产代码、接口文档、fixture 和自动化测试已提交。
- `make check` 在最终代码状态通过。
- Baseline 与 Candidate 使用同一版本化查询集完成至少 3 轮运行。
- 原始基准输出和汇总报告已保存。
- 报告的全部通过门槛均为 PASS。
- 未将 CAPTCHA、timeout、空结果或缺失正文从分母中删除。
- 没有尚未实现却被标记完成的设计条目。
