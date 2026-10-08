# 通用主播风格评分策略母版

版本：1.0.0。用途：初始化每个方案的风格评分规则，不绑定主播、商品、行业、语言模型或房间。

**这是一份由开发侧制作、供程序读取的默认规则母版，不是让大模型估算权重的提示词。** 本版先落文件，尚未接入现有评分器；不能仅凭修改本文件就声称运行评分已经改变。

## 一、母版与方案配置的边界

- 本文件只维护通用默认值，不存任何主播的具体词语、商品事实、房间ID、租户ID或方案ID。
- 首次提取主播话术时，模型提取词语候选、功能、别名、句式和情绪等定性信息；程序核对实际次数、密度、频次主次及适用项，再将本母版与主播基准组合为方案评分配置。
- 方案评分配置保存到数据库，按租户、方案、版本隔离，房间关联读取。运行读取数据库生效版本，不把本文件当所有房间的共享可变配置。
- 未设置权重的方案自动采用默认规则，不要求用户先填权重或额外确认权重才能运行。
- 用户可以在方案的高级设置调整权重、容差和辅助项扣分上限，保存新版本或恢复默认；不得修改本文件来代替修改单个方案。
- 母版升级不自动覆盖已有方案。重新分析样本只更新词语映射和实测基准，不覆盖用户已有的权重覆盖项；新出现特征按默认规则初始化，删除特征的旧设置保留迁移记录。
- 本场启动冻结配置快照与版本。大循环的临时快照、进度、记忆仍存Redis，结束清理，服务器重启重新开始；稳定方案配置仍存数据库。

## 二、默认配置 JSON

以下数字是确定性产品默认规则，不是模型输出或样本估数。调整时必须更新版本并经过程序校验。

```json
{
  "schema": "style-scoring-policy/v1",
  "policy_id": "generic-anchor-style-default",
  "version": "1.0.0",
  "scope": "template",
  "score_scale": 100,
  "adoption": {
    "threshold": 40,
    "comparison": "greater_than_or_equal",
    "rewrite_only_below_threshold": true,
    "extra_feature_veto": false,
    "minor_issue_forces_rewrite": false,
    "retroactive_rewrite": false
  },
  "tiers": {
    "primary": {
      "weight_points": 70,
      "within_tier_weights": {
        "main_particle": 25,
        "main_audience_address": 25,
        "self_identity": 20
      }
    },
    "secondary": {
      "weight_points": 20,
      "within_tier_weights": {
        "auxiliary_particles": 6,
        "auxiliary_addresses": 4,
        "dialect_catchphrases": 6,
        "number_expression": 4
      }
    },
    "detail": {
      "weight_points": 10,
      "within_tier_weights": {
        "clause_rhythm": 4,
        "recurrence_rhythm": 4,
        "punctuation_proxy": 2
      }
    }
  },
  "secondary_penalty_cap": {
    "relative_to": "active_primary_weight_budget",
    "numerator": 1,
    "denominator": 3,
    "apply_once": true
  },
  "applicability": {
    "feature_terms_source": "current_plan_calibrated_baseline",
    "missing_sample_feature": "not_applicable",
    "zero_sample_rate": "no_missing_frequency_penalty",
    "task_irrelevant_feature": "not_applicable",
    "within_tier_redistribution": "proportional_to_active_feature_weights",
    "empty_tier_redistribution": "proportional_to_remaining_active_tier_budgets",
    "primary_fallback_order": [
      "clause_rhythm",
      "dialect_catchphrases",
      "recurrence_rhythm",
      "number_expression"
    ],
    "primary_fallback_weight_points": 70,
    "no_measurable_features": "observation_only_no_numeric_score"
  },
  "measurement": {
    "counting_method": "unicode_codepoints_including_punctuation_excluding_whitespace",
    "alias_matching": "confirmed_alias_groups_longest_non_overlapping",
    "ambiguous_token": "require_context_rule_or_exclude_from_density",
    "deduplicate_cross_feature_counts": true,
    "punctuation_is_voice_emotion": false,
    "program_added_punctuation_in_score": false,
    "program_added_punctuation_in_style_window": false
  },
  "segment": {
    "density_mode": "observe_not_exact_quota",
    "primary_ratio_mode": "observe_not_exact_quota",
    "min_expected_count_for_missing_presence_penalty": 2,
    "max_missing_presence_loss_fraction": 0.5,
    "punctuation_missing_loss_fraction": 0,
    "secondary_missing_loss_fraction": 0,
    "score_window_feedback_as_segment_penalty": false
  },
  "accepted_window": {
    "characters": 1000,
    "update_after_adoption_only": true,
    "exclude_rejected_candidates": true,
    "retain_across_cycles": true,
    "partial_window_density_action": "observation_only",
    "min_expected_count_for_density_issue": 2,
    "primary_rate_band": {"lower_multiplier": 0.8, "upper_multiplier": 1.3},
    "secondary_rate_band": {"lower_multiplier": 0, "upper_multiplier": 2},
    "detail_rate_action": "observe_without_frequency_rewrite"
  },
  "dominance": {
    "families": ["particles", "audience_addresses"],
    "main_source": "program_verified_sample_frequency_ranking",
    "comparison": "each_auxiliary_vs_main_not_sum_of_auxiliaries",
    "allow_equal_counts": true,
    "full_window_required": true,
    "min_expected_main_count": 3,
    "consecutive_full_window_evaluations": 2,
    "min_new_accepted_characters_between_evaluations": 200,
    "action": "light_feedback_to_future_segment",
    "rewrite_accepted_text": false
  },
  "feedback": {
    "selection_order": ["primary", "secondary", "detail"],
    "secondary_only_issue_action": "observe_or_light_feedback_no_forced_rewrite",
    "no_density_debt_repayment": true,
    "no_fixed_next_segment_token_quota": true,
    "helper_may_change_weights": false,
    "address_source": "current_plan_confirmed_style_addresses"
  }
}
```

