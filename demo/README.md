# Demo

静态演示页面默认调用 API Market 测试环境：

```text
POST https://tapi.insmtx.com/v6/se/general/search
POST https://tapi.insmtx.com/v6/se/general/fetch
```

页面要求输入已申请这两个接口权限的 API Key，并发送 `Authorization: Bearer <API_KEY>`。API Key 只保存在当前浏览器标签页的 `sessionStorage`。

启动静态页面：

```bash
cd /Users/zoe/Documents/daily/web-search-backend
make run-demo
```

测试环境网关必须允许 `http://127.0.0.1:8090` 的浏览器 CORS；如果未开放 CORS，请使用 curl、Python 或 Go 示例测试。
