# JXX integration and JX-LAN operations

## Ownership boundary

Search-X 是独立项目，提供搜索与正文读取能力。JXX 负责决定何时调用、如何向最终用户授权、如何限额，以及如何保存业务结果；Search-X 不读取 JXX Session、Cookie 或业务数据库。

JXX 在 K3s 内应调用：

```text
http://websearch-api.jxx.svc.cluster.local:8080/v1/websearch
http://webfetch-api.jxx.svc.cluster.local:8081/v1/webfetch
```

`https://tapi.juxonmedia.com/v1/websearch` 与 `/v1/webfetch` 是测试和运维验收入口，不应成为 JXX 的默认内部地址。

## Client contract

- 每次调用设置总超时；建议 WebSearch/WebFetch 从 20s 开始，绝不超过服务允许的 60s。
- 透传或生成 `X-Request-ID`，并记录响应的 `request_id`。
- 只在 problem body 明确返回 `retryable: true` 时进行有界重试；建议最多 2 次并使用指数退避与 jitter。
- WebSearch 分页必须原样回传 `page.next_cursor`，并保持 query、limit、filters、query options 与 Provider 顺序不变。
- WebFetch 的 URL 是不受信任输入。即便服务已有 SSRF 防护，JXX 仍应在产品层限制谁能发起读取、频率与可接受结果大小。
- 不要把 Search-X 当作事实数据库；搜索缓存和正文缓存都是可丢弃投影。

## Availability behavior

| 场景 | JXX 建议行为 |
| --- | --- |
| 400/403/413/415/422 | 向调用方返回稳定的不可重试业务错误 |
| 502/503/504 且 `retryable=true` | 有界重试；预算耗尽后返回依赖暂不可用 |
| WebSearch cursor 过期或不匹配 | 让调用方从第一页重新开始，不替换 cursor 内容 |
| WebFetch 上游网页不可读 | 保留原始 `code` 与 `request_id`，不要把空正文当成功 |

## Kubernetes contract

- Namespace: `jxx`
- Services: `websearch-api:8080`, `webfetch-api:8081`
- Public ingress host: `tapi.juxonmedia.com`
- Probes: `GET /healthz`, `GET /readyz`
- Service type: `ClusterIP`
- NetworkPolicy: 只允许 `kube-system/traefik` 与 `jxx` namespace 的 Pod 访问服务端口
- WebSearch 的 `WEBSEARCH_CURSOR_KEY` 由 `search-x-config` Secret 提供；Secret 不进入 Git
- 镜像必须使用不可变 tag 与 registry digest，不使用 `latest`

部署权威清单位于 `JUXON-AI/k3syaml`。回滚时恢复两个 Deployment 先前记录的完整 `tag@sha256:digest`，随后重新验证 rollout、内部 Service DNS 和两个公共 POST 路由。

## Smoke requests

```bash
curl --fail-with-body https://tapi.juxonmedia.com/v1/websearch \
  -H 'Content-Type: application/json' \
  --data '{"query":"JUXON AI","limit":3,"timeout":"20s"}'

curl --fail-with-body https://tapi.juxonmedia.com/v1/webfetch \
  -H 'Content-Type: application/json' \
  --data '{"url":"https://example.com/","timeout":"20s","output":{"format":"text","max_chars":3000}}'
```

公共测试路由当前不执行应用层鉴权。若要提供给不受信任客户端，必须先在边缘增加鉴权、调用配额、滥用防护和审计，不能依赖 CORS 或 NetworkPolicy 代替身份验证。