## 三、特征映射：不用固定主播词表

| 特征ID | 默认层级 | 取值来源与适用范围 |
| --- | --- | --- |
| main_particle | 主要 | 当前主播有效语气词中，程序实测确定的主力词或并列主力组；没有就不启用 |
| main_audience_address | 主要 | 当前主播主力称呼组及确认别名；允许放在句中、句尾，不要求固定开头 |
| self_identity | 主要 | 当前样本支持的主播自指体系，按功能和确认计数规则区分身份自称与普通叙述人称 |
| auxiliary_particles | 次要 | 当前主播其余有效语气词；可短段缺失，不要求每段塞齐 |
| auxiliary_addresses | 次要 | 当前主播辅助称呼组；可根据话题轮换，不强制单段全出现 |
| dialect_catchphrases | 次要 | 当前样本提供的方言与口头禅，不套用任何地域词库 |
| number_expression | 次要 | 数字的口语表达、换算和算账习惯；只在样本支持且当前任务相关时适用，不核验商品事实 |
| clause_rhythm | 细节 | 当前样本实测的分句长短和停顿结构，不固定某个句长 |
| recurrence_rhythm | 细节 | 当前样本的确认、回环及衔接方式，不把称呼的同一次出现再计作回环，也不要求逐字复读 |
| punctuation_proxy | 细节 | 标点节奏的弱代理，不能代表声音情绪；程序补的标点不抬分 |

频次主次与辨识度不是一回事。模型可以描述词语功能、辨识度及依据，不能填权重数字；同类词的频次排序由程序核对。不得从“样本没出现”直接推导“绝对禁用”，强禁用必须有明确规则或可靠依据。

不同主播可能没有明显语气词、称呼或身份自称。此时不得强造这些特征：按配置中的固定回退顺序，从有实测支持的其他特征中选择一项作为主要特征，移出原层级，主要层级预算仍为70。回退依据和角色迁移必须记录，不能将同一项在两个层级重复计分。标点不能独自升级成核心风格。如果所有特征都无法可靠测量，记录观察态及原因，不伪造0分或100分。

## 四、评分的确定性计算

1. **判断适用项。** 样本没有的特征、没有可靠计数规则的特征，以及本轮任务不相关的数字表达不参与计分；不把“不适用”记成零分。
2. **分配预算。** 三个层级默认70／20／10。层级内按有效特征的相对权重分配该层级预算；整个层级不可用时，其预算按剩余有效层级比例分配。记录最终有效权重，不静默丢掉分母。
3. **计算损失比例。** 每项规则产生0到1的损失比例。单段不执行精准密度或主辅配比配额；当前应出现的主特征确实缺失，且样本密度推算的期望次数至少为2时，默认缺失损失为0.5。明确错误须有基准、上下文规则和可定位证据，不因单段少一个词就判严重漂移。
4. **计算扣分。** 单项扣分＝该项有效权重×损失比例，不超过该项权重。同一证据不得在多个维度重复扣分；尤其不得将普通否定字算作语气词，或将称呼再算作回环。
5. **限制辅助扣分。** 辅助层级累计扣分不超过有效主要层级权重预算的1／3。这里参照的是主要层级的预算，不是主要项本段实际扣分。默认辅助总权重20已经较低，不能再无条件乘1／3，避免重复降权。
6. **合成总分。** 加权总分＝100－主要扣分－封顶后的辅助扣分－细节扣分。内部保留精度，最终分数按四舍五入显示为整数，范围0到100。同稿、同基准、同策略版本、同任务上下文和适用项应得到同分。
7. **决定采纳。** 加权总分≥40即采纳，低于40才进入当前段重写；沿用场次已有的重试上限及选优逻辑。不新增“某个小指标没过就必须重写”的隐藏门槛。空稿、网络错误或统计失败是运行错误，不冒充风格低分。

