# L1 · P0 话术硬拦截母版

## 用途与范围

这是公用合规规则母版，仅建立 **P0**。当前已接入本地测试平台的大循环候选采纳、局部重写和声音缓存检查；不含 P1 资质规则、P2 替换规则，不代表已建立向量库或已部署生产服务器。

来源：用户提供的 `D:\电商\小蓝直播搭子\规则\抖音食品带货-违禁词表与硬拦截规则.md` 中 P0 部分，以及用户确认的执行要求。

本表作为系统保守的业务检测策略。原文中“所有这些词在任何语境下一律违法”的概括不作为法律结论；法律认定仍取决于具体内容和场景。除极少数明确硬规则外，程序字面命中只进入语义复核，不直接等同于 P0；例如“老字号”可能是资质声明、经营年限事实或字段名称。不得据此宣称词表能识别全部违法表达或保证平台放行。

规则正文、匹配词、正则、适用范围和重写次数均在本 MD 中维护，不把词表写死在代码里。未来 L2 可增加行业限制，但房间风格、评分、事实卡和资质声明不能豁免已经命中的 P0。

## 执行约定

1. 每场启动时加载母版、记录文件 SHA256、编译匹配器，将本场规则快照放入 Redis 或内存。母版更新用于下一场；不以浏览器旧正文覆盖新母版。
2. 生成后的候选稿先完整扫描，审核通过才允许采纳、进入风格记忆、TTS 或声音缓存。已有缓存复用前也检查当前规则。
3. 命中时保存原文、规则 ID、原始字符位置和所在句段。优先只重写违规句段，不让模型改动未命中的其它部分；程序组装后重新检查全文。
4. 首次生成被拦截后，最多再重写 **3 次**。仍不通过则记录跳过当前卖点×角度，不选“违规稿里最高分”的版本，也不把跳过记录当已播出记忆。
5. 本场跳过的组合不要立即重新选回。可用组合耗尽或连续跳过达到保护上限时暂停，不能无限请求。
6. 公用规则及后续规则向量可共享；房间事实依据与资质声明独立保存，不混入公用向量库。规则使用日志和耗时只在后台留档，前台不额外展示。
7. 医疗功效规则在本母版中标记 `ordinary_food` 范围，不将普通食品的宣传限制误套到有合法许可的药品业务；具体许可业务需要对应行业规则。
8. 无依据限时、数据和荣誉先由程序命中，再进行语义复核。语义判断为可核验声明时，必须由当前方案所属用户补充与本商品、本场、本时间一致的实际依据；复制事实包数字或“默认有资质”的声明不能绕过。
9. 归一化用于匹配，不修改原始返回；字符位置必须能映射回原文。未知适用范围、条件或正则结构明确报错，不默默跳过规则。

## 结构化母版

每条规则含稳定 ID、档位、类别、适用范围、触发条件、复核模式、字面词和正则。`review_mode=hard` 才是程序直接硬拦；`review_mode=semantic` 表示程序命中后必须结合完整语境复核。`when_unsubstantiated` 语义成立时还需匹配的可核验依据。`ordinary_food` 只作用于普通食品，不作为全行业医疗词黑名单。

保留原文 P0 词表，不为凑数量补造新的禁词。原文末尾单列的“永久禁用”表达独立保存，并标明是用户选定的食品业务保守策略，而非未经核验的全行业法定禁词。

