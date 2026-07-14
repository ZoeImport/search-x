# Baidu 固定 Cookie Session 设计

## 1. 背景

当前 BaiduProvider 已具备 CookieJar、结果解析、错误留痕、缓存和跨 Provider fallback，但百度 HTTP 请求仍存在以下问题：

- 没有先访问百度首页建立 Cookie session。
- desktop 和 mobile 共用 CookieJar，却会按 `request_id` 或 query 选择不同 Header Profile。
- 通用 token bucket 默认允许 `burst=3`，不能保证同一 session 的请求间隔。
- desktop 失败后可能继续请求 mobile 和 chromedp，在 CAPTCHA、403 或 429 场景下增加无效上游请求。
- CAPTCHA 判断包含宽泛的 `captcha` DOM 字符串，正常页面存在误判风险。
- CAPTCHA 后 transport breaker 会冷却，但 CookieJar 本身不会销毁和重建。

2026-07-14 已在当前本机网络出口完成固定 session 基线验证：固定 Chrome 150 User-Agent、固定中文请求头、单 CookieJar、首页 bootstrap、响应结束后等待 3 秒、连续 12 个不同查询。结果为 12 个正常百度搜索页、0 个 CAPTCHA，平均请求耗时 1.221 秒；每个页面都存在 `#content_left` 和约 20 个结果容器。

## 2. 目标

实现一个独立的 `BaiduSessionTransport`，让 BaiduProvider 通过单个、固定身份、低频且可冷却的 Cookie session 请求百度桌面搜索页。

必须满足：

- 首次搜索前访问 `https://www.baidu.com/`，使用同一个 CookieJar 保存 bootstrap 和搜索响应 Cookie。
- 一个进程只维护一个百度 session；同一 session 的 bootstrap、等待和搜索操作完全串行。
- session 创建时绑定一个固定 Header Profile，session 生命周期内不轮换。
- 每次上游请求完成后至少空闲 3 秒，并追加 0–2 秒随机 jitter。
- 保留现有 Header Profile Pool 链路作为百度内部备选策略；固定 session 的 network、timeout 和 parse changed 可以进入备选策略。
- CAPTCHA、429、403 和 503 不切换另一套百度策略，立即返回给 BaiduProvider；`provider=auto` 由现有 ProviderChain fallback。
- CAPTCHA 后销毁当前 CookieJar，进入 30 分钟冷却；429、403 和 503 后进入 5 分钟冷却。
- 开发 Debug 保留状态码、最终 URL、页面标题、原始错误、session 状态、等待时长和 artifact；永不记录 Cookie 值。
- 搜索结果继续携带 `provider: "baidu"`。
- API 请求结构不新增 `agentpool` 参数。

## 3. 非目标

本阶段不实现：

- CAPTCHA 自动处理或绕过。
- 多百度 Agent、多 Cookie session 或代理 IP 池。
- 百度登录态和用户 Cookie 导入。
- Header Profile Pool、百度 mobile HTTP 和百度 chromedp 的行为重写；它们按现状保留在备选策略中。
- 分布式 session、跨进程限速或多副本协调。
- Bing、Brave、DuckDuckGo 的 session 策略重构。
- 对非官方百度 HTML 接口提供可用性 SLA。

## 4. 方案选择

采用独立的百度专用 transport，并与现有 Header Profile Pool transport 共同组成百度内部 StrategyChain，而不是互相替代。

理由：

- CookieJar 重建、bootstrap、固定身份和冷却属于百度 session 生命周期，不应污染通用 HTTP transport。
- `BaiduSessionTransport` 继续实现现有 `transport.SearchTransport`，不会改变 ProviderRegistry、SearchService、HTTP API 或缓存边界。
- session 的 clock、等待器、HTTP client factory 和响应分类器均可通过接口替换，测试不需要真实等待或访问百度。
- StrategyChain 首先执行固定 session；只对 network、timeout 和 parse changed 执行百度内部 fallback，避免风控响应在不同 transport 间放大。

两套策略为：

| 策略 | transport | 作用 |
|---|---|---|
| `fixed_session` | `baidu_session_http` | 专用固定 Header、独立 CookieJar、bootstrap、串行 pacing 和 session 冷却 |
| `header_pool` | `desktop_http`、`mobile_http`、`chromedp` | 保留当前 sticky Header Profile Pool，作为非风控错误的兼容备选 |

## 5. 组件设计

### 5.1 BaiduSessionTransport

新增 `internal/provider/baidu/session_transport.go`，实现：

