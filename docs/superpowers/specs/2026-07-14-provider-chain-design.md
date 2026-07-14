# Provider Chain 设计规范

## 目标

在现有通用 `Provider` 接口上增加跨搜索引擎 fallback，使默认搜索不再因为百度验证码、限流或页面变化而整体不可用，同时保留显式 `provider=baidu` 的严格百度语义。

第一版注册四个 Provider：

- `baidu`：现有 `BaiduProvider`，内部保持 `desktop_http -> mobile_http -> chromedp`。
- `duckduckgo`：新增免费 HTTP Provider，读取 DuckDuckGo HTML 搜索结果页。
- `bing`：新增浏览器 Provider，使用现有 Go `chromedp` 运行时读取 Bing 搜索结果页。
- `auto`：新增 `ProviderChain`，按 `baidu -> duckduckgo -> bing` 顺序执行。

## 非目标

- 第一版不实现 Brave 或 Google Provider。
- 不并发请求多个搜索引擎，不融合或重排不同 Provider 的结果。
- 不保证任何公开搜索结果页面长期稳定，也不承诺规避搜索引擎安全验证。
- `provider=baidu` 失败时不静默返回其他搜索引擎结果。
- 搜索接口不自动下载每条结果的正文；正文读取由独立的 `POST /v1/read` 提供，详细设计见 [正文读取 API 设计规范](./2026-07-14-read-api-design.md)。

## 与正文读取的边界

`GET /v1/search` 负责发现、排序并返回候选 URL；AI 或人类客户端根据标题和摘要选择相关结果，再调用 `POST /v1/read` 获取标准化正文。搜索结果不支持 `include_content=true`，避免一次搜索隐式扇出访问多个第三方站点，导致延迟、资源消耗和失败率失控。

```mermaid
flowchart LR
  Client["人类客户端或 AI"] --> Search["GET /v1/search"]
  Search --> Results["候选 URL 列表"]
  Results --> Select["客户端选择相关 URL"]
  Select --> Read["POST /v1/read"]
  Read --> Document["标准 Markdown 或纯文本"]
```

## API 语义

### 请求

`GET /v1/search` 的 `provider` 支持：

- 不传或传 `auto`：使用 Provider Chain。
- `baidu`：只调用百度。
- `duckduckgo`：只调用 DuckDuckGo。
- `bing`：只调用 Bing Browser Provider。
- 其他值：返回 `provider_not_found`。

默认 Provider 从 `baidu` 改为 `auto`。

### 成功响应

顶层 `provider` 表示实际产生结果的 Provider，而不是请求值。`meta.requested_provider` 表示调用方选择的 Provider；`meta.provider_fallback_count` 表示在成功前跳过的 Provider 数量。现有 `meta.fallback_count` 继续表示单个 Provider 内部的 transport fallback 数量。

示例：百度失败、DuckDuckGo 成功时：

```json
{
  "query": "golang",
  "provider": "duckduckgo",
  "results": [],
  "meta": {
    "requested_provider": "auto",
    "transport": "duckduckgo_http",
    "fallback_count": 0,
    "provider_fallback_count": 1,
    "cached": false,
    "degraded": true,
    "request_id": "req_example"
  },
  "warnings": [
    {
      "code": "provider_fallback",
      "message": "provider baidu failed; using duckduckgo"
    }
  ]
}
```

只要发生跨 Provider fallback，`meta.degraded` 为 `true`，并加入不泄露敏感响应内容的 `provider_fallback` warning。

### Debug 响应

`domain.Attempt` 增加可选的 `provider` 字段。Chain 会给下游 Provider 返回的每个 Attempt 补充 Provider 名称，使 `debug=true` 能区分：

- Provider fallback：例如 `baidu -> duckduckgo`。
- Provider 内部 transport fallback：例如 `desktop_http -> mobile_http -> chromedp`。

非 Debug 响应继续隐藏原始错误、响应头、HTML 预览和调试产物路径。

### 错误响应

错误响应 `meta.provider` 返回请求的 Provider，不再固定为 `baidu`。

当 Chain 中所有 Provider 都失败时：

- 对外错误码为 `provider_unavailable`，HTTP 状态为 `503`。
- `Retryable` 为 `true`。
- `Original` 聚合每个 Provider 的原始错误，并带 Provider 名称。
- Debug 模式返回按执行顺序合并的 Attempt 和 artifact 路径。

## ProviderChain

`ProviderChain` 实现现有接口：

```go
type Provider interface {
	Name() domain.ProviderName
	Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error)
}
```

