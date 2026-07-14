# 本地搜索 Web UI 设计

## 目标

提供一个无构建工具、无外部依赖的本地页面，面向开发阶段的人类调用者验证 `GET /v1/search` 和 `POST /v1/read`。页面必须清楚展示搜索结果、Provider fallback、缓存、耗时、warnings、debug attempts，以及选中结果的正文和元数据。

## 方案

采用 `webui/index.html`、`webui/app.css`、`webui/app.js` 三个静态文件。桌面端使用搜索结果与正文阅读器双栏布局，移动端改为单栏。页面默认使用当前 Origin，也允许输入 API Base URL，以便静态文件被独立打开或由其他本地静态服务器提供。

搜索表单包含 `q`、`provider`、`limit`、`page`、`refresh`、`debug` 和 Debug Token。Token 只写入 `sessionStorage`，只通过 `X-Debug-Token` Header 发送，不进入 URL。点击搜索结果后向 `/v1/read` 发送 `url`、`format`、`max_chars`、`refresh`、`debug`。

所有来自 API 的标题、摘要、正文、warnings 和错误通过 `textContent` 或显式 DOM Node 写入页面。结果卡显示 `results[].provider`，旧响应缺失时回退到顶层 `provider`。

正文区域提供 Preview、Markdown、JSON、Debug 四个 Tab。Preview 使用本地实现的安全 Markdown 子集解析器，只创建 heading、paragraph、list、inline code、pre/code、blockquote 和经过协议校验的 link 节点；Markdown Tab 用 `<pre>.textContent` 展示原文。搜索与正文的完整 JSON 都使用可折叠 JSON Tree 展示并支持复制，不执行 HTML，也不引入 Markdown renderer，从而避免 XSS。

## 错误与状态

统一的请求函数同时保留 HTTP status、JSON 响应和无法解析为 JSON 时的原始文本。页面分别展示 loading、empty、成功和失败状态；错误卡显示稳定错误码、message、retryable、request ID，并在 debug 响应存在时显示 `original_error`、attempts 和 artifacts。

## 验证

- 静态检查：JavaScript 语法、DOM ID 完整性、禁止外部 CDN、禁止把 API 数据写入 `innerHTML`。
- 浏览器检查：搜索参数、token sessionStorage、结果选择、正文读取、移动端布局。
- 后端未注册 `/v1/read` 时，正文面板应显示原始 404 响应，不影响搜索功能。