```go
type BaiduSessionTransport struct {
    config        SessionConfig
    profile       headerprofile.Profile
    clientFactory SessionClientFactory
    pacer         SessionPacer
    classifier    ResponseClassifier
    now           func() time.Time
    gate          chan struct{}
    session       BaiduSession
}
```

职责：

- 持有进程级百度 session。
- 串行执行 bootstrap、pacing 和 search。
- 构造百度搜索 URL 和固定请求头。
- 读取有限大小的响应体，提取最终 URL 和页面标题。
- 调用响应分类器。
- 根据分类使 session 失效并进入冷却。
- 返回 `transport.Response` 和可分类的 session error。

它不负责：

- 解析搜索结果卡片。
- 选择其他 Provider。
- 写 HTTP API 响应。
- 读取正文。

### 5.2 BaiduSession

状态类型放在 `internal/domain`，使通用 `transport.Response` 可以引用它而不反向依赖 `provider/baidu`。状态使用项目约定的类型和 `var` 组合：

```go
type BaiduSessionState string

var (
    BaiduSessionStateCold    BaiduSessionState = "cold"
    BaiduSessionStateWarm    BaiduSessionState = "warm"
    BaiduSessionStateCooling BaiduSessionState = "cooling"
)
```

内部状态：

```go
type BaiduSession struct {
    State                 BaiduSessionState
    Client                *http.Client
    LastFinished          time.Time
    BlockedUntil          time.Time
    BlockedClassification domain.Classification
    Generation            uint64
}
```

状态语义：

| 状态 | 语义 | 允许的动作 |
|---|---|---|
| `cold` | 没有可用 Cookie session | 创建 CookieJar 和 client，执行首页 bootstrap |
| `warm` | bootstrap 成功且可搜索 | 等待 session pacing 后执行搜索 |
| `cooling` | 最近一次响应触发风控 | 冷却期内不访问百度；到期后转为 `cold` |

`Generation` 每次创建新 CookieJar 时递增，仅用于 Debug 和指标，不作为安全凭证。

### 5.3 可替换接口

```go
type SessionClientFactory interface {
    New() (*http.Client, error)
}

type SessionPacer interface {
    Wait(ctx context.Context, lastFinished time.Time, reserve time.Duration) (time.Duration, error)
}

type ResponseClassifier interface {
    Classify(statusCode int, finalURL, pageTitle string, body []byte) domain.Classification
}
```

生产 factory 每次创建新的 CookieJar 和 `http.Client`，同时复用应用级基础 `http.Transport`。生产 pacer 使用 context-aware timer 和加密随机 jitter；测试实现使用 fake client、fake clock 和确定性等待器。

session transport 对非正常页面和冷却状态返回携带分类的 typed error：

```go
type SessionError struct {
    Classification domain.Classification
    RetryAfter      time.Duration
    Original        error
}
```

`BaiduProvider` 使用 `errors.As` 读取 `SessionError`，避免 CAPTCHA 或 cooling 被误映射成普通 `network_error`。`SessionError` 的 `Error` 和 `Unwrap` 方法必须保留最底层原始错误。

`SessionConfig` 明确包含 `BootstrapURL`、`SearchURL`、`RequestTimeout`、`MinInterval`、`MaxJitter`、`CaptchaCooldown`、`RateLimitCooldown`、`FallbackReserve` 和 `MaxBodyBytes`，构造时统一验证 URL、正数时长和 body 上限。

不创建通用的 `AgentPool` 接口。本阶段只有一个固定 Baidu session，避免为未验证的多 Agent 行为提前抽象。

### 5.4 Baidu StrategyChain

`StrategyStep` 包含策略名称、transport、该策略使用的 waiter 和是否使用旧 transport breaker。固定 session step 不使用旧 token bucket 和 breaker；Header Pool steps 继续使用当前 limiter、jitter 和 breaker。

`StrategyFallbackPolicy` 根据稳定分类决定是否继续下一 step。默认策略只允许 `network_error`、`timeout` 和 `parse_changed`；`captcha`、`rate_limited`、`blocked` 和正常空结果停止百度内部 chain。

## 6. 请求数据流

`BaiduSessionTransport` 使用容量为 1 的 channel 作为 session gate。`Fetch` 通过 `select` 获取 gate，因此排队阶段可以被调用 context 取消。获取 gate 后，直到本次搜索完成或失败才释放：

