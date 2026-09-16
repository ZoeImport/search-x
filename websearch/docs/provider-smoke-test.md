# Provider smoke test

测试时间：2026-07-16（Asia/Shanghai）

运行方式：根目录 `make run`，直接请求 `POST /v1/websearch`，query=`golang`。高级搜索与分页通过同一 Search-X API 完成端到端验收。

| Provider | HTTP | 结果 | 判定 |
| --- | ---: | --- | --- |
| Baidu | 200 | 3/3 返回，约 5.4s | 当前出口可用；未携带高级 query options 时可进入默认 Provider 链 |
| Bing | 200 | 3/3 返回，约 4.9s；UI 回归返回 10 条 | 可用；`meta.provider=bing`，点击首条结果后 Read 成功跟随跳转并提取 `go.dev` 正文 |
| Brave | 502 | `captcha_required` | Provider 已执行；当前出口遇到 Brave 安全验证，服务按约定返回 typed error |
| DuckDuckGo | 200 | 普通查询、高级查询和连续两页均返回 | 可用；真实 continuation form 被加密保存到 cursor，第二页与第一页 canonical URL 无重叠 |

本地 Demo 通过后端环境变量开启显式 Provider 选择；生产 Deployment 保持默认关闭。验证码结果依赖出口 IP 与上游状态，不能视为永久健康结论。

## Advanced search acceptance

Search-X 端到端实测覆盖：

- `filters.include_domains` / `exclude_domains`
- `query_options.exact_phrases` / `any_terms` / `exclude_terms`
- `query_options.title_terms` / `file_types`
- Provider capability filtering
- canonical URL metadata
- DuckDuckGo continuation cursor
- cursor fingerprint mismatch

DuckDuckGo 第一页返回 `go.dev/`、`go.dev/doc/`，第二页返回 `go.dev/doc/go1.26`、`exercism.org/tracks/go`，两页 canonical URL 交集为空。修改 cursor 所绑定的 query options 后，Search-X 返回 `cursor_mismatch`。

Brave 的普通查询与高级查询在当前出口均被上游挑战，说明该现象不是高级搜索 operator compiler 引入的差异。服务不会通过额外错误 `curl` 探测或复用挑战页面；挑战响应会映射为 `captcha_required`，Profile Pool 再依据请求结果、健康度和生命周期处理对应 profile。
