# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

Go + Gin 网页搜索 API demo（`web-search-backend`），不依赖付费 SERP API，通过读取 Baidu、DuckDuckGo、Bing、Brave 的公开搜索结果页提供搜索，并提供独立 Read API 抓取正文并转换为 Markdown/text。详细的 API 契约、错误码、环境变量和稳定性策略见 `README.md`（这是本仓库的权威文档，改动路由行为前应先读它）。

## 常用命令

```bash
make run              # go run ./cmd/server，日志写终端和 log/server.log（可用 LOG_FILE= 覆盖）
make build             # 构建 bin/search-api
make test              # go test ./...
make test-race         # go test ./... -race
make vet               # go vet ./...
make check             # test + test-race + vet + build，提交前应跑这个
make smoke Q='golang'  # 起一个真实请求打本机运行中的服务

go test ./internal/profilepool/...            # 单个包
go test ./internal/routing/... -run TestName  # 单个测试
```

Docker（用于验证容器化行为，Compose 文件适配 Colima 环境的 `docker-compose` 独立命令）:

```bash
make docker-up
make docker-ps
make docker-down
```

测试全部离线：单测和集成测试用本地假 Baidu/DuckDuckGo/正文 server 及静态 Bing/Brave fixture（`testdata/`），不会打真实上游。

## 架构

请求路径：`Gin (internal/api/httpapi) → SearchService (internal/app) → Router (internal/routing) → Provider Profile Pool (internal/profilepool) → Provider (internal/provider/*) → Transport (internal/transport/*)`。

- `internal/bootstrap`：`app.go` 组装整个进程（header profile pool、debug artifact store、cache、各 provider 池、read pipeline、router），`search_runtime.go` 专门构建搜索侧运行时（4 个 provider 的 profile pool + `routing.Router`）。改配置项、加新 Provider、调整依赖注入顺序都从这里入手。
- `internal/routing`：`Router`（`domain.Provider` 的一个实现，名字固定是 `auto`）按严格优先级 `baidu → bing → brave → duckduckgo` 从各自的 `profilepool.Pool` 拿 lease；拿不到就短暂等待任意池释放槽位，仍拿不到就报队列满。只对 CAPTCHA、限流、上游结构变化、Provider 不可用、超时这几类 retryable 错误做跨 Provider 重路由，其它错误直接返回。
- `internal/profilepool`：每个 Provider 一个 `Pool`，池内是若干"Agent Profile"（不仅是 UA 字符串，还绑定并发容量、信任状态机 `Probation/Trusted/Degraded/Quarantined/Draining/Retired`、以及可选的 manifest 持久化）。`Lease` 是从池里租出的一次使用权，请求结束后必须 `Release` 并带上 `Result.Classification` 驱动状态机迁移。改并发限流、Profile 生命周期、健康度评估都在这里。
- `internal/searchtrace`：给路由/池/Provider 的每个决策点（`route_decision`、`lease_acquired`、`provider_attempt`、`lease_released` 等）写持久化 trace（spool 落盘 + SQLite/JSON sink），用于 AI/人工排障，`FailureMode` 可配 `strict`（trace 写失败即请求失败）或 `best_effort`。
- `internal/provider/{baidu,bing,brave,duckduckgo}`：各 Provider 的具体实现；Baidu 内部还有自己的 transport fallback 链 `desktop_http → mobile_http → chromedp`（固定 session 优先，失败才退到 Header Profile Pool 备选策略），Bing/Brave 用独立 Chromedp Profile，DuckDuckGo 是纯 HTTP 不起浏览器。四者共享 `internal/detector`（区分正常页/空结果/验证码/429/DOM 漂移）和各自 parser。
- `internal/app`：业务编排层——`search_service.go`（fresh/stale 缓存 + singleflight）、`provider_selector.go` / `single_provider_selector.go`（含正文时先选源）、`search_content_service.go` + `read_scheduler.go`（按 \(C=\min(2N+2,20)\) 超额取候选、并发读正文、保持原始排名）、`read_service.go`（Read API 主流程）。
- `internal/read/*`：Read API 的窄接口链路：`safeurl`（SSRF 防护）→ `reader`（HTTP 优先、按需 `chromebrowser` 渲染）→ `detector`（MIME/JS shell 判断）→ `extractor`（Readability/纯文本）→ `quality` → `converter`（Markdown/text）→ `cache`。每一段都是接口，方便单独换实现或测试。
- `internal/headerprofile`：管理 Chromium 桌面 UA/Accept-Language/Client Hints/viewport 的一致性组合（"profile"），供 HTTP transport 和 Chromedp 统一使用，避免 UA 与其它头字段互相矛盾。
- `internal/debugartifact`：仅在授权 `debug=true` 时落盘/返回的原始 HTML、截图、body SHA-256，用于诊断验证码/DOM 漂移；普通响应绝不包含这些。
- `internal/domain`：跨包共享的请求/响应/错误类型（`SearchRequest`、`SearchResponse`、`SearchError`、`ProviderName` 等），是各层之间的契约,改字段要检查 `internal/api/httpapi` 的序列化和各 Provider 的赋值点。
- `webui/`：内嵌的调试用 Searchroom 前端（`embed.go` 用 `go:embed`），挂在 `/ui/`,用于人工验证 search/read 链路和 debug attempts。

## 开发要点

- 全部环境变量以 `SEARCH_` 前缀，非空值严格解析，非法值直接拒绝启动（不会静默 fallback 到默认值）——加新配置项时保持这个约定，改动在 `internal/config/config.go`。
- Router 到 Pool 到 Lease 的调用约定是"租了就必须释放"：任何新增的 Provider 调用路径如果拿了 `profilepool.Lease`，必须保证 `Release` 一定执行（包括 panic/超时路径),否则会永久占用槽位。
- 加新 Provider 时需要：实现 `provider.Provider` 接口 → 在 `search_runtime.go` 里建对应的 `profilepool.Pool` → 加进 `routing.Router` 的优先级列表 → 补 `testdata/<provider>/` fixture 和离线集成测试。
- 涉及 SSRF/Read 安全策略（`internal/read/safeurl`）的改动要格外小心：私网、回环、link-local、CGNAT、Metadata 地址必须拒绝，`SEARCH_READ_HOST_ALLOWLIST` 只用于受控本机 Demo,不能放开 Metadata 或 link-local。
- 改路由/池相关逻辑前建议先读 `docs/provider-profile-pool-routing-design.md`,里面有完整的需求背景、已验证事实和非目标,避免重新论证已经讨论过的取舍。