1. 检查调用 context。
2. 如果状态为 `cooling` 且未到期，返回保存的风控分类和 `blocked_until`，不访问上游。
3. 如果状态为 `cooling` 且已到期，清空 client 和时间字段，转为 `cold`。
4. 如果状态为 `cold`，创建包含新 CookieJar 的 HTTP client，`Generation` 加一。
5. 如果 `LastFinished` 非零，先执行 session pacing，再使用固定请求头访问 `https://www.baidu.com/`；首次进程启动时不额外等待。
6. bootstrap 最终 URL、标题或状态码显示 CAPTCHA、429、403、503 时，直接进入对应冷却；其他非成功响应保持 `cold` 并返回原始错误。
7. bootstrap 成功后记录 `LastFinished`，状态转为 `warm`。
8. `SessionPacer` 根据 `LastFinished` 等待 3 秒加 0–2 秒 jitter；等待必须响应 context 取消，并为本次百度请求和后续 Provider fallback 保留预算。
9. 首页构造 `/s?wd=<query>&ie=utf-8`；非首页不访问固定 Session，由 Strategy Chain 进入 Header Pool 备选。
10. 使用同一个 client 和固定请求头发送搜索请求。
11. 无论成功、网络错误还是 context timeout，响应结束后更新 `LastFinished`。
12. 提取状态码、最终 URL、页面标题和有限响应体，完成页面分类。
13. CAPTCHA、429、403 或 503 会销毁 client，将 session 转为 `cooling` 并保存分类和到期时间。
14. 正常页面返回 Provider 解析；其他分类返回可分类错误。

gate 覆盖整个流程是有意设计：P0 必须保证同一个 Cookie session 不会并发访问百度。后续如需要提高吞吐，只能在独立验证后设计完整的 session pool，不能放宽单 session 串行约束。

## 7. 请求头策略

固定 session 使用独立的 `baidu_fixed_session` profile，不加入通用 Header Pool，也不根据 `request_id` 或 query 轮换。实测要求 `Accept-Language` 使用 `en;q=0.8`，且不能发送 `Upgrade-Insecure-Requests`；通用 Header Pool 的 Primary/Secondary profile 保持不变。

bootstrap 和搜索共同发送：

```http
User-Agent: <SEARCH_USER_AGENT>
Accept-Language: zh-CN,zh;q=0.9,en;q=0.8
Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8
```

搜索额外发送：

```http
Referer: https://www.baidu.com/
```

不手动设置 `Accept-Encoding`。Go Transport 自动协商并解压 gzip；在没有 Brotli reader 时禁止声明 `br`。

第一页搜索 URL 不发送 `rn` 和 `pn`。真实 Go 请求验证表明这两个参数会显著触发百度安全验证；百度默认第一页返回足够候选，由 parser 按 API `limit` 截断。第二页及以后不走固定 Session，直接进入 Header Pool 备选策略，避免错误地把第一页结果当成分页结果。

HTTP client 使用默认重定向行为，并通过最终 `http.Response.Request.URL` 记录最终 URL。Debug Header 必须经过现有脱敏器，Cookie 和 `Set-Cookie` 不返回客户端。

## 8. 限速与容量

配置默认值：

| 环境变量 | 默认值 | 含义 |
|---|---:|---|
| `SEARCH_BAIDU_SESSION_MIN_INTERVAL` | `3s` | 上一次上游响应完成后的最小空闲时间 |
| `SEARCH_BAIDU_SESSION_JITTER_MAX` | `2s` | 在最小间隔上追加的最大随机等待 |
| `SEARCH_BAIDU_CAPTCHA_COOLDOWN` | `30m` | CAPTCHA 后的 session 冷却时间 |
| `SEARCH_BAIDU_RATE_LIMIT_COOLDOWN` | `5m` | 429、403、503 后的 session 冷却时间 |
| `SEARCH_BAIDU_FALLBACK_RESERVE` | `5s` | 百度等待前为当前请求和后续 Provider 保留的最小预算 |

旧的通用 `SEARCH_PROVIDER_RATE`、`SEARCH_PROVIDER_BURST`、`SEARCH_JITTER_MIN` 和 `SEARCH_JITTER_MAX` 只作用于 `header_pool` 备选策略，不作用于 `fixed_session`，避免两套 limiter 叠加。

按照本机平均百度响应耗时 1.221 秒估算，单 session 平均请求周期为：

\[
E[T] = T_{\min} + E[J] + E[L]
= 3 + 1 + 1.221
= 5.221\text{ 秒}
\]

其中：

- \(T_{\min}\) 是响应结束后的最小空闲时间。
- \(J\) 是 0–2 秒均匀 jitter。
- \(L\) 是一次百度搜索网络耗时。

理论平均吞吐为：

\[
QPS \approx \frac{1}{E[T]} \approx 0.19
\]

即每分钟约 11–12 次实时百度请求。缓存和 singleflight 命中不进入 session transport。