1／3、70／20／10和各容差都是初始默认档，不是“某个主播的事实”。用户不设置时自动采用；高级设置允许调整，但必须校验总预算、非负数、分母、范围及适用项映射，并记录新版本。

## 五、单段与1000字窗口的职责

### 当前候选段

- 评分关注明确的表达漂移、当前适用的主要特征及基本口播节奏，不要求每个特征都在每一段精确出现。
- 次要词偶尔缺失默认不扣缺失分；单段出现多一点可以记录，不因主辅频次偶然反转强制重写。
- 无问号、无感叹号默认不扣缺失分。可以补齐明确句末标点，但评分和风格统计继续使用模型原稿。
- 单段已满足40分时照常采纳，不把此前窗口的欠缺转成当前段的额外扣分，也不撤回旧稿。

### 采纳后的连续窗口

- 只统计已采纳版本的模型原稿。取最近1000个Unicode字符，含原始标点、不含空白；跨直播小循环保留，不把重写拒绝稿或程序补的标点混进去。
- 不足1000字时密度与主辅占比只观察。窗口完整且期望次数足够后，主要特征默认参考样本的80%–130%，辅助默认允许0–200%，细节不设置强制频次配额。
- 原有的近期开头／结尾避重仍可独立检测；允许主力称呼高频出现在句中、句尾，禁止把加强主力理解成固定僵尸开头。
- 主辅反超只比较同类特征中的每个辅助与主力，不把不同单位的特征相互比较，也不要求主力大于所有辅助之和。主辅并列允许；样本的并列主力按程序核对规则处理，不能凭词典序制造谁必须压过谁。
- 完整窗口中，某辅助持续超过主力，且主力期望至少3次，达到2次有效评估才触发轻量回灌。两次评估之间至少新增200个采纳字符，不能把几乎相同的窗口当多次独立证据。
- 回灌只指导后续自然微调，不补偿历史缺口、不规定下一段必须补多少次、不为了压低某个字破坏语义。不得重写已采纳稿。

## 六、回灌与通用性约束

- 程序传入已触发问题、层级、权重、实测值、容差及依据；辅助模型只组织回灌文案，不重新打分、数数、确定权重或扩充禁用词。
- 优先纠正主要问题，次要问题轻推，细节问题通常观察。辅助特征反超主力时，提示恢复主力的自然存在感，不简单禁用辅助特征。
- 不把高亢当所有主播的默认情绪，不强制所有主播用方言、亲属称呼、某个自指、某种语气词、算账或逼单。
- 不检查商品事实，不补商品卖点，不给任何特定品类单独提高权重；本文件只管语言风格。
- 数字表达只评说法习惯，不要求编出缺失的金额、数量或承诺。主播样本里的商品信息不能作为其他商品的事实来源。

## 七、方案保存与评分明细

数据库方案配置至少保存：来源母版ID与版本、关联主播基准版本、有效特征与词语映射、默认／用户覆盖来源、层级与单项有效权重、容差、辅助扣分上限、采纳阈值、生效版本及变更记录。模型原始提取返回独立保留，不能覆盖。

评分明细至少显示：特征ID、层级、是否适用及原因、实测值、参考目标或容差、有效权重、损失比例、封顶前后扣分、最终分数、原稿／补标点输出的区分、基准与策略版本。回退和不支持的指标也要显示原因。

## 八、后续接入与验收

本次只建立母版，**不自动导入已有数据库方案，不修改当前运行场次、不改变现有打分器。** 后续接入需同时完成读取校验、方案数据库版本化、评分器执行、界面调整入口及场次快照；不得只把这份文件喂给模型就当作权重功能完成。

接入验收：

1. 不设置权重也能初始化方案；文件中没有固定主播词或商品信息。
2. 同稿在同基准、策略、任务上下文下重复评分一致；不调用模型估权重或计数。
3. 次要项或标点小偏差不单独导致重写，40分门槛无隐藏否决项。
4. 主辅反超的单段波动被允许，完整窗口持续偏离才指导后续，已采纳稿不撤回。
5. 无语气词／无称呼／无数字习惯的主播不会被强迫套入示例主播的特征；适用项回退和重分配可追踪。
6. 方案甲调整权重不影响方案乙；母版修改不覆盖甲乙的数据库配置；用户覆盖项不因重新分析被重置。
7. 运行场次仍使用冻结版本，下一场或明确升级后才采用新配置；不会因辅助模型建议改变评分标尺。
