# 百度、Bing 与第三方 SERP API 定价调研

调研日期：2026-07-15。价格会变动，采购前应以链接所示的结算页为准。

## 结论

* 百度已有原厂的“百度搜索”API，后付费为 **￥0.036/次**（约 ￥36/千次）；预付费最低到 **￥30.6/千次**。每月赠送 1,500 次，默认 1 QPS，开通后付费默认 3 QPS。
* 微软的传统 Bing Search APIs 已于 2025-08-11 完全退役，不能将旧 Azure Bing API 当作可新购方案。官方建议迁移到 Grounding with Bing Search，但它面向 Azure AI Agent 的 LLM grounding，不等价于返回原始 SERP JSON。
* 若需要原样的 Bing / Baidu 搜索结果，实际常用的是第三方 SERP 提供商。低成本批量场景 DataForSEO 约 **$0.6–2/千页**；简单即时集成 SerpApi 约 **$3.75–25/千次**；Zenserp（Bing）约 **$0.9–2/千次**。

## 可比价格

| 服务 | 可搜索的引擎 | 公开价与套餐 | 换算为每千次/页 | 关键限制 |
| --- | --- | --- | --- | --- |
| 百度智能云 百度搜索（原厂） | 百度 | 后付费 ￥0.036/次；10k/100k/1m 次包分别 ￥352/￥3420/￥30600 | ￥36；￥35.2；￥34.2；￥30.6 | 每月免费 1,500 次（按天发），默认 1 QPS，开通付费默认 3 QPS。 |
| 百度智能云 智能搜索生成 | 百度 + LLM 总结 | ￥0.036/次，1 万/10 万/100 万次包同上 | 同上，另计模型 Token | 含基础搜索费；深搜索最多触发 10 次；每日 100 免费次并与普通搜索共享总调用上限。 |
| Microsoft Grounding with Bing Search | Bing 网页知识供 Azure AI Agent | Microsoft 官方退役公告未给出公开价格；Microsoft Learn Q&A 中 Azure SKU 显示 $35/千 transactions，应在 Azure 控制台复核 | 约 $35/千（待购买前复核） | 是 LLM grounding，而非原 Bing Search v7 API。 |
| SerpApi | Baidu、Bing（及其他） | 免费 250/月；$25/1k、$75/5k、$150/15k、$275/30k；$3,750/1m | $25、$15、$10、$9.17、$3.75 | 成功查询计费；同一响应返回 100 条也只算 1 次。 |
| DataForSEO SERP API | Baidu、Bing（及其他） | Standard normal $0.0006/页；priority $0.0012；Live $0.002 | $0.6、$1.2、$2 | 一页是 10 结果；深度每多 10 条加一页（后续页 75% 基础价）；最低充值 $50。Baidu 解码直链 `get_website_url=true` 为 10 倍。 |
| Zenserp | Bing（公开页面说明不为百度） | 免费 50/月；$49.99/25k、$149.99/100k、$299.99/250k、$899.99/1m | 约 $2、$1.5、$1.2、$0.9 | 第三方实时抓取；其页面明确声明未获搜索引擎背书/不使用搜索引擎官方 API。 |

## 选型含义

* 产品需要**合规的百度搜索能力**：优先百度智能云；十万次量包约 ￥3,420/年，适合中低并发在线查询。
* 必须拿**Bing/Baidu 原始 SERP、地区/设备、排名追踪**：选第三方 SERP。批量异步用 DataForSEO 最便宜；需要少量、同步、统一 SDK 则 SerpApi 更省接入成本。
* 要让 Agent 有**实时网页依据后生成答案**：百度“智能搜索生成”或 Azure Grounding with Bing Search，但都还要叠加模型 Token 成本，不能只按搜索请求预算。

## 一手来源

* 百度智能云：[百度搜索计费说明](https://cloud.baidu.com/doc/BAIDU_AI_SEARCH/s/Vmkmg3wm9)、[智能搜索生成计费说明](https://cloud.baidu.com/doc/BAIDU_AI_SEARCH/s/9mkmg2x9n)
* Microsoft：[Bing Search APIs 退役公告](https://learn.microsoft.com/en-us/lifecycle/announcements/bing-websearch-api-retirement)、[Grounding with Bing Search 文档](https://learn.microsoft.com/en-us/azure/foundry-classic/agents/how-to/tools-classic/bing-grounding)
* SerpApi：[套餐定价](https://serpapi.com/pricing)、[支持 Baidu/Bing 的 API 清单](https://serpapi.com/)
* DataForSEO：[SERP API 定价](https://dataforseo.com/apis/serp-api/pricing)、[Bing Organic 具体价格](https://dataforseo.com/pricing/serp/bing-organic-serp-api)、[深度与 Baidu 链接解码计费](https://dataforseo.com/help-center/serp-api-cost-explained)
* Zenserp：[定价](https://zenserp.com/pricing-plans/)、[Bing API 说明及非官方声明](https://zenserp.com/bing-websearch-api/)
