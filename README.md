# WebSearch / WebFetch Backend

工作区包含两个可独立部署的 Go 服务和一个公共 Runtime module。WebSearch 与 WebFetch 不互相 import，根目录 `go.work` 仅用于联合开发。对外测试统一通过 API Market 测试环境，`127.0.0.1:8080/8081` 仅供 API Market Executor 调用本地后端。

## 导航

- [WebSearch README](websearch/README.md) · [config](websearch/config.yaml) · [OpenAPI](websearch/openapi/openapi.yaml) · [curl](websearch/examples/curl/websearch.sh) · [Python](websearch/examples/python/websearch.py) · [Go](websearch/examples/go/main.go)
- [WebFetch README](webfetch/README.md) · [config](webfetch/config.yaml) · [OpenAPI](webfetch/openapi/openapi.yaml) · [curl](webfetch/examples/curl/webfetch.sh) · [Python](webfetch/examples/python/webfetch.py) · [Go](webfetch/examples/go/main.go)
- [统一接口文档](docs/api-reference.md) · [本地 Demo](demo/README.md) · [Runtime](runtime/README.md)
- [部署清单](websearch/deploy/k8s/deployment.yaml) · [WebFetch 部署清单](webfetch/deploy/k8s/deployment.yaml)
- [官方售卖商契约兼容性调研](docs/research/vendor-contract-compatibility-2026-07-16.md) · [商用接口对比](docs/research/commercial-search-read-api-comparison.md)
- [基准测试](docs/benchmarks/2026-07-15-query-fetch.md) · [WebSearch smoke test](websearch/docs/provider-smoke-test.md) · [测试目录](websearch/internal/api/httpapi/router_test.go) · [WebFetch 测试目录](webfetch/internal/api/httpapi/router_test.go)

## 本地运行

```bash
make run
```

也可分别运行 `make run-websearch`、`make run-webfetch`、`make run-demo`。配置优先级为：代码默认值 < `config.yaml` < 环境变量；两个服务都支持 `-config` 指定 YAML。

本仓库的 curl、Python、Go 和 Demo 示例默认调用：

```text
https://tapi.insmtx.com/v6/se/general/search
https://tapi.insmtx.com/v6/se/general/fetch
```

调用前设置 `API_KEY`。请求使用 `Authorization: Bearer <API_KEY>`，业务参数放在 `request` 字段中，业务响应位于网关返回的 `response` 字段。

## 验证

```bash
make check
```

该命令执行全部 module 的单测、race、vet 和 build。