构造函数接收 Chain 名称及有序 Provider 列表。第一版 Chain 名为 `auto`，成员顺序为 `baidu -> duckduckgo -> bing`。Bing 放在最后，避免正常请求优先承担浏览器启动与渲染成本。

执行规则：

1. 依次调用成员 Provider，并把传给成员的 `SearchRequest.Provider` 改成成员名称。
2. 第一个成功响应立即返回，不再调用后续 Provider。
3. 成功响应的 `Provider` 强制设置为实际成员名称。
4. 为响应写入 `RequestedProvider=auto` 和 Provider fallback 次数。
5. 前序失败信息仅以结构化 warning 和 Debug Attempt 呈现。

下列上游错误允许继续 fallback：

- `captcha_required`
- `rate_limited`
- `upstream_changed`
- `provider_unavailable`
- `upstream_timeout`

下列情况立即停止：

- Context 已取消或超时。
- `invalid_request`。
- `debug_unauthorized`。
- `provider_not_found`。
- 未识别的非 `SearchError` 编程错误。

### 接口和类型化常量约束

Provider Chain 只依赖 `Provider` 接口，不在 Chain 中判断 Baidu、DuckDuckGo 或 Bing 的具体实现类型。`SearchRequest.Provider`、`SearchResponse.Provider`、`Meta.RequestedProvider` 和 `Attempt.Provider` 均使用 `ProviderName`；transport 名称、错误码和分类值也分别使用自定义业务类型与类型化 `const`。禁止在 Chain、Registry、Handler 和测试中传播魔法字符串。

```go
// ProviderName 表示一个可注册的搜索来源或搜索策略名称。
type ProviderName string

const (
	// ProviderNameAuto 表示自动执行 Provider Chain。
	ProviderNameAuto ProviderName = "auto"

	// ProviderNameBaidu 表示百度搜索来源。
	ProviderNameBaidu ProviderName = "baidu"

	// ProviderNameDuckDuckGo 表示 DuckDuckGo 搜索来源。
	ProviderNameDuckDuckGo ProviderName = "duckduckgo"

	// ProviderNameBing 表示 Bing 搜索来源。
	ProviderNameBing ProviderName = "bing"
)
```

该写法遵循 `reviewcode` G-01：`type ProviderName string` 是自定义业务类型，枚举值集中声明为类型化 `const`；不使用 `type ProviderName = string`，也不使用可变 `var` 模拟常量。导出类型、常量、接口和方法必须提供以自身名称开头的完整 Go Doc 注释。

## DuckDuckGoProvider

### 请求

使用共享 `http.Client` 请求：

```text
GET https://html.duckduckgo.com/html/?q=<query>&s=<offset>
```

其中 `offset` 为：

$$
s = (page - 1) \times limit
$$

- $page$：从 1 开始的页码。
- $limit$：期望返回的条目数。
- $s$：DuckDuckGo 查询偏移量。

请求继承服务总超时，并配置独立的 Provider HTTP 超时、浏览器形态 User-Agent、HTML Accept Header、响应体大小上限和共享连接池。

### 解析

使用项目已有的 `goquery`，从 `.result` 中提取：

- 标题：`.result__a`。
- URL：`.result__a[href]`，解析 DuckDuckGo redirect 参数 `uddg` 后返回目标 URL。
- 摘要：`.result__snippet`。
- 排名：当前响应中的顺序，从 1 开始。

页面不包含预期结果根节点时返回 `upstream_changed`；正常页面没有结果时返回空数组，不视为解析错误。

### 限制与保护

- 不执行 JavaScript。
- 不自动处理验证码。
- HTTP 429 分类为 `rate_limited`。
- HTTP 5xx 分类为 `provider_unavailable`。
- 请求超时分类为 `upstream_timeout`。
- HTML 结构不匹配分类为 `upstream_changed`。
- 不把 Cookie、响应正文或敏感 Header 放入普通响应。

## BingProvider

### 请求链路

第一版复用项目现有 `chromedp` 浏览器运行时，导航到：

```text
GET https://www.bing.com/search?q=<query>&count=<limit>&first=<offset>
```

Bing 的起始位置为：

$$
first = (page - 1) \times limit + 1
$$

- $page$：从 1 开始的页码。
- $limit$：期望返回的条目数。
- $first$：Bing 从 1 开始的结果偏移量。

浏览器等待 `body` 和结果选择器就绪，随后抓取最终 URL、完整 DOM 和 Debug 模式需要的截图。Bing 使用独立 Profile 目录和单并发信号量，不与百度共享 Session，但复用同一个 Chrome 可执行文件配置。

第一版采用直接结果页导航。访问 Bing 首页、填写搜索框并提交表单属于后续可靠性增强，不作为本次验收条件。