transport 向 pacer 传入 `RequestTimeout + FallbackReserve`。如果 context 剩余时间不足以覆盖计划等待和保留预算，pacer 立即返回 deadline error，而不是等待到整个请求 context 耗尽；BaiduProvider 映射为 `upstream_timeout`，使 `provider=auto` 仍有时间 fallback。session gate 等待同样必须受调用 context 控制，不能形成不可取消的队列。

## 9. 页面分类

更新百度分类器签名，使标题成为显式输入。按以下优先级判断：

1. HTTP 429 为 `rate_limited`。
2. HTTP 403、503 和其他 5xx 为 `blocked`。
3. 最终 host 精确为 `wappass.baidu.com`，或已知百度验证路径，为 `captcha`。
4. 页面标题包含“百度安全验证”为 `captcha`。
5. 页面存在验证表单的强结构特征且包含验证文本，为 `captcha`。
6. 页面存在正常搜索根节点且存在结果卡，为 `normal`。
7. 页面存在正常搜索根节点和明确空结果标识为 `empty`。
8. 其他情况为 `parse_changed`。

禁止以裸 `strings.Contains(body, "captcha")` 或单个静态资源名判定 CAPTCHA。

正常结果卡与 parser 使用同一组选择器来源。目前 desktop parser 使用 `#content_left` 根节点及 `.result, .result-op, .c-container` 直接子节点；分类器不能声明 parser 不识别的选择器作为唯一正常依据。

## 10. BaiduProvider 行为

P0 的 BaiduProvider 注册一个显式 StrategyChain：`fixed_session` 在前，`header_pool` 的 desktop、mobile 和 chromedp steps 在后。

Provider 识别 session transport 返回的可分类错误，并继续复用现有稳定错误码：

| 分类 | API 错误码 | Retryable | `auto` 是否 fallback |
|---|---|---:|---:|
| CAPTCHA | `captcha_required` | true | 是 |
| 429 | `rate_limited` | true | 是 |
| 403/503 | `provider_unavailable` | true | 是 |
| timeout | `upstream_timeout` | true | 是 |
| parse changed | `upstream_changed` | true | 是 |
| normal empty | 成功空结果 | 不适用 | 否 |

百度内部策略切换规则：

| 固定 session 结果 | 是否进入 Header Pool 备选 |
|---|---:|
| network error | 是 |
| timeout | 是 |
| parse changed | 是 |
| CAPTCHA | 否 |
| 429、403、503 | 否 |
| normal 或 empty | 否 |

`provider=baidu` 不跨 Provider fallback，直接返回 BaiduProvider 错误。`provider=auto` 保持当前 ProviderChain 顺序，本设计不调整搜索质量策略。

## 11. Debug 与安全

扩展 `transport.Response` 和 `domain.Attempt`：

```go
type Response struct {
    // existing fields
    Classification    domain.Classification
    PageTitle         string
    SessionState      domain.BaiduSessionState
    SessionGeneration uint64
    SessionWait       time.Duration
    BlockedUntil      time.Time
}
```

API Debug attempt 增加：

```go
PageTitle         string             `json:"page_title,omitempty"`
SessionState      BaiduSessionState  `json:"session_state,omitempty"`
SessionGeneration uint64             `json:"session_generation,omitempty"`
SessionWaitMS     int64              `json:"session_wait_ms,omitempty"`
BlockedUntil      *time.Time         `json:"blocked_until,omitempty"`
```

沿用现有授权规则：只有 `debug=true` 且 `X-Debug-Token` 正确时返回原始错误、attempts、headers、body preview 和 artifact 路径；Debug 自动强制 refresh。普通响应不返回 session 内部信息。

不得记录或返回：

- Cookie 和 `Set-Cookie` 值。
- Debug Token。
- 完整敏感请求头。
- CookieJar 序列化内容。

## 12. 配置与装配

`bootstrap.New` 的百度装配调整为：

1. 创建共享的基础 `http.Transport`。
2. 根据 `SEARCH_USER_AGENT` 创建独立的 `baidu_fixed_session` profile；不复用通用 Pool 的语言和额外 Header。
3. 创建百度 session client factory；每个 generation 复用基础 transport，但创建新的 CookieJar 和 `http.Client`。
4. 创建 session pacer 和百度 classifier。
5. 创建单个 `BaiduSessionTransport` 作为 `fixed_session` step。
6. 保留现有 desktop、mobile 和 chromedp clients，作为 `header_pool` steps，并使用独立于固定 session 的 CookieJar。
7. 创建 Baidu StrategyChain，再创建 BaiduProvider。

Bing、Brave 和正文 reader 的浏览器生命周期保持不变；DuckDuckGo HTTP 保持不变。

