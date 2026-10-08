# L2 · 食品 P2 风险表达安全改写母版

## 来源与范围

从用户提供的 `D:\电商\小蓝直播搭子\规则\抖音食品带货-违禁词表与硬拦截规则.md` 第四节提取 6 组原始映射，第六节补充 3 项情境型表达。MD与数据协议已建立；测试页已按本母版标注P2候选词为大红，但尚未接入运行情境判定与替换器。

P2 是用户选定的边缘风险与表达优化策略，不将清单中每个句子一概定性为违法。原文建议的替换也不天然正确：替换后必须仍由本场事实支持，且全文重新经过 P0/P1。

优先级为 `P0 > P1依据不足 > P2`。例如“绝对放心”同时命中 P0“绝对”，不能先用 P2 换掉词就把同一候选视为已经通过 P0。原文第六节将部分比价示例归为 P0；保留其 P2 改写建议用于后续重写，不覆盖已生效的 P0 检查。

## 执行约定

1. 只修改命中的表达及必要相邻语法，保留主播腔调。原始模型返回不覆盖；原文位置、规则 ID、替换前后与原因后台留档。
2. 先检查原稿 P0 和 P1；只有没有更高档阻断时，才应用 P2 自动替换。
3. 原始映射保存在 `source_replacement`，不强制视为已获事实支持的成品。使用 `preferred_replacement` 前核对 `required_fact_keys`；没有依据时使用不新增事实的 `fallback_replacement`。
4. “老粉丝都回购”必须有对应真实回购依据，不能为了删“一定不会让你失望”而制造新的回购事实。已有依据也不得使用“都”泛化成未经支持的全体结论。
5. “省了健康钱”不能改出治疗或人群安全保证。本母版采用不新增健康断言的回退表达。
6. “都拍到了不？”是问句，单独出现不认定实际已经下单；不当成销量事实。只有结合库存、倒计时、虚构抢购等具体上下文触发风险时才处理。
7. “没得了”“错过就真没得了”需要与本场库存/活动依据一致；有适用依据时不机械删除主播方言，缺依据时去掉稀缺承诺。不能把活动结束时间当库存证明。
8. 组装后全文再次检查 P0/P1、数字和事实引用。不放行替换产生的新违规，也不反复循环替换；仍失败沿用局部重写最多3次、耗尽跳过的硬拦流程。
9. 词表与正文维护在本 MD；后续房间运行读取行业规则快照。用户方案的事实和证据保存在隔离的数据库版本，不能修改公用规则来豁免。

## 结构化母版

