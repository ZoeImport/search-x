# Demo

静态演示页面默认调用 JX-LAN 测试入口：

```text
POST https://tapi.juxonmedia.com/v1/websearch
POST https://tapi.juxonmedia.com/v1/webfetch
```

启动静态页面：

```bash
make run-demo
```

公共 ingress 需要允许页面 origin 的浏览器 CORS。若未开放 CORS，请使用 curl、Python 或 Go 示例测试；JXX 后端集成不受浏览器 CORS 影响。
