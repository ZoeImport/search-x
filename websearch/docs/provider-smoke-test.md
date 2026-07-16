# Provider smoke test

测试时间：2026-07-16（Asia/Shanghai）

运行方式：根目录 `make run`，直接请求内部后端 `POST /v1/websearch`，query=`golang`。该文件记录 Provider 层验证，不是 API Market 客户端调用示例。

| Provider | HTTP | 结果 | 判定 |
| --- | ---: | --- | --- |
| Baidu | 502 | `captcha_required` | Provider 已执行；当前出口遇到百度安全验证，服务按约定返回 typed error |
| Bing | 200 | 3/3 返回；UI 回归返回 10 条 | 可用；`meta.provider=bing`，点击首条结果后 Read 成功跟随跳转并提取 `go.dev` 正文 |
| Brave | 502 | `captcha_required` | Provider 已执行；当前出口遇到 Brave 安全验证，服务按约定返回 typed error |
| DuckDuckGo | 200 | 3/3 返回 | 可用；`meta.provider=duckduckgo` |

本地 Demo 通过后端环境变量开启显式 Provider 选择；生产 Deployment 保持默认关闭。验证码结果依赖出口 IP 与上游状态，不能视为永久健康结论。