```json
{
  "schema_version": 1,
  "layer": "L1",
  "severity": "P0",
  "policy_version": "2026-10-08-p0-v3",
  "status": "workbench_runtime_supported",
  "policy_basis": "user_selected_conservative_policy",
  "execution": {
    "max_rewrites": 3,
    "max_consecutive_skips": 5,
    "default_scope": "ordinary_food",
    "rewrite_scope": "matched_sentence_or_paragraph",
    "recheck_scope": "whole_candidate",
    "on_exhausted": "skip_current_selling_point_angle",
    "count_rejected_as_adopted": false,
    "allow_user_override": false,
    "frontend_show_metrics": false,
    "log_fields": ["tenant_id", "plan_id", "room_id", "run_id", "candidate_id", "rule_version", "rule_sha256", "rule_id", "matched_text", "original_start", "original_end", "rewrite_number", "decision", "latency_ms"]
  },
  "normalization": {
    "unicode_form": "NFKC",
    "case_fold": true,
    "remove_zero_width": true,
    "ignore_spaces_for_match": true,
    "keep_original_offsets": true,
    "aliases": {"蕞": "最", "第①": "第一", "NO.1": "第一", "NO1": "第一", "Top1": "第一", "TOP1": "第一"}
  },
  "rules": [
    {
      "id": "l1.p0.absolute_claim",
      "severity": "P0",
      "category": "极限词与绝对化宣传",
      "scope": "all",
      "condition": "always",
      "review_mode": "semantic",
      "terms": ["最好", "最佳", "最优", "最大", "最高", "最低", "最低价", "最便宜", "最优惠", "最划算", "最先进", "最受欢迎", "最流行", "最时尚", "极致", "顶级", "极品", "顶尖", "终极", "第一", "中国第一", "全球第一", "全网第一", "销量第一", "排名第一", "第一品牌", "唯一", "首个", "首家", "首选", "独家", "全网首发", "全球首发", "全国首家", "世界领先", "遥遥领先", "领导者", "领袖", "王牌", "冠军", "国家级", "世界级", "宇宙级", "最高级", "绝对", "100%", "百分百", "零风险", "纯天然", "纯手工", "万能", "史无前例", "独一无二", "绝无仅有", "填补空白", "领导品牌", "著名品牌", "驰名商标", "老字号", "特供", "专供", "保证", "绝不", "杜绝", "绝无", "绝没", "绝佳", "绝顶", "坚决不"],
      "excluded_terms": ["第一步"],
      "patterns": ["(?i)(?:no[.]?1|top[.]?1|第[1①])"],
      "note": "按用户提供的保守词表拦截。存在语境误拦，不将单字命中等同于法定违法认定。"
    },
    {
      "id": "l1.p0.food_medical_claim",
      "severity": "P0",
      "category": "普通食品医疗与保健功效宣传",
      "scope": "ordinary_food",
      "condition": "always",
      "review_mode": "semantic",
      "terms": ["治疗", "治愈", "根治", "消炎", "止痛", "降压", "降血脂", "降血糖", "降三高", "抗癌", "防癌", "抗衰老", "减肥", "瘦身", "排毒", "祛湿", "养胃", "增强免疫力", "调理内分泌", "修复", "停药", "疗程", "药方", "处方", "医院推荐", "医生推荐", "专家推荐"],
      "patterns": [],
      "note": "普通食品业务采用硬拦；一般资质声明不能授权疾病预防、治疗或本表禁止的功效表达。"
    },
    {
      "id": "l1.p0.exaggerated_effect",
      "severity": "P0",
      "category": "虚假与夸大功效",
      "scope": "all",
      "condition": "always",
      "review_mode": "semantic",
      "terms": ["特效", "神效", "奇效", "速效", "全效", "强效", "永不反弹", "当天见效", "三天见效", "七天见效", "一针", "告别过敏", "逆转衰老", "起死回生", "无毒副作用", "绝对安全", "100%有效", "零风险"],
      "patterns": [],
      "note": "本词表为保守业务硬禁表，不因普通资质勾选放行。"
    },
    {
      "id": "l1.p0.unsupported_urgency",
      "severity": "P0",
      "category": "无依据限时限量与紧迫感",
      "scope": "all",
      "condition": "when_unsubstantiated",
      "review_mode": "semantic",
      "terms": ["最后一天", "最后一波", "马上涨价", "马上恢复原价", "随时结束", "仅此一次", "仅此一天", "再不抢就没了", "不会再便宜了", "错过就没机会了", "万人抢购", "万人疯抢", "抢疯了", "秒杀"],
      "patterns": [],
      "required_basis": ["匹配本商品和本场的实际活动规则", "与宣称一致的活动时间或库存依据", "与当前时间一致的有效状态"],
      "note": "没有具体依据时按P0拦截；不得用默认资质声明冒充实际库存、销量或活动状态。同一表达还命中always规则时仍拦截。"
    },
    {
      "id": "l1.p0.unsupported_data_or_endorsement",
      "severity": "P0",
      "category": "虚假数据、评价与荣誉背书",
      "scope": "all",
      "condition": "when_unsubstantiated",
      "review_mode": "semantic",
      "terms": ["科研背书", "国家级检测"],
      "patterns": ["(?:回购|销量|卖了|售出|成交)[^。！？!?\\n]{0,20}[0-9一二三四五六七八九十百千万亿两]+(?:多|余)?(?:万|亿)?(?:单|件|桶|人)", "(?:好评率|回购率)[^。！？!?\\n]{0,12}(?:[0-9]+(?:[.][0-9]+)?%|百分之[0-9一二三四五六七八九十百]+)"],
      "required_basis": ["对应宣称的真实可核验数据或背书凭证", "数据统计范围与时间", "匹配商品或经营主体"],
      "note": "不能仅凭事实包里出现同一数字认定凭证有效，也不能由模型或默认状态编造凭证。专家推荐等其它always命中项不由本条件豁免。本规则只针对销量、销售额、市场占有率、好评率、回购率等可核验背书数据；经营年限、门店数量、原料品种、工艺步骤属于客观事实陈述，不在本规则范围，只要 facts 已提供即可正常复述（依据《广告绝对化用语执法指南》第六条第(六)项）。"
    },
    {
      "id": "l1.p0.inspection_exemption_claim",
      "severity": "P0",
      "category": "免检与免抽检宣传",
      "scope": "all",
      "condition": "always",
      "review_mode": "semantic",
      "terms": ["质量免检", "免抽检"],
      "patterns": [],
      "note": "按用户提供的P0硬禁词表处理。"
    },
    {
      "id": "l1.p0.off_platform_trade",
      "severity": "P0",
      "category": "站外引流与诱导交易",
      "scope": "all",
      "condition": "always",
      "review_mode": "semantic",
      "terms": ["微信", "加V", "加微信", "扫码加好友", "转账", "私下交易", "手机号", "QQ", "淘宝", "拼多多"],
      "excluded_terms": [],
      "patterns": [],
      "note": "站外下单、购买或联系仍按P0硬拦；京东作为配送方式（京东物流/快递/配送/发货）不作为站外交易命中，故京东不列入本规则词表。后续调整须改母版及规则版本，不由生成器自行豁免。"
    },
    {
      "id": "l1.p0.competitor_disparagement",
      "severity": "P0",
      "category": "贬低同行",
      "scope": "all",
      "condition": "always",
      "review_mode": "semantic",
      "terms": ["别家都是垃圾", "别家都是假的"],
      "patterns": ["比[^。！？!?\\n]{1,20}品牌好", "[^。！？!?\\n]{1,20}不如我们"],
      "note": "XX是占位符，不当成要匹配的字面品牌名；正则长度受限，命中后保留具体原文。"
    },
    {
      "id": "l1.p0.food_selected_permanent_ban",
      "severity": "P0",
      "category": "用户指定食品永久禁用表达",
      "scope": "ordinary_food",
      "condition": "always",
      "review_mode": "hard",
      "terms": ["非转基因更安全", "食品级", "可食用", "孕妇可用", "婴儿可用"],
      "patterns": [],
      "note": "来自用户原MD末尾永久禁用清单，作为其选定的食品业务保守策略保存；不宣称这些词在全行业、所有语境均属法定禁词。"
    }
  ]
}
```

