# Web Search Backend Demo

一个面向人类客户端和 AI Agent 的 Go + Gin 网页搜索 API。项目通过通用 `Provider` 接口注册 Baidu、DuckDuckGo、Bing、Brave，并通过容量感知的 `auto` Router 统一调度。

当前实现不使用付费 SERP API。搜索链路读取公开搜索结果页，并通过跨 Provider fallback、Provider 内部多 Agent Profile 池化调度、Provider 质量选择、超额候选、Provider 内部 transport fallback、缓存、限流、抖动和熔断提供 best-effort 可用性；独立的 Read API 可按需读取某条结果的 HTML/纯文本正文并转换为 Markdown 或 text。

## 架构

```mermaid
flowchart TD
    Client["人类客户端 / AI Agent"] --> Gin["Gin HTTP API"]
    Gin --> Service["SearchService"]
    Service --> Fresh["Fresh Cache"]
    Service --> Registry["Provider Registry"]
    Registry --> Auto["routing.Router: auto"]
    Registry --> Explicit["PooledProvider: baidu/bing/brave/duckduckgo"]
    Auto --> BaiduPool["profilepool.Pool: baidu"]
    Auto --> BingPool["profilepool.Pool: bing"]
    Auto --> BravePool["profilepool.Pool: brave"]
    Auto --> DuckPool["profilepool.Pool: duckduckgo"]
    Explicit --> BaiduPool
    Explicit --> BingPool
    Explicit --> BravePool
    Explicit --> DuckPool
    BaiduPool --> Lease["Lease: Probation/Trusted/Degraded/Quarantined/Draining/Retired"]
    BingPool --> Lease
    BravePool --> Lease
    DuckPool --> Lease
    Lease --> Baidu["BaiduProvider"]
    Lease --> Bing["BingProvider"]
    Lease --> Brave["BraveProvider"]
    Lease --> Duck["DuckDuckGoProvider"]
    Baidu --> Desktop["Desktop HTTP"]
    Desktop -->|失败| Mobile["Mobile HTTP"]
    Mobile -->|失败| Chrome["Chromedp"]
    Desktop --> Detector["Detector + Parser"]
    Mobile --> Detector
    Chrome --> Detector
    Duck --> DuckHTTP["DuckDuckGo HTML HTTP"]
    Bing --> BingChrome["Bing Chromedp"]
    Brave --> BraveChrome["Brave Chromedp"]
    Auto --> Trace["searchtrace.Manager: spool + SQLite + JSON log"]
    Explicit --> Trace
    BaiduPool --> Manifest["ManifestStore: 每 Profile 磁盘持久化信任状态"]
    BingPool --> Manifest
    BravePool --> Manifest
    DuckPool --> Manifest
    Gin --> Combined["SearchContentService"]
    Combined --> Quality["Unicode/CJK Quality Selector"]
    Quality --> Registry
    Combined --> Scheduler["ReadScheduler: oversample + stable ranks"]
    Scheduler --> ReadService
    Gin --> ReadService["ReadService"]
    ReadService --> SafeURL["SafeURLPolicy"]
    ReadService --> ReadHTTP["Bounded HTTP Reader"]
    ReadService --> Extract["Extractor Registry"]
    ReadService --> Convert["Converter Registry"]
    Service -->|实时失败| Stale["Stale Cache"]
    Baidu --> Artifacts["HTML / Screenshot Artifacts"]
    Bing --> Artifacts
    Brave --> Artifacts
```

职责边界：

- Gin：路由、query binding、request ID、客户端限流和 JSON 编码。
- SearchService：参数归一化、fresh/stale cache、`singleflight`，以及基于 `SEARCH_GLOBAL_INFLIGHT_MAX`/`SEARCH_AUTO_QUEUE_MAX` 的全局在飞请求闸门（Live Guard）。
- Provider Registry：按名称查找 `auto`、`baidu`、`bing`、`brave` 和 `duckduckgo`；显式 Provider 都是包一层 `profilepool.Pool` 的 `PooledProvider`。
- `routing.Router`（`auto`）：容量感知路由，见下方「容量感知路由与 Profile 池」。
- `profilepool.Pool`：每个 Provider 一个池，内部管理若干 Agent Profile 的信任状态机、容量、限速和磁盘 manifest 持久化。
- BaiduProvider：编排 `desktop_http → mobile_http → chromedp`（固定 Session 策略优先，退回 Header Profile Pool 备选策略）。
- DuckDuckGoProvider：读取轻量 HTML 搜索页，不启动浏览器。
- BingProvider：使用独立 Chromedp Profile 读取 Bing 结果页。
- BraveProvider：使用独立 Chromedp Profile 读取 Brave 公开结果页。
- QualityProviderSelector：组合模式下并发观察 `baidu → bing → brave → duckduckgo`，使用 Unicode/CJK 相关性、字段完整率和域名多样性选择整组结果。
- SearchContentService：按 \(C=\min(2N+2,20)\) 超额搜索候选，并发读取后按原始排名保留前 \(N\) 条成功正文。
- Detector：区分正常页、空结果、验证码、429、封禁和 DOM 变化。
- Parser：分别解析桌面页、移动页和浏览器 DOM。
- DebugArtifactStore：保存完整 HTML、截图以及 SHA-256。
- `searchtrace.Manager`：记录路由/池/Provider 每个决策点的 trace，见下方「Search Trace」。
- ReadService：执行 URL 安全策略、正文缓存、HTTP 优先读取、Chromedp 按需渲染、提取、质量判断和格式转换；HTTPReader 与 BrowserReader 通过统一接口解耦。
- Read Pipeline：`URLPolicy`、`ResourceReader`、`SourceTypeDetector`、`ContentExtractor`、`QualityEvaluator`、`ContentConverter`、`ReadCache` 均通过窄接口解耦。

