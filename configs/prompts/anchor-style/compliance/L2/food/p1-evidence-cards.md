# L2 · 食品 P1 资质与事实依据卡母版

## 来源与当前状态

从用户提供的 `D:\电商\小蓝直播搭子\规则\抖音食品带货-违禁词表与硬拦截规则.md` 第三节、附表及第六节提取。卡片母版与数据协议已经建立；测试页已按本母版标注P1候选词为粉红，但尚未接入资质登记数据库、事实包登记页面或运行审核。

P1 表示本系统选定的“需要相应依据才能宣传”的业务档位，不表示每个词在任何语境都违法，也不表示有任意一张报告就能宣传所有相关功效。只登记报告或凭证实际支持的商品、批次、指标、范围与有效时间；不得将营养指标升级成治疗、保健或人群安全承诺。

本文件是食品行业 L2 母版，不能覆盖 L1 P0。`非转基因更安全` 不建立可放行资质卡，仍归 P0。普通的“非转基因”只登记原料/产品属性依据。

## 卡片实例化与存放

同一租户、方案、商品范围及批次下，`review_identity=atomic_attribute` 的同类属性声明按规则ID合并为一张卡，保留所有命中来源。商品名、事实依据和角度中重复出现不重复确认。未配置语义归并的规则、带具体检测数值/统计口径的表达继续按原文分别审核。商品范围不明时不把另一商品的确认套用过来；批次或规则版本变化须重新确认。

1. 仅从已确认事实、商品卡、属性和福利中匹配声明，不把主播风格样本里的数字或模型猜测当成事实。生成话术中新出现的 P1 声明先作为待核对候选，不自动变成有效事实。
2. 匹配声明后按“方案 + 商品/链接 + 声明 + 所需证据类型”登记卡片，关联原事实 ID、原文与位置。重复匹配合并来源，不能把一个商品的报告复制给另一个商品。
3. 用户要求的“默认有资质/有纸质”体现在初始声明字段：`declared_has_evidence=true`、`storage_form=paper`。其含义是待用户确认的默认选项，不是程序已取得、验真或挂载了一份证明。
4. 页面按检测报告、认证证书、经营/生产证明、真实数据分卡；用户可确认有、确认无、撤回确认，并填写凭证名称、编号、证明内容、涉及商品/批次与有效期。纸质材料允许登记；不强制上传扫描件，但不得编造编号、机构或日期。
5. 卡片稳定版本保存在数据库，按租户、方案、商品和版本隔离；房间读取所关联方案。本场快照加载 Redis，结束清理，不从记录恢复直播进度。母版更新不改写已确认卡片。
6. 初始卡片 `confirmation_status=pending`，不能仅凭默认有资质放行。确认无、撤回、过期、对象不匹配或证明范围不足时，命中 P1 按硬拦处理，局部重写最多 3 次后跳过；确认有且登记的依据与当前声明匹配后，才满足本系统 P1 依据检查。
7. 用户确认与程序范围检查并非第三方验真。保留 `verification_status`，不能把 `user_confirmed` 展示成“官方已认证”。P1 满足也不能豁免 P0 或保证平台放行。
8. 数量、比率、经营年限必须核对具体数值与时间口径，不是有一个“销量证明”标签就允许任意喊数。营业执照不能自动证明未登记的传承、工艺或实际生产过程。

## 结构化母版

共 18 种声明卡：11 个产品/营养属性，4 个经营/工艺声明，3 个真实数据声明。`高油酸` 来自原文第六节；`39年` 作为经营年限实例，不能把所有主播写死成 39 年。

