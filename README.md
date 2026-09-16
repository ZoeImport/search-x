# Search-X

Search-X 是独立的 Web 搜索与网页正文读取项目。工作区包含两个可独立部署的 Go 服务和一个公共 Runtime module；WebSearch 与 WebFetch 不互相 import，根目录 `go.work` 仅用于联合开发。

## 服务

| 服务 | 公共测试入口 | JXX 集群内入口 |
| --- | --- | --- |
| WebSearch | `POST https://tapi.juxonmedia.com/v1/websearch` | `http://websearch-api.jxx.svc.cluster.local:8080/v1/websearch` |
| WebFetch | `POST https://tapi.juxonmedia.com/v1/webfetch` | `http://webfetch-api.jxx.svc.cluster.local:8081/v1/webfetch` |

健康检查分别为 `GET /healthz` 和 `GET /readyz`，只供 Kubernetes probe 与集群内诊断使用，不通过公共 ingress 暴露。

## 本地验证

```bash
make check
```

## 文档

- [统一 API 契约](docs/api-reference.md)
- [JXX 集成与运行手册](docs/jxx-integration.md)
- [WebSearch OpenAPI](websearch/openapi/openapi.yaml)
- [WebFetch OpenAPI](webfetch/openapi/openapi.yaml)

`k3syaml` 是 JX-LAN 测试集群部署清单的权威来源。仓库内 `deploy/k8s` 文件仅作为服务自身的资源与 probe 示例，不能替代带不可变镜像 digest 的集群发布清单。