## 容量感知路由与 Profile 池

搜索侧的路由不再是简单的固定优先级 fallback chain，而是 `internal/routing.Router` + `internal/profilepool.Pool` 的两层结构。完整设计背景、已验证事实和验收记录见 `docs/provider-profile-pool-routing-design.md`。

**Provider Profile Pool**（`internal/profilepool`）：每个 Provider（baidu/bing/brave/duckduckgo）持有一个 `Pool`，池内是若干 Agent Profile（不只是 UA，还绑定并发容量、独立 Chrome/Cookie 身份）。每个 Profile 有一个信任状态机：

```
warming → probation ⇄ trusted
              ↓  连续失败超过阈值               ↓ CAPTCHA / 限流
          degraded（冷却后重试，超过重试上限则 draining → retired）
          quarantined（冷却后重试，超过重试上限则 draining → retired）
```

- `TryAcquire`/`Acquire` 从池里租出一个 `Lease`（独占一个容量槽位），请求结束后必须 `Release` 并带上分类结果（`normal`/`empty`/`captcha`/`rate_limited`/`blocked`/`parse_changed`/`timeout`/`network_error`），驱动 EWMA 信任分和状态迁移。
- `probation` 达到最小样本数、成功率和近期 EWMA 阈值后升级为 `trusted`；`TrustedTrafficPercent`（默认 80%）流量按 `request_id` 哈希桶优先分配给 `trusted` Profile，其余分给 `probation`，为新 Profile 持续攒经验值。
- CAPTCHA/限流直接进入 `quarantined`（默认冷却 5 分钟）；连续失败超过 `MaxConsecutiveFailures`（默认 3 次）进入 `degraded`（默认冷却 1 分钟）；冷却后重试次数超过 `MaxRecoveryAttempts`（默认 3 次）则 `draining` 直到在飞请求清零后 `retired`。
- 每个 Profile 的信任状态和统计量落盘到 `SEARCH_PROFILE_MANIFEST_ROOT`（每 Provider 一个子目录，每 Profile 一个 JSON 文件，原子写入），进程重启后按时间衰减恢复，避免频繁重启导致 Profile 反复回到 `probation` 冷启动。

**Router**（`internal/routing`，Provider 名字固定为 `auto`）：

- 严格按 `baidu → bing → brave → duckduckgo` 顺序尝试从各自池 `TryAcquireAuto`；某个池暂时没有可用 Lease 就跳过，不等待。
- 全部池都拿不到时，短暂 `AcquireAuto`（默认 `SEARCH_AUTO_ROUTE_WAIT=2s`）等待任意一个池释放槽位；等待前先对全局排队深度做 `SEARCH_AUTO_QUEUE_MAX`（默认 100）限流，超过直接返回 `search_queue_full`。
- 只对 CAPTCHA、限流、上游结构变化（`upstream_changed`）、Provider 不可用、超时这几类 retryable 错误做跨 Provider 重路由（`MaxProviderAttempts`，默认 3 次）；其它错误直接返回给调用方。
- 显式指定 Provider（非 `auto`）走 `routing.PooledProvider`，只在该 Provider 自己的池里等待（默认 `SEARCH_EXPLICIT_ROUTE_WAIT=5s`），拿不到 Lease 返回 `provider_busy`；不跨源 fallback。为避免显式请求长期抢占 `auto` 的等待名额，池内部对连续 3 次显式抢占之后会短暂让路给已在排队的 `auto` 等待者。
- `SEARCH_CAPACITY_ROUTER_ENABLED=false` 时退回旧的固定优先级 `provider.Chain`（不做容量感知等待，也不区分 Profile 信任状态）；`SEARCH_PROFILE_POOL_ENABLED=false` 时每个 Provider 强制退化为 1 个 Profile、容量 1，相当于关闭多 Profile 并发和信任状态机。

## Search Trace

`internal/searchtrace` 给路由和池的每个决策点（`route_decision`、`lease_acquired`、`provider_attempt`、`lease_released`、`shadow_route_decision`/`shadow_route_compare` 等）写持久化事件，用于事后排障和信任状态审计：