```json
{
  "schema_version": 1,
  "layer": "L2",
  "industry": "ordinary_food",
  "severity": "P2",
  "policy_version": "2026-10-08-food-p2-v1",
  "status": "mother_only",
  "policy_basis": "user_selected_conservative_policy",
  "execution": {
    "precedence": ["P0", "P1_missing_evidence", "P2"],
    "action": "replace_matched_expression_then_recheck_whole_candidate",
    "preserve_raw_response": true,
    "require_facts_for_new_claims": true,
    "max_replacement_passes": 1,
    "after_failed_recheck": "hard_block_and_local_rewrite",
    "max_rewrites": 3,
    "on_exhausted": "skip_current_selling_point_angle"
  },
  "normalization": {
    "unicode_form": "NFKC",
    "case_fold": true,
    "remove_zero_width": true,
    "ignore_spaces_for_match": true,
    "keep_original_offsets": true
  },
  "rules": [
    {"id":"food.p2.purchase_profit","severity":"P2","category":"诱导式收益表达","terms":["买到就是赚到"],"patterns":[],"condition":"always_if_no_higher_priority_block","source_replacement":"买得值","preferred_replacement":"买得值","required_fact_keys":[],"fallback_replacement":"按你们实际需要选","source_section":"四","note":"不新增价格或收益数字"},
    {"id":"food.p2.health_savings","severity":"P2","category":"健康暗示","terms":["省了健康钱"],"patterns":[],"condition":"always_if_no_higher_priority_block","source_replacement":"吃得安心","preferred_replacement":"选自己家适合的","required_fact_keys":[],"fallback_replacement":"选自己家适合的","source_section":"四","note":"原文候选保留；运行优先不用替换制造新的健康安全断言"},
    {"id":"food.p2.unit_price","severity":"P2","category":"单价式比价表达","terms":["划下来才13块一斤","划下来才13块钱一斤"],"patterns":[],"condition":"always_if_no_higher_priority_block","source_replacement":"一桶算下来挺实惠","preferred_replacement":"一桶算下来挺实惠","required_fact_keys":["matching_current_product_price_and_spec"],"fallback_replacement":"按你们实际需要选","source_section":"四","note":"仅提取原文给出的数字实例；未来扩展金额匹配须核对算法、舍入和现价，不把13写死成商品事实"},
    {"id":"food.p2.supermarket_comparison","severity":"P2","category":"无依据比较","terms":["比超市划算多了"],"patterns":[],"condition":"always_if_no_higher_priority_block","source_replacement":"我们家价格实在","preferred_replacement":"我们家价格实在","required_fact_keys":["matching_current_product_price"],"fallback_replacement":"按你们实际需要选","source_section":"四、六","note":"若原稿已命中P0贬低规则，走P0重写；P2不能豁免"},
    {"id":"food.p2.absolute_assurance","severity":"P2","category":"绝对化安抚","terms":["绝对放心"],"patterns":[],"condition":"always_if_no_higher_priority_block","source_replacement":"放心买","preferred_replacement":"先看清楚再选","required_fact_keys":[],"fallback_replacement":"先看清楚再选","source_section":"四","note":"当前P0包含绝对；该映射供重写参考，不用于跳过原稿P0拦截"},
    {"id":"food.p2.disappointment_guarantee","severity":"P2","category":"效果与信任保证","terms":["一定不会让你失望"],"patterns":[],"condition":"always_if_no_higher_priority_block","source_replacement":"老粉丝都回购","preferred_replacement":"有老粉丝回购","required_fact_keys":["verified_matching_product_repeat_purchase_records"],"fallback_replacement":"你们按自己的需要选","source_section":"四","note":"无回购事实时必须回退，不能把称呼老粉当回购证明"},
    {"id":"food.p2.purchase_question","severity":"P2","category":"情境型催问","terms":["都拍到了不？","都拍到了不?"],"patterns":[],"condition":"context_includes_unsupported_scarcity_or_purchase_claim","source_replacement":null,"preferred_replacement":"链接看清楚了不？","required_fact_keys":["matching_product_link"],"fallback_replacement":"按你们实际需要选","source_section":"六","note":"独立问句保留；不凭此问句推定成交或销量。上下文判断不能只靠字面出现"},
    {"id":"food.p2.no_stock_dialect","severity":"P2","category":"情境型库存稀缺","terms":["没得了"],"patterns":[],"condition":"inventory_claim_without_matching_current_inventory_basis","source_replacement":null,"preferred_replacement":"按你们实际需要选","required_fact_keys":[],"fallback_replacement":"按你们实际需要选","source_section":"六","note":"先识别是否库存/稀缺承诺；有实际库存依据时保留方言，其他语义不能误拦"},
    {"id":"food.p2.miss_out","severity":"P2","category":"情境型错失承诺","terms":["错过就真没得了"],"patterns":[],"condition":"scarcity_claim_without_matching_inventory_or_activity_basis","source_replacement":null,"preferred_replacement":"按你们实际需要选","required_fact_keys":[],"fallback_replacement":"按你们实际需要选","source_section":"六","note":"有适用依据也不得扩大为永不再有；优先长词匹配，避免被没得了重复替换"}
  ],
  "logging": {
    "fields": ["tenant_id", "plan_id", "room_id", "run_id", "candidate_id", "rule_id", "rule_sha256", "original_start", "original_end", "before", "after", "replacement_source", "fact_refs", "recheck_result", "latency_ms"],
    "frontend_risk_highlight": "original_matched_ranges_red",
    "frontend_show_extra_metrics": false
  }
}
```

## 接入边界

6 组字面映射可做确定性程序替换；另 3 项带情境判断，不得当成“只要出现就替换”的永久黑名单。原文没有给出后三项固定安全句，回退文案是本次工程设计补充，已与原始映射分开。

后续实现须支持全部字段、最长匹配、重叠处理、原文位置、事实条件、P0优先与替换后全文复查。未知条件报错，不默默当成已经满足。P2母版不会被目前只支持P0的解析器自动启用。
