# Local Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建一个可直接验证搜索和正文读取 API 的精致本地静态页面。

**Architecture:** 使用无构建 Vanilla HTML/CSS/JavaScript。`index.html` 定义语义结构，`app.css` 负责响应式视觉系统，`app.js` 负责参数、请求、状态和安全 DOM 渲染。

**Tech Stack:** HTML5、CSS3、ES2022 Fetch API、sessionStorage；无外部依赖。

## Global Constraints

- 不修改 Go router、bootstrap 或后端业务文件。
- 所有 API 数据只通过 `textContent` 渲染，禁止 `innerHTML` 注入。
- Debug Token 只存入 `sessionStorage`，只通过 `X-Debug-Token` Header 发送。
- 页面不得引用外部 CDN、字体或图片。

---

### Task 1: 页面结构与视觉系统

**Files:**
- Create: `webui/index.html`
- Create: `webui/app.css`

**Interfaces:**
- Produces: 表单元素 ID、搜索结果容器、诊断面板、正文阅读面板，供 `app.js` 查询和更新。

- [x] **Step 1:** 创建包含搜索参数、双栏工作区和可访问状态区域的语义 HTML。
- [x] **Step 2:** 创建自适应 CSS，在桌面显示双栏、窄屏显示单栏，并覆盖 loading、empty、error、selected 状态。
- [x] **Step 3:** 检查页面不包含外部资源 URL。

### Task 2: 搜索请求与诊断渲染

**Files:**
- Create: `webui/app.js`

**Interfaces:**
- Consumes: `GET /v1/search?q=&provider=&limit=&page=&refresh=&debug=`。
- Produces: `requestJSON()`、`runSearch()`、`renderSearchResponse()` 和安全 DOM helper。

- [x] **Step 1:** 实现 API Base URL 归一化、查询参数生成、token sessionStorage 和统一错误保留。
- [x] **Step 2:** 使用 `textContent` 渲染结果卡、结果级 Provider、meta、warnings、debug attempts、artifacts 和错误原始信息；用 DOM API 构建可折叠完整 JSON Tree 与复制操作。
- [x] **Step 3:** 实现 loading、empty、重试与分页交互。

### Task 3: 正文读取与静态验证

**Files:**
- Modify: `webui/app.js`

**Interfaces:**
- Consumes: `POST /v1/read` JSON `{url, format, max_chars, refresh, debug}`。
- Produces: `readResult()` 和正文、元数据、warnings、debug 错误视图。

- [x] **Step 1:** 点击结果卡时发起读取请求，并通过 `X-Debug-Token` Header 复用授权。
- [x] **Step 2:** 提供 Preview、Markdown、JSON、Debug Tabs；使用安全 DOM API 渲染 Markdown 子集并用 `<pre>.textContent` 保留 Raw Markdown，显示 final URL、作者、时间、语言、transport、extractor、缓存与截断信息。
- [x] **Step 3:** 执行 `node --check webui/app.js`、外部资源扫描和 `innerHTML` 扫描，修正全部问题。