- 写入路径是 NDJSON spool（`SEARCH_TRACE_ROOT/spool/events.ndjson`，每条 `fsync`）→ 异步消费进 SQLite（`SEARCH_TRACE_ROOT/search-trace.db`）→ 同时写一份按天滚动的 JSON 日志（`SEARCH_TRACE_ROOT/logs/`）。进程启动时会先 `Replay` 未消费完的 spool 记录，保证不丢事件。
- `SEARCH_TRACE_FAILURE_MODE=strict`（默认）：trace 写入失败会让当前搜索请求本身失败（`trace_persistence_unavailable`），确保 trace 和实际路由行为一致；`best_effort` 则只记录失败但不影响搜索请求。
- `SEARCH_TRACE_STORE_QUERY=false`（默认）：只落盘查询词的 SHA-256、长度和脱敏 preview（数字被替换为 `*`），不存原始查询词；设为 `true` 才存原文。
- 保留策略按小时任务清理：`SEARCH_TRACE_REQUEST_RETENTION`（默认 7 天）清理整条请求 trace，`SEARCH_TRACE_PROFILE_RETENTION`（默认 30 天）清理 Profile 健康事件，`SEARCH_TRACE_SQLITE_MAX_BYTES` 设置后超出会按最旧 trace 优先删除并 `VACUUM`。
- 同一小时任务里，各 Pool 也会做一次 `Reconcile`（到期的 `quarantined`/`degraded` Profile 恢复到 `probation` 重试）。

## 本机运行

环境要求：

- Go 1.26 或更新版本。
- Google Chrome。当前机器可直接使用 `/Applications/Google Chrome.app`。

```bash
cd /Users/zoe/Documents/daily/web-search-backend
make
# 等价于 make run
```

`make run` 最终执行 `go run ./cmd/server`。如需直接使用底层命令：

运行日志会同时显示在终端并追加写入 `log/server.log`。可通过 `LOG_FILE` 覆盖路径，例如 `make run LOG_FILE=log/debug.log`。

```bash
go mod download
go run ./cmd/server
```

服务默认监听 `:8080`。测试：

```bash
curl --get 'http://127.0.0.1:8080/v1/search' \
  --data-urlencode 'q=golang' \
  --data 'provider=auto' \
  --data 'limit=10' \
  --data 'page=1'
```

或运行：

```bash
make smoke Q='golang'
```