## 13. 测试设计

### 13.1 Session transport 单元测试

- 首次搜索严格先 bootstrap，再搜索。
- bootstrap 响应 Cookie 自动出现在搜索请求中。
- 连续查询的 User-Agent、Accept-Language 和其他固定请求头完全相同。
- 固定 session 的 `Accept-Language` 为 `zh-CN,zh;q=0.9,en;q=0.8`，且不包含 `Upgrade-Insecure-Requests`。
- 第一页 URL 不包含 `rn` 和 `pn`；第二页及以后不访问 fixed session upstream。
- 多个并发 `Fetch` 在同一 session 上不会重叠发送请求。
- 第二次查询必须经过配置的最小间隔和 jitter。
- bootstrap、session gate 等待和 pacing 均响应 context 取消。
- 网络错误也更新 `LastFinished`，防止失败后高频重试。
- CAPTCHA 使 client 被销毁、状态转为 `cooling`、generation 不立即增长。
- 冷却期间请求不访问上游，并保留 CAPTCHA 分类。
- 冷却到期后创建新 CookieJar，generation 加一并重新 bootstrap。
- 429、403、503 使用 5 分钟冷却。
- 响应体超过限制时返回稳定错误，不泄漏部分 Cookie 或 headers。

### 13.2 分类器测试

- `wappass.baidu.com` 最终 URL。
- “百度安全验证”标题。
- 强验证表单。
- 正常页面包含 CAPTCHA 静态资源但仍分类为 normal。
- 正常结果页。
- 明确空结果页。
- 根节点存在但结果结构改变。
- 429、403、503 和其他 5xx。

### 13.3 Provider 与 chain 测试

- session CAPTCHA 映射为 `captcha_required` 并保留原始 final URL 和 title。
- session 冷却错误仍保持原始分类，而不是退化为 `network_error`。
- `provider=baidu` 不跨 Provider fallback。
- `provider=auto` 在百度失败后调用下一个 Provider。
- fixed session 的 network、timeout、parse changed 会调用 Header Pool 备选策略。
- fixed session 或 Header Pool 的 CAPTCHA、429、403 不会继续请求另一个百度策略。
- 正常结果和每条结果的 Provider 均为 `baidu`。
- 普通响应不泄漏 session Debug 字段。

### 13.4 真实环境 smoke test

真实百度测试不进入默认 CI，也不把“永不出现 CAPTCHA”作为确定性验收条件。人工验收应记录当前出口网络、查询数量、正常页数、CAPTCHA 数和首次风控所在序号，并验证：

- 正常响应标题符合 `<query>_百度搜索`，且存在 `#content_left` 和结果卡。
- Debug 中同一 generation 的 Header Profile 相同、Cookie 连续且没有并发请求。
- CAPTCHA 能以最终 URL 或页面标题正确识别，后续请求命中本地冷却而不再访问百度。
- CAPTCHA、429、403 不进入 Header Pool；网络失败、超时和解析变化可进入 Header Pool。

2026-07-14 本机实现后的一轮连续请求中，固定 Session 先返回 9 次正常结果，第 10 次进入 CAPTCHA，后续请求正确命中 `cooling`。这说明策略可以显著改善低频请求的可用性，但不能保证 12/12 或永不触发上游风控。真实 smoke 失败时必须保留失败样本和分类，不能把 CAPTCHA 页面从统计分母中删除。

## 14. 验收标准

实现完成必须同时满足：

- 所有 Go 单元测试和集成测试通过。
- `go test -race ./...` 通过，session 不存在数据竞争。
- `go vet ./...` 通过。
- API schema 没有新增 `agentpool` 参数。
- 百度请求使用固定 Header Profile 和单 CookieJar。
- 百度 upstream 同一时刻最多一个请求。
- 固定 session 和 Header Pool 的策略名称、实际 transport 与 fallback attempts 可在 Debug 中区分。
- CAPTCHA、429、403、503 不触发百度内部策略切换；network、timeout 和 parse changed 可以切换。
- `auto` fallback、缓存、refresh、Debug Token 和原始错误语义保持兼容。
- README 记录新配置、容量边界、Debug 字段和 CAPTCHA 行为。
- 完成一轮真实 smoke test，保存正常页、风控页和冷却行为的客观结果。

## 15. 后续阶段

只有固定 session 运行数据证明单 session 吞吐不足，且明确区分 IP 风控与 session 风控后，才评估多 Agent Session Pool。届时每个 Agent 必须拥有独立固定 Header Profile、CookieJar、pacer、状态和冷却；禁止多个 Header Profile 共享一个 CookieJar 轮换使用。