```json
{
  "schema_version": 1,
  "layer": "L2",
  "industry": "ordinary_food",
  "severity": "P1",
  "policy_version": "2026-10-08-food-p1-v1",
  "status": "mother_only",
  "policy_basis": "user_selected_conservative_policy",
  "execution": {
    "instantiate_from": "confirmed_fact_package",
    "new_generated_claim": "pending_review_not_active_fact",
    "default_declaration_is_proof": false,
    "missing_evidence_action": "hard_block_and_local_rewrite",
    "max_rewrites": 3,
    "on_exhausted": "skip_current_selling_point_angle",
    "p0_overrides_p1": true
  },
  "card_defaults": {
    "declared_has_evidence": true,
    "storage_form": "paper",
    "confirmation_status": "pending",
    "verification_status": "unverified",
    "evidence_refs": [],
    "confirmed_by_user_id": null,
    "confirmed_at": null
  },
  "card_instance_contract": {
    "required_fields": ["tenant_id", "plan_id", "version", "card_key", "template_id", "subject_key", "claim_text", "source_fact_refs", "declared_has_evidence", "storage_form", "confirmation_status", "verification_status", "evidence_refs"],
    "confirmation_status_values": ["pending", "confirmed_has", "confirmed_none", "revoked"],
    "verification_status_values": ["unverified", "user_confirmed", "externally_verified"],
    "storage_form_values": ["paper", "digital", "both", "unknown"],
    "source_ref_fields": ["source_type", "source_id", "field", "original_text", "original_start", "original_end"],
    "evidence_ref_fields": ["evidence_type", "title", "reference_number", "issuer", "proof_summary", "subject_keys", "batch_keys", "valid_from", "valid_until", "statistics_period", "statistics_method", "supported_claims", "supported_values", "attachment_refs"],
    "unknown_evidence_field_value": null,
    "release_checks": ["confirmation_status=confirmed_has", "declared_has_evidence=true", "evidence_refs_not_empty", "matching_subject_and_batch_if_applicable", "proof_summary_and_supported_claims_cover_current_claim", "matching_values_and_statistics_period_if_applicable", "not_expired_or_revoked", "no_p0_hit"]
  },
  "card_templates": [
    {"id":"food.p1.cholesterol_zero","title":"零胆固醇依据","review_identity":"atomic_attribute","category":"product_report","terms":["零胆固醇"],"patterns":[],"required_evidence_types":["test_report"],"required_proof":"本商品胆固醇指标及零含量表述依据","source_section":"三"},
    {"id":"food.p1.fat_zero","title":"零脂肪依据","review_identity":"atomic_attribute","category":"product_report","terms":["零脂肪"],"patterns":[],"required_evidence_types":["test_report"],"required_proof":"本商品脂肪指标及零含量表述依据","source_section":"三"},
    {"id":"food.p1.fat_low","title":"低脂依据","review_identity":"atomic_attribute","category":"product_report","terms":["低脂"],"excluded_terms":["低脂肪酸"],"patterns":[],"required_evidence_types":["test_report"],"required_proof":"本商品低脂声明对应指标；不能把低脂肪酸等同低脂食品","source_section":"三"},
    {"id":"food.p1.erucic_low","title":"低芥酸依据","review_identity":"atomic_attribute","category":"product_report","terms":["低芥酸"],"patterns":[],"required_evidence_types":["test_report"],"required_proof":"本商品或对应批次芥酸指标；不延伸为治疗或特定人群安全结论","source_section":"三"},
    {"id":"food.p1.salt_low","title":"低盐依据","review_identity":"atomic_attribute","category":"product_report","terms":["低盐"],"patterns":[],"required_evidence_types":["test_report"],"required_proof":"本商品钠/盐声明对应指标及表述依据","source_section":"三"},
    {"id":"food.p1.sugar_zero","title":"无糖依据","review_identity":"atomic_attribute","category":"product_report","terms":["无糖"],"patterns":[],"required_evidence_types":["test_report"],"required_proof":"本商品糖含量与无糖表述依据","source_section":"三"},
    {"id":"food.p1.no_additives","title":"无添加依据","category":"product_report","terms":["无添加"],"patterns":[],"required_evidence_types":["test_report_and_ingredient_process_records"],"required_proof":"明确没有添加的具体成分、配料及生产依据；不得泛化成所有添加物均无","source_section":"三"},
    {"id":"food.p1.non_gmo","title":"非转基因属性依据","review_identity":"atomic_attribute","category":"product_report","terms":["非转基因"],"patterns":[],"required_evidence_types":["test_report_or_certificate"],"required_proof":"本商品/原料对应报告或凭证及追溯关联；不支持非转基因更安全","source_section":"三"},
    {"id":"food.p1.organic","title":"有机认证依据","review_identity":"atomic_attribute","category":"certification","terms":["有机"],"patterns":[],"required_evidence_types":["organic_certificate"],"required_proof":"对应商品在有效认证范围内；不能用一般检测报告代替认证","source_section":"三"},
    {"id":"food.p1.green_food","title":"绿色食品认证依据","review_identity":"atomic_attribute","category":"certification","terms":["绿色食品"],"patterns":[],"required_evidence_types":["green_food_certificate"],"required_proof":"对应商品的认证证书、范围及有效期","source_section":"三"},
    {"id":"food.p1.oleic_high","title":"高油酸依据","review_identity":"atomic_attribute","category":"product_report","terms":["高油酸"],"patterns":[],"required_evidence_types":["test_report"],"required_proof":"本商品油酸指标与高油酸表述依据；不延伸为心血管治疗承诺","source_section":"六"},
    {"id":"food.p1.own_production_sales","title":"自产自销依据","category":"business_origin","terms":["自产自销"],"patterns":[],"required_evidence_types":["business_license_and_production_sales_records"],"required_proof":"生产主体与销售主体及实际生产销售关系；不凭执照单独推定自产","source_section":"三"},
    {"id":"food.p1.business_years","title":"经营年限依据","category":"business_origin","terms":["39年"],"patterns":["[0-9零〇一二三四五六七八九十百两]{1,8}年(?:老店|老油坊|油坊|老作坊|老字号)","(?:做了|开了|经营了|干了)[0-9零〇一二三四五六七八九十百两]{1,8}年","八几年就榨"],"required_evidence_types":["business_license_and_operating_history"],"required_proof":"声明主体的实际经营起始时间、连续经营记录及声明时点；按事实实例化年份","source_section":"三、六"},
    {"id":"food.p1.heritage","title":"祖传与传承依据","category":"business_origin","terms":["祖传","祖祖辈辈传下来"],"patterns":[],"required_evidence_types":["heritage_and_operating_history"],"required_proof":"本主体的传承记录；不把经营年限自动推成祖传","source_section":"三、六"},
    {"id":"food.p1.old_craft","title":"老手艺依据","category":"business_origin","terms":["老手艺"],"patterns":[],"required_evidence_types":["craft_and_operating_history"],"required_proof":"当前商品工艺与实际工艺经营记录对应","source_section":"三"},
    {"id":"food.p1.sales","title":"销量与回购数量依据","category":"verifiable_data","terms":["销量"],"patterns":["(?:卖了|售出|卖出|回购了)[0-9零〇一二三四五六七八九十百千万亿两多余来.]{1,16}(?:单|桶|件)"],"required_evidence_types":["verifiable_sales_records"],"required_proof":"商品范围、订单口径、统计周期与真实数量；默认有报告不能支持80万单","source_section":"三、六"},
    {"id":"food.p1.repurchase_rate","title":"回购率依据","category":"verifiable_data","terms":["回购率"],"patterns":[],"required_evidence_types":["verifiable_repurchase_statistics"],"required_proof":"对应商品的回购定义、分子分母、周期和实际比例","source_section":"三"},
    {"id":"food.p1.positive_review_rate","title":"好评率依据","category":"verifiable_data","terms":["好评率"],"patterns":[],"required_evidence_types":["verifiable_review_statistics"],"required_proof":"对应商品的评价范围、样本数、统计周期和实际比例","source_section":"三"}
  ],
  "excluded_p0_claims": ["非转基因更安全"],
  "source_conflict_resolution": "永久禁用及P0命中优先；数据类P1卡不能绕过当前P0未接证据核验的保护"
}
```

## 对现有程序的接入要求

母版尚未被当前 P0 解析器读取，不能把 `P1` JSON 混入其只接受 `P0` 的 schema。后续接入须同时实现卡片模型、数据库版本化、事实包引用、页面确认、证据范围检查与日志。

原文的营养词按“检测报告/证书”概括，以上证据类型与匹配字段是为登记而细分的数据设计，不是新增法定许可清单。不得从一张表自行判断商品已经有证，或自动填写未见过的检测数值。

当前 P0 条件型数据规则尚未接证据核验：即便有 P1 销量卡，当前运行仍按 P0 保守拦截，不能宣称登记卡已经改变运行放行行为。