浏览器访问 [http://127.0.0.1:8080/ui/](http://127.0.0.1:8080/ui/) 可使用内嵌的 Searchroom：

- 设置 query、Provider、limit、page、refresh、debug 和 Debug Token。
- 可开启“返回可读正文”，设置候选上限、format 和 max chars，直接调用统一 `POST /v1/search`。
- 查看每条结果的实际 Provider、fallback、缓存、warnings 和 attempts。
- 点击结果调用 `/v1/read`，查看渲染后的 Markdown、原始 Markdown/text、可折叠 JSON Tree 和 Debug。
- Debug Token 只保存在浏览器 `sessionStorage`，不会进入 URL。

## Docker 运行

Docker Desktop 新版本通常使用 `docker compose`。当前这台 Mac 的 Colima 环境安装的是独立命令 `docker-compose`，两者使用同一个 `compose.yaml`；以下命令已在本机实测通过：

```bash
make docker-up
make docker-ps
make smoke Q='golang'
```

Makefile 会自动选择 `docker compose` plugin 或独立的 `docker-compose`。需要直接排查 Compose 时，当前 Mac 可使用：

```bash
docker-compose build
docker-compose up -d
docker-compose ps
```

容器使用非 root 用户运行。Baidu、Bing、Brave 与正文读取使用独立 Chromium Profile；Profile 和调试文件分别持久化到独立 volume。

停止服务：

```bash
make docker-down
```

如需同时清理 Demo 创建的所有 volume：

```bash
docker-compose down --volumes
```

## API

### `GET /v1/search`

| 参数 | 必填 | 默认值 | 范围 |
|---|---:|---:|---|
| `q` | 是 | - | 1–256 个字符 |
| `provider` | 否 | `auto` | 支持 `auto`、`baidu`、`duckduckgo`、`bing`、`brave`；显式 Provider 不跨源 fallback |
| `limit` | 否 | `10` | 1–20 |
| `page` | 否 | `1` | 1–10 |
| `refresh` | 否 | `false` | `true` 跳过 fresh cache，强制实时查询 |
| `debug` | 否 | `false` | `false` 或省略时返回干净响应；`true` 必须通过 Debug Token 鉴权 |

成功响应：

```json
{
  "query": "golang",
  "provider": "baidu",
  "results": [
    {
      "title": "The Go Programming Language",
      "url": "https://go.dev/",
      "snippet": "Go is an open source programming language...",
      "rank": 1,
      "provider": "baidu"
    }
  ],
  "meta": {
    "requested_provider": "auto",
    "transport": "desktop_http",
    "cached": false,
    "degraded": false,
    "fallback_count": 0,
    "provider_fallback_count": 0,
    "selected_provider": "baidu",
    "route_reason": "priority",
    "profile_id": "baidu-0003",
    "provider_queue_ms": 0,
    "took_ms": 382,
    "request_id": "req_01J..."
  },
  "warnings": [],
  "debug": {
    "attempts": [],
    "raw_artifacts": []
  }
}
```

顶层 `provider` 是本次结果集的实际搜索源；每条 `results[].provider` 是该条内容的来源；`requested_provider` 是调用方选择的值。`fallback_count` 表示单个 Provider 内部 transport fallback 次数，`provider_fallback_count` 表示跨 Provider fallback 次数。

`selected_provider` 是路由最终选中的 Provider（`auto` 场景下等价于顶层 `provider`）。`route_reason` 说明选择原因：`explicit`（调用方显式指定）、`priority`（`auto` 按严格优先级首次命中）、`retry_reroute`（`auto` 因前序 Provider 失败重路由，此时 `meta.degraded=true` 并附带 `provider_fallback` warning）。`profile_id` 是本次实际租用的 Agent Profile（如 `baidu-0003`），`provider_queue_ms` 是本次请求等待 Profile Lease 的排队耗时（显式 Provider 才计算，`auto` 场景恒为 0）。授权 Debug 响应里 `debug.attempts[*]` 还会带上 `profile_id`、`lease_id` 和 `route_round`（本次是第几轮尝试的 Provider）。

`transport` 可能是 `desktop_http`、`mobile_http`、`chromedp`、`duckduckgo_http`、`bing_chromedp`、`brave_chromedp`、`fresh_cache` 或 `stale_cache`。

### `POST /v1/search`

同一路径的高级 JSON 调用。`content` 缺省或 `enabled=false` 时只执行轻量搜索；`content.enabled=true` 时执行质量选源、超额候选和正文读取。正文模式的 `limit` 范围为 1–10，默认 5。

```bash
curl 'http://127.0.0.1:8080/v1/search' \
  --header 'Content-Type: application/json' \
  --data '{
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
  }'
```

`candidate_limit=0` 自动使用：

\[
C=\min(2N+2,20)
\]

其中 \(N\) 是请求的可读正文数，\(C\) 是搜索候选数。例如请求 5 篇正文时搜索 12 条候选。读取完成顺序不会改变排名：`original_rank` 保留 Provider 原始名次，`selected_rank` 是本次成功正文的连续序号。

成功数不足 `limit` 但至少有一篇时仍返回 HTTP 200，并设置 `meta.partial=true` 和 `partial_readable_results` warning；全部候选读取失败时返回 HTTP 422 `insufficient_readable_results`。`debug=true` 需要同一 `X-Debug-Token`，失败响应会额外包含 `search_attempts` 与 `read_attempts` 的原始信息。

### 强制刷新

`refresh=true` 跳过 fresh cache，直接查询 Provider；实时查询失败时仍允许返回 stale cache：

```bash
curl --get 'http://127.0.0.1:8080/v1/search' \
  --data-urlencode 'q=golang' \
  --data 'refresh=true'
```

### 请求级 Debug

通过授权 token 临时采集当前请求的诊断信息：

```bash
export SEARCH_DEBUG_TOKEN='replace-with-a-random-secret'

curl --get 'http://127.0.0.1:8080/v1/search' \
  --data-urlencode 'q=golang' \
  --data 'debug=true' \
  --header "X-Debug-Token: ${SEARCH_DEBUG_TOKEN}"
```

`debug=true` 自动隐含 `refresh=true`，确保 attempts、headers、HTML preview 和 artifacts 来自本次实时请求。Token 只能放在 `X-Debug-Token` Header，不要放入 URL。Token 缺失、错误或服务端未配置时返回 HTTP 403 `debug_unauthorized`。

`debug=false` 或省略 `debug` 始终只返回标准字段，即使服务运行在 Gin Debug 模式也不会绕过 Token。授权 Debug 数据不会写入共享搜索缓存，后续普通请求不会读到上一次的诊断信息。

### `POST /v1/read`

请求一个搜索结果 URL，返回经过清洗和长度限制的正文。第一阶段支持 `text/html`、`application/xhtml+xml` 和 `text/plain`；PDF 明确返回 `unsupported_content_type`，留到第二阶段。

```bash
curl 'http://127.0.0.1:8080/v1/read' \
  --header 'Content-Type: application/json' \
  --data '{
    "url": "https://go.dev/doc/",
    "format": "markdown",
    "max_chars": 30000,
    "refresh": false,
    "debug": false
  }'
```

`format` 支持 `markdown`、`text`；`max_chars` 范围为 1000–100000，按 Unicode 字符计数并优先在段落边界截断。`debug=true` 与搜索接口相同，需要 `X-Debug-Token` 并自动强制 refresh。

```json
{
  "url": "https://go.dev/doc/",
  "final_url": "https://go.dev/doc/",
  "title": "Documentation",
  "source_type": "html",
  "content": "# Documentation\n\n...",
  "content_format": "markdown",
  "content_length": 12580,
  "truncated": false,
  "meta": {
    "transport": "http",
    "extractor": "readability",
    "cached": false,
    "degraded": false,
    "fallback_count": 0,
    "took_ms": 312,
    "request_id": "req_01J..."
  },
  "warnings": []
}
```

读取链路为 `SafeURLPolicy → HTTPReader → MIME Detector → Extractor → Quality → Converter`。当 HTTP 返回 JS challenge、无法提取正文或质量判断为 JS shell 时，服务会按需执行一次 `BrowserReader → Detector → Extractor → Quality → Converter`。浏览器执行页面脚本并读取最终 DOM，不会自动填写验证码、登录账号或绕过付费墙；浏览器渲染仍没有正文时会返回明确错误。

BrowserReader 使用独立 Profile、单请求 deadline、默认 3 个并发 tab slot 和 DOM 大小上限。当前实现面向本机 Demo：主 URL 和最终 URL 都经过 SafeURLPolicy，但浏览器加载的页面子资源尚未通过 HTTPReader 的 DNS pinning transport。部署到不可信公网前，应将浏览器 reader 放入无内网路由的隔离 worker/容器并配置 egress policy；也可以设置 `SEARCH_READ_BROWSER_ENABLED=false` 完全关闭它。

SSRF 防护会拒绝私网、回环、link-local、CGNAT、Metadata、组播、非法协议和危险重定向。Demo 如需读取本机 fixture，只能通过 `SEARCH_READ_HOST_ALLOWLIST` 精确允许 Host；allowlist 仍不能开放 Metadata 或 link-local 地址。

如果本机 Clash/Mihomo 使用 fake-IP，DNS 可能返回 `198.18.x.x`；该保留网段会被 Read API 拒绝。开发时应在当前实际生效的 Clash 配置中，将测试域名加入 `dns.fake-ip-filter` 并重载内核，使应用拿到真实 IP；不要在后端放行整个 `198.18.0.0/15`。搜索结果域名是动态的，若要测试任意站点，应使用独立的 real-IP DNS 开发配置，而不是逐条维护业务代码白名单。

### 授权原始错误

只有显式 `debug=true` 且 `X-Debug-Token` 正确时，失败响应才同时提供稳定错误码和原始上游信息：

```json
{
  "error": {
    "code": "captcha_required",
    "message": "百度返回安全验证页面",
    "retryable": true,
    "original_error": "baidu response classified as captcha: status=200 final_url=https://wappass.baidu.com/..."
  },
  "meta": {
    "provider": "baidu",
    "request_id": "req_01J..."
  },
  "debug": {
    "attempts": [
      {
        "strategy": "fixed_session",
        "transport": "baidu_session_http",
        "header_profile": "baidu_fixed_session",
        "http_status": 200,
        "classification": "captcha",
        "session_state": "cooling",
        "session_generation": 1,
        "blocked_until": "2026-07-14T16:30:00+08:00",
        "original_error": "baidu response classified as captcha: status=200 final_url=...",
        "body_preview": "<!doctype html>...",
        "body_sha256": "..."
      }
    ],
    "raw_artifacts": [
      "var/debug/req_01J/desktop_http.html",
      "var/debug/req_01J/chromedp.png"
    ]
  }
}
```

每个 fallback attempt 都保留：

- Strategy、Transport、Header Profile、请求 URL、最终 URL、页面标题和耗时。
- 固定 Session 的 state、generation、等待时间和 `blocked_until`。
- HTTP status、分类结果和 Parser error。
- 原始 Go error。
- 已脱敏的响应头。
- 最多 32 KiB 正文 preview 和完整正文 SHA-256。
- 完整 HTML；chromedp 还会保存截图。

以下信息不会进入 API 响应：`Cookie`、`Set-Cookie`、`Authorization`、`Proxy-Authorization` 和 Chrome Profile 身份数据。

普通请求永远不会返回 `original_error`、attempt 正文和本地 artifact 路径；`SEARCH_DEBUG` 只控制 Gin 运行模式，不构成 API Debug 授权。

## 错误码

| HTTP | code | 含义 |
|---:|---|---|
| 400 | `invalid_request` | 参数错误 |
| 403 | `debug_unauthorized` | 请求级 Debug token 缺失、错误或服务端未配置 |
| 404 | `provider_not_found` | Provider 未注册 |
| 429 | `rate_limited` | 本服务或百度限流 |
| 502 | `upstream_changed` | 百度 DOM 变化导致解析失效 |
| 503 | `captcha_required` | 百度要求安全验证且没有 stale cache |
| 503 | `provider_unavailable` | 所有 Transport 不可用或处于熔断状态 |
| 503 | `provider_busy` | 显式指定的 Provider 在等待窗口内没有可用 Profile Lease |
| 503 | `provider_capacity_exhausted` | `auto` 路由尝试次数用尽仍没有 Provider 拿到 Lease 或全部重路由失败 |
| 503 | `search_queue_full` | `auto` 路由的等待队列已达 `SEARCH_AUTO_QUEUE_MAX` 上限 |
| 503 | `trace_persistence_unavailable` | `SEARCH_TRACE_FAILURE_MODE=strict` 时 trace 落盘失败 |
| 504 | `upstream_timeout` | 整体搜索链路超时 |
| 403 | `unsafe_url` | Read URL、DNS 结果或重定向目标不安全 |
| 415 | `unsupported_content_type` | Read 第一阶段不支持 PDF、图片或 Office 文件 |
| 413 | `content_too_large` | 解压后的资源超过读取硬上限 |
| 422 | `extraction_failed` | HTTP 与浏览器路径都未提取到有效正文，或浏览器 fallback 被关闭 |
| 422 | `insufficient_readable_results` | 组合搜索的全部候选都未产生有效正文 |
| 502 | `fetch_failed` | Read DNS、TLS、连接或上游响应失败 |
| 504 | `fetch_timeout` | Read 获取或渲染超时 |

正常零结果返回 HTTP 200 和 `results: []`。只有检测到正常搜索页根节点后，空数组才被视为真正的空结果。

## 稳定性策略

- Fresh cache：15 分钟。
- Stale cache：24 小时。
- 相同查询使用 `singleflight` 合并并发请求。
- BaiduProvider 每个 Agent Profile 首先使用固定 Session 策略：专用 `baidu_fixed_session` Header Profile、独立 CookieJar、首页 bootstrap、串行请求，响应结束后等待 3–5 秒；第一页不发送 `rn/pn` 参数。
- 固定 Session 的 network、timeout、parse changed 会进入该 Profile 自己的 Header Profile Pool 备选策略（`desktop_http → mobile_http → chromedp`）；CAPTCHA、429、403、503 直接交给 Profile Pool 的信任状态机（进入 `quarantined`）和跨 Provider fallback，不继续请求百度。
- Header Profile Pool 备选策略保留每秒 1 次、burst 3 和 200–800 ms jitter；同一 `request_id` sticky。
- DuckDuckGo、Bing、Brave、百度 Header Pool 和正文浏览器继续使用 Header Profile Pool；百度固定 Session 在创建时绑定该 Profile 自己的 Primary Header Profile，生命周期内不轮换。
- 每个 profile 同时约束 `User-Agent`、`Accept-Language`、UA Client Hints、`navigator.platform` 和 viewport，避免只改 UA 造成字段冲突。
- Profile Pool 不是验证码绕过器；上游已返回 CAPTCHA 时仍返回 `captcha_required`，触发对应 Agent Profile 进入 `quarantined`（默认冷却 5 分钟），并由 `auto` 执行跨 Provider fallback。
- Baidu 固定 Session 内部另有一层 Transport 级熔断（`baidu.Breaker`）：CAPTCHA 熔断 30 分钟（`SEARCH_BAIDU_CAPTCHA_COOLDOWN`），限流/403/503 熔断 5 分钟（`SEARCH_BAIDU_RATE_LIMIT_COOLDOWN`），与 Profile Pool 的 `quarantined`/`degraded` 状态机是两层独立机制。
- 每个 Provider 默认 4–6 个 Agent Profile 并发工作（各自独立容量），而非单一 UA 轮换；搜索 Chromedp client 默认最多 2 个并发 tab，正文 Chromedp client 默认最多 3 个并发 tab。
- Baidu、Bing、Brave 和正文读取使用不同的 Chrome Profile，Cookie 与 Session 不共享；同一 Provider 内部不同 Agent Profile 之间也各自独立 Chrome/Cookie 身份。
- `auto` 只对验证码、限流、超时、上游结构变化和 Provider 不可用执行跨源 fallback。
- 实时失败且存在 stale cache 时返回 HTTP 200、`degraded=true`，并附带本次实时失败 attempts。

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `SEARCH_ADDR` | `:8080` | 监听地址 |
| `SEARCH_DEBUG` | `true` | Gin 开发/发布运行模式；不绕过 Debug Token |
| `SEARCH_DEBUG_TOKEN` | 空 | 请求级 `debug=true` 使用的 `X-Debug-Token`；空值拒绝请求级 Debug |
| `SEARCH_DEBUG_DIR` | `./var/debug` | HTML/截图目录 |
| `SEARCH_DEBUG_PREVIEW_BYTES` | `32768` | API 正文 preview 上限 |
| `SEARCH_CHROME_PROFILE_DIR` | `./var/chrome-profile` | 持久化 Chrome Profile |
| `SEARCH_BING_PROFILE_DIR` | `./var/chrome-profile-bing` | Bing 独立 Chrome Profile |
| `SEARCH_BRAVE_PROFILE_DIR` | `./var/chrome-profile-brave` | Brave 独立 Chrome Profile |
| `SEARCH_CHROME_PATH` | 空 | Chrome/Chromium 可执行文件 |
| `SEARCH_CHROME_HEADLESS` | `true` | 是否无头运行 |
| `SEARCH_CHROME_NO_SANDBOX` | `false` | Docker 中可设为 `true` |
| `SEARCH_USER_AGENT` | 本机 Chrome 150 desktop UA | Header Profile Pool 的 Chromium UA 基线；部署时应与实际 Chrome 主版本保持一致 |
| `SEARCH_TOTAL_TIMEOUT` | `20s` | 整体请求超时 |
| `SEARCH_CONTENT_TIMEOUT` | `30s` | 组合搜索与正文读取的整体请求超时 |
| `SEARCH_DESKTOP_TIMEOUT` | `4s` | Desktop HTTP 超时 |
| `SEARCH_MOBILE_TIMEOUT` | `4s` | Mobile HTTP 超时 |
| `SEARCH_CHROME_TIMEOUT` | `10s` | Chromedp 超时 |
| `SEARCH_DUCKDUCKGO_URL` | `https://html.duckduckgo.com/html/` | DuckDuckGo HTML 搜索地址 |
| `SEARCH_DUCKDUCKGO_TIMEOUT` | `5s` | DuckDuckGo HTTP 超时 |
| `SEARCH_BING_URL` | `https://www.bing.com/search` | Bing 搜索地址 |
| `SEARCH_BING_TIMEOUT` | `10s` | Bing Chromedp 超时 |
| `SEARCH_BRAVE_URL` | `https://search.brave.com/search` | Brave 搜索地址 |
| `SEARCH_BRAVE_TIMEOUT` | `10s` | Brave Chromedp 超时 |
| `SEARCH_PROVIDER_BROWSER_SLOTS` | `2` | 每个搜索浏览器 client 的最大并发 tab 数 |
| `SEARCH_CAPACITY_ROUTER_ENABLED` | `true` | `false` 退回旧的固定优先级 `provider.Chain`，不做容量感知等待 |
| `SEARCH_PROFILE_POOL_ENABLED` | `true` | `false` 时每个 Provider 强制退化为 1 个 Profile、容量 1 |
| `SEARCH_BAIDU_PROFILE_COUNT` | `6` | Baidu Agent Profile 数量 |
| `SEARCH_BAIDU_PROFILE_CAPACITY` | `1` | 每个 Baidu Profile 的并发容量 |
| `SEARCH_BING_PROFILE_COUNT` | `5` | Bing Agent Profile 数量 |
| `SEARCH_BING_PROFILE_CAPACITY` | `2` | 每个 Bing Profile 的并发容量 |
| `SEARCH_BRAVE_PROFILE_COUNT` | `3` | Brave Agent Profile 数量 |
| `SEARCH_BRAVE_PROFILE_CAPACITY` | `1` | 每个 Brave Profile 的并发容量 |
| `SEARCH_DUCKDUCKGO_PROFILE_COUNT` | `4` | DuckDuckGo Agent Profile 数量 |
| `SEARCH_DUCKDUCKGO_PROFILE_CAPACITY` | `1` | 每个 DuckDuckGo Profile 的并发容量 |
| `SEARCH_PROFILE_MANIFEST_ROOT` | `./var/provider-manifests` | 每个 Agent Profile 信任状态持久化根目录 |
| `SEARCH_AUTO_ROUTE_WAIT` | `2s` | `auto` 在全部池暂时无可用 Lease 时的等待窗口 |
| `SEARCH_EXPLICIT_ROUTE_WAIT` | `5s` | 显式指定 Provider 等待 Lease 的窗口，超时返回 `provider_busy` |
| `SEARCH_MAX_PROVIDER_ATTEMPTS` | `3` | `auto` 跨 Provider 重路由的最大尝试次数 |
| `SEARCH_MINIMUM_ATTEMPT_BUDGET` | `3s` | `auto` 判断是否还有预算发起下一次尝试的最小剩余时间 |
| `SEARCH_AUTO_QUEUE_MAX` | `100` | `auto` 路由等待队列深度上限，超过返回 `search_queue_full` |
| `SEARCH_GLOBAL_INFLIGHT_MAX` | `100` | 全局在飞搜索请求数上限（Live Guard） |
| `SEARCH_TRACE_ENABLED` | `true` | 是否启用 Search Trace |
| `SEARCH_TRACE_ROOT` | `./var/trace` | Trace spool/SQLite/日志根目录 |
| `SEARCH_TRACE_FAILURE_MODE` | `strict` | `strict` 或 `best_effort`；`strict` 时 trace 写入失败会让搜索请求失败 |
| `SEARCH_TRACE_STORE_QUERY` | `false` | `true` 时在 trace 里存原始查询词，默认只存哈希/长度/脱敏 preview |
| `SEARCH_TRACE_REQUEST_RETENTION` | `168h` | 整条请求 trace 的保留时长 |
| `SEARCH_TRACE_PROFILE_RETENTION` | `720h` | Profile 健康事件的保留时长 |
| `SEARCH_TRACE_LOG_RETENTION` | `168h` | 按天滚动 JSON trace 日志的保留时长 |
| `SEARCH_TRACE_SQLITE_MAX_BYTES` | `1073741824` | Trace SQLite 文件大小上限，超出按最旧 trace 优先清理 |
| `SEARCH_FRESH_TTL` | `15m` | fresh cache TTL |
| `SEARCH_STALE_TTL` | `24h` | stale 最大年龄 |
| `SEARCH_PROVIDER_RATE` | `1` | Provider token/s |
| `SEARCH_PROVIDER_BURST` | `3` | Provider burst |
| `SEARCH_JITTER_MIN` | `200ms` | 上游请求最小 jitter |
| `SEARCH_JITTER_MAX` | `800ms` | 上游请求最大 jitter |
| `SEARCH_BAIDU_SESSION_MIN_INTERVAL` | `3s` | 固定百度 Session 在上次响应结束后的最小空闲时间 |
| `SEARCH_BAIDU_SESSION_JITTER_MAX` | `2s` | 固定百度 Session 追加的最大随机等待 |
| `SEARCH_BAIDU_CAPTCHA_COOLDOWN` | `30m` | 百度 CAPTCHA 后固定 Session 冷却时间 |
| `SEARCH_BAIDU_RATE_LIMIT_COOLDOWN` | `5m` | 百度 429、403、503 后固定 Session 冷却时间 |
| `SEARCH_BAIDU_FALLBACK_RESERVE` | `5s` | 固定 Session 等待时为当前请求和后续 fallback 保留的预算 |
| `SEARCH_CLIENT_RATE` | `5` | 每客户端 IP token/s |
| `SEARCH_CLIENT_BURST` | `10` | 每客户端 IP burst |
| `SEARCH_TRUSTED_PROXIES` | 空 | 逗号分隔的可信内网 CIDR |
| `SEARCH_READ_ENABLED` | `true` | 是否注册 `POST /v1/read` |
| `SEARCH_READ_HTTP_TIMEOUT` | `6s` | HTTP 正文读取超时 |
| `SEARCH_READ_BROWSER_ENABLED` | `true` | 是否在符合条件时启用 Chromedp 正文 fallback |
| `SEARCH_READ_BROWSER_TIMEOUT` | `12s` | 单次浏览器正文读取超时 |
| `SEARCH_READ_BROWSER_WAIT` | `2s` | DOM ready 后等待页面脚本渲染的时间 |
| `SEARCH_READ_BROWSER_SLOTS` | `3` | 正文浏览器最大并发 tab 数 |
| `SEARCH_READ_CHROME_PROFILE_DIR` | `./var/chrome-profile-read` | 正文浏览器独立 Profile 目录 |
| `SEARCH_READ_FRESH_TTL` | `30m` | 正文 fresh cache TTL |
| `SEARCH_READ_STALE_TTL` | `24h` | 正文 stale cache 最大年龄 |
| `SEARCH_READ_CACHE_MAX_ITEMS` | `500` | 正文内存缓存条目上限 |
| `SEARCH_READ_MAX_BODY_BYTES` | `5242880` | 解压后正文资源硬上限 |
| `SEARCH_READ_MAX_REDIRECTS` | `5` | 正文 HTTP 重定向上限 |
| `SEARCH_READ_HOST_ALLOWLIST` | 空 | 允许读取私网/回环的精确 Host；只用于受控本机 Demo |

所有非空配置都严格解析。值非法时服务拒绝启动，不会静默回退到默认值。

## 测试

默认测试完全离线：

```bash
make check
```

`make check` 顺序执行普通测试、Race Detector、`go vet` 和二进制构建。也可以单独执行 `make test`、`make test-race`、`make vet` 或 `make build`。

离线端到端测试会启动本地假百度、DuckDuckGo 和正文服务器，并通过静态 Bing/Brave fixture 完整验证 Gin、SearchService、质量选源、超额候选、ReadScheduler、Provider、Transport、Detector、Extractor 和 Converter 链路。

## 边界

- 本项目无法保证百度、DuckDuckGo、Bing 或 Brave 永不返回验证码或限流。
- 不自动解题、破解或绕过验证码。
- 持久化 Profile、缓存、低频率、jitter 和熔断用于减少触发概率及避免反复冲击上游。
- 搜索引擎调整 DOM 后需要更新对应 Parser fixture 和 selector。
- `auto` fallback 返回的是实际成功搜索源的排名；它不能冒充百度排名。
- 免费、固定搜索源精确排名、无人值守高可用无法同时得到保证；本 Demo 对不可用情况返回明确、可诊断的结构化结果。
- Read API 不破解验证码、登录、付费墙或访问控制，不携带调用方 Cookie/Authorization，不递归抓取链接。
- 第一阶段不解析 PDF、Office、图片或 OCR，也不保证 Readability 能从任意网页提取出有效正文。

## Header Profile Pool 设计与 Bing 诊断

注意区分两个不同层次的 "Profile"：本节的 Header Profile Pool（`internal/headerprofile`）管理 UA/Accept-Language/Client Hints/viewport 的一致性组合，供单次 HTTP/Chromedp 请求选用；前文「容量感知路由与 Profile 池」里的 Agent Profile（`internal/profilepool`）是更高层的调度单位，绑定并发容量和跨请求的信任状态机，一个 Agent Profile 内部的多次请求会各自选用 Header Profile。

Header Profile 的选择键优先使用 `request_id`，缺失时使用 query 的稳定 hash；因此同一逻辑请求会复用同一 header profile。Pool 已接入 Baidu HTTP、DuckDuckGo HTTP、Baidu/Bing/Brave Chromedp 和正文 browser reader。授权 Debug 响应的 `attempts[*].header_profile` 可用于按 header profile 聚合成功率，`attempts[*].profile_id` 则对应 Agent Profile。

Chromedp 在导航前通过 CDP 同时设置 User-Agent、语言、平台、UA Client Hints、额外请求头和 viewport。不要从互联网上收集大量陈旧 UA 随机轮换；少量、可验证且和实际 Chromium 版本一致的 profile 更容易诊断，也不会制造互相矛盾的浏览器信号。

Bing HTTP 200 不代表一定是结果页。Bing 可能在正常 `/search` URL 和正常页面标题下嵌入 Cloudflare Turnstile。检测器会识别 `turnstile-widget`、Turnstile script 和 `/challenge/verify` 特征，并返回：

```json
{
  "error": {
    "code": "captcha_required",
    "message": "Bing 返回安全验证页面",
    "retryable": true
  },
  "debug": {
    "attempts": [{
      "transport": "bing_chromedp",
      "header_profile": "chrome_desktop_secondary",
      "http_status": 200,
      "classification": "captcha"
    }]
  }
}
```

这类结果不是 Parser DOM 漂移。只有既不是 CAPTCHA/限流/网络错误、又找不到有效结果结构时，才返回 `upstream_changed`。Header Profile Pool 可降低不一致指纹和便于分桶观测，但无法解决 IP reputation、请求突发、Cookie 状态或搜索引擎策略导致的 challenge。