## 后续接入边界

- `review_mode=hard` 的程序硬拦必须执行，模型和用户都不能直接推翻；`review_mode=semantic` 的字面命中不直接定为 P0，须保存语义复核结果并由用户处理。语义复核仍不通过时只能修改原文或提交后台申请。
- 本次不建立 P1/P2，不创建事实资质卡、不更改其默认选项、不生成检测报告。
- 本次不修改现有 L1/L2/L3 数据库版本，也不自动发布到所有房间。测试大循环启动时将本母版快照存入 Redis，按 SHA 共享编译器；母版更新在下一场生效。结束/重启不恢复旧场次。
- 局部重写使用同一 Agent 会话和 `p0-local-rewrite.md`；正常口播通过 `p0-generation-boundary.md` 恢复纯话术格式。程序只应用指定句段 ID 的替换并重新检查全文，坏 JSON 也消耗重写次数。
- 后台数据库 `live_p0_policy_versions` 保留公用规则快照，`live_p0_audit_events` 保留请求、原始返回、组装稿、命中位置、决定和扫描耗时。失败未落档时不采纳；按租户/方案查询 `GET /api/v1/live-agent-plans/{planID}/anchor-style-workbench/p0-audit?run_id=...&limit=50`。前台不增加审核统计面板。
- 审核记录不是运行进度，不能从其恢复场次。本场跳过组合不立即重选，连续跳过默认5次或轮盘耗尽时暂停；上限在本MD中维护。
- 正式房间的其它生成入口、L2行业库及向量近义补查仍未接入，不能把测试大循环覆盖等同于全系统覆盖。