### 浏览器 Transport 泛化

现有 `chromebrowser.Client` 的 URL 构造写死了百度参数。它将改为接收 `SearchURLBuilder`：

```go
type SearchURLBuilder func(domain.SearchRequest) (string, error)
```

百度和 Bing 分别提供自己的 URL Builder；浏览器生命周期、并发控制、DOM 抓取、截图和响应体上限继续由通用 Client 负责。这样不在通用 Transport 中增加搜索引擎条件分支。

### 解析与错误

使用 `goquery` 从 `.b_algo` 中提取：

- 标题与 URL：`h2 a`。
- 摘要：优先 `.b_caption p`，其次 `p`。
- 排名：当前响应中的顺序，从 1 开始。

最终 URL 或 DOM 出现安全验证特征时分类为 `captcha_required`；结果根节点消失时分类为 `upstream_changed`；浏览器超时分类为 `upstream_timeout`；其他启动、导航和抓取错误分类为 `provider_unavailable`。

## 缓存和 singleflight

缓存键继续包含请求 Provider：

```text
provider|query|page|limit
```

因此 `auto`、`baidu`、`duckduckgo`、`bing` 缓存相互隔离。`auto` 缓存值保留实际成功 Provider；命中缓存后仍将 `meta.requested_provider` 设置为 `auto`。

`refresh=true` 和 `debug=true` 的现有规则不变。Debug 数据不得写入缓存。

stale-cache warning 改为通用文案，不再声称一定是百度实时查询失败。

## 配置与装配

新增 DuckDuckGo 配置：

- `SEARCH_DUCKDUCKGO_URL`，默认 `https://html.duckduckgo.com/html/`。
- `SEARCH_DUCKDUCKGO_TIMEOUT`，默认 `5s`。
- `SEARCH_BING_URL`，默认 `https://www.bing.com/search`。
- `SEARCH_BING_TIMEOUT`，默认 `10s`。
- `SEARCH_BING_PROFILE_DIR`，默认 `./var/chrome-profile-bing`。

Bootstrap 执行顺序：

1. 创建共享 HTTP Client。
2. 创建并注册 `BaiduProvider`。
3. 创建并注册 `DuckDuckGoProvider`。
4. 创建并注册 `BingProvider`。
5. 使用三个搜索 Provider 构造并注册 `ProviderChain("auto", ...)`。

Chain 的成员直接持有 Provider 引用，不在运行期间再次查询 Registry，避免递归 Chain 和运行时配置歧义。

## 测试策略

所有生产代码遵循测试先行：先写会因功能缺失而失败的测试，确认失败原因正确，再写最小实现。

测试范围：

1. `ProviderChain` 首个 Provider 成功时不调用后续 Provider。
2. ProviderName、transport、错误码和分类不使用裸字符串或裸整数。
3. 可重试上游错误触发下一 Provider。
4. 非可重试错误和 Context 取消立即停止。
5. 全部失败时保留按顺序聚合的原始错误和 Debug Attempt。
6. 成功响应准确设置实际 Provider、请求 Provider、两类 fallback 计数和 degraded 状态。
7. DuckDuckGo fixture 覆盖正常结果、redirect URL、空结果、页面结构变化、429、5xx 和超时。
8. Bing fixture 覆盖正常结果、空结果、页面结构变化、安全验证和浏览器错误分类。
9. 通用 Chromedp Client 分别使用百度和 Bing URL Builder 生成正确分页地址。
10. HTTP Handler 默认选择 `auto`，显式 `baidu`、`duckduckgo` 或 `bing` 均不跨 Provider fallback。
11. `auto`、`baidu`、`duckduckgo` 和 `bing` 使用独立缓存键。
12. stale-cache 文案和错误响应 Provider 不再写死百度。
13. 离线端到端测试通过本地假 DuckDuckGo Server 和静态 Bing fixture 验证 Gin、Service、Chain、Provider、Parser 和响应 JSON；真实浏览器访问作为手动 smoke test，不进入离线测试。

## 验收标准

- 不传 `provider` 时请求进入 `auto`。
- 百度返回验证码时，`auto` 能继续尝试 DuckDuckGo。
- 百度和 DuckDuckGo 均失败时，`auto` 能继续尝试 Bing。
- 显式指定 `baidu`、`duckduckgo` 或 `bing` 时绝不跨 Provider fallback。
- 普通响应能识别实际 Provider，但不包含原始调试信息。
- 授权的 Debug 响应能看到每个 Provider 及其 transport 的原始失败。
- 所有既有测试和新增测试通过。
- README 包含 Provider 列表、fallback 语义、配置项和 curl 示例。
