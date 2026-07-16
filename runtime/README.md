# Runtime

Search 和 Read 都实际依赖的公共 Go module。目前包含：

- 严格环境变量解析；
- stdout + 滚动文件双流 JSON logger；
- 统一 RFC 9457 风格 Problem Details；
- 加密安全的 request ID。

Runtime 不包含 Search/Read domain、Provider、Profile Pool、缓存、正文提取或业务配置。只有两边语义相同且都在生产代码使用的实现才能进入此 module。

```bash
go test ./...
```
