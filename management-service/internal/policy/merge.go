package policy

import (
	"fmt"
	"sort"
	"strings"

	"livecompanion/management/internal/model"
)

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case model.LivePolicyModeVerbatim:
		return model.LivePolicyModeVerbatim
	default:
		return model.LivePolicyModeIntent
	}
}

func normalizeRule(rule model.LivePolicyRule) model.LivePolicyRule {
	rule.Key = strings.TrimSpace(rule.Key)
	rule.Title = strings.TrimSpace(rule.Title)
	rule.Text = strings.TrimSpace(rule.Text)
	rule.ExecutionMode = normalizeMode(rule.ExecutionMode)
	rule.FixedText = strings.TrimSpace(rule.FixedText)
	if rule.ExecutionMode == model.LivePolicyModeVerbatim && rule.FixedText == "" {
		rule.FixedText = rule.Text
	}
	if rule.Text == "" && rule.FixedText != "" {
		rule.Text = rule.FixedText
	}
	if !rule.Enabled {
		rule.Enabled = rule.Text != "" || rule.FixedText != ""
	}
	return rule
}

func normalizeOverride(item model.LivePolicyOverride) model.LivePolicyOverride {
	item.Key = strings.TrimSpace(item.Key)
	item.Operation = strings.ToLower(strings.TrimSpace(item.Operation))
	item.Title = strings.TrimSpace(item.Title)
	item.Text = strings.TrimSpace(item.Text)
	item.ExecutionMode = normalizeMode(item.ExecutionMode)
	item.FixedText = strings.TrimSpace(item.FixedText)
	if item.ExecutionMode == model.LivePolicyModeVerbatim && item.FixedText == "" {
		item.FixedText = item.Text
	}
	if item.Text == "" && item.FixedText != "" {
		item.Text = item.FixedText
	}
	return item
}

func ValidateDraft(
	layer string,
	rules []model.LivePolicyRule,
	overrides []model.LivePolicyOverride,
	l1 *model.LivePolicyVersion,
) []model.LivePolicyConflict {
	layer = strings.ToUpper(strings.TrimSpace(layer))
	conflicts := make([]model.LivePolicyConflict, 0)
	seen := map[string]struct{}{}

	if layer == model.LivePolicyLayerL1 || layer == model.LivePolicyLayerL2 {
		if len(overrides) > 0 {
			conflicts = append(conflicts, model.LivePolicyConflict{
				Code:    "invalid_override_layer",
				Message: layer + " 只保存完整规则，不能保存 L3 覆盖指令",
			})
		}
		l1Locked := map[string]struct{}{}
		if layer == model.LivePolicyLayerL2 && l1 != nil {
			for _, raw := range l1.Rules {
				rule := normalizeRule(raw)
				if rule.Key != "" && rule.Enabled {
					l1Locked[rule.Key] = struct{}{}
				}
			}
		}
		for _, raw := range rules {
			rule := normalizeRule(raw)
			if rule.Key == "" {
				conflicts = append(conflicts, model.LivePolicyConflict{Code: "missing_rule_key", Message: "规则缺少稳定 key"})
				continue
			}
			if _, ok := seen[rule.Key]; ok {
				conflicts = append(conflicts, model.LivePolicyConflict{Code: "duplicate_rule_key", Key: rule.Key, Message: "同一层存在重复规则 key"})
				continue
			}
			seen[rule.Key] = struct{}{}
			if _, locked := l1Locked[rule.Key]; locked {
				conflicts = append(conflicts, model.LivePolicyConflict{
					Code:    "l1_locked",
					Key:     rule.Key,
					Message: "L2 使用了 L1 强制规则的 key，不能覆盖 L1",
				})
			}
			if rule.Text == "" {
				conflicts = append(conflicts, model.LivePolicyConflict{Code: "empty_rule", Key: rule.Key, Message: "规则内容不能为空"})
			}
			if rule.ExecutionMode == model.LivePolicyModeVerbatim && rule.FixedText == "" {
				conflicts = append(conflicts, model.LivePolicyConflict{Code: "verbatim_missing_text", Key: rule.Key, Message: "固定原话规则必须保存 fixed_text"})
			}
		}
		return conflicts
	}

	if layer != model.LivePolicyLayerL3 {
		return []model.LivePolicyConflict{{Code: "invalid_layer", Message: "策略层必须是 L1、L2 或 L3"}}
	}
	if len(rules) > 0 {
		conflicts = append(conflicts, model.LivePolicyConflict{
			Code:    "invalid_l3_rules",
			Message: "L3 只记录与行业默认不同的覆盖项，不复制整份 L2",
		})
	}

	locked := map[string]struct{}{}
	if l1 != nil {
		for _, raw := range l1.Rules {
			rule := normalizeRule(raw)
			if rule.Key != "" && rule.Enabled {
				locked[rule.Key] = struct{}{}
			}
		}
	}

	for _, raw := range overrides {
		item := normalizeOverride(raw)
		if item.Key == "" {
			conflicts = append(conflicts, model.LivePolicyConflict{Code: "missing_override_key", Message: "L3 覆盖项缺少 key"})
			continue
		}
		if _, exists := seen[item.Key]; exists {
			conflicts = append(conflicts, model.LivePolicyConflict{
				Code:    "duplicate_override_key",
				Key:     item.Key,
				Message: "同一层存在重复的 L3 覆盖 key",
			})
			continue
		}
		seen[item.Key] = struct{}{}
		switch item.Operation {
		case model.LivePolicyOverrideAdd, model.LivePolicyOverrideReplace, model.LivePolicyOverrideDisable:
		default:
			conflicts = append(conflicts, model.LivePolicyConflict{
				Code:    "invalid_override_operation",
				Key:     item.Key,
				Message: "L3 operation 只能是 add、replace 或 disable",
			})
			continue
		}
		if _, ok := locked[item.Key]; ok {
			conflicts = append(conflicts, model.LivePolicyConflict{
				Code:    "l1_locked",
				Key:     item.Key,
				Message: "该规则属于 L1 强制边界，L3 不能使用同一 key 新增、替换或关闭",
			})
		}
		if item.Operation != model.LivePolicyOverrideDisable && item.Text == "" {
			conflicts = append(conflicts, model.LivePolicyConflict{
				Code:    "empty_override",
				Key:     item.Key,
				Message: "新增或替换规则必须有内容",
			})
		}
		if item.Operation != model.LivePolicyOverrideDisable &&
			item.ExecutionMode == model.LivePolicyModeVerbatim &&
			item.FixedText == "" {
			conflicts = append(conflicts, model.LivePolicyConflict{
				Code:    "verbatim_missing_text",
				Key:     item.Key,
				Message: "固定原话覆盖必须保存 fixed_text",
			})
		}
	}
	return conflicts
}

func effectiveRule(rule model.LivePolicyRule, layer string, versionID int64) model.LiveEffectivePolicyRule {
	rule = normalizeRule(rule)
	return model.LiveEffectivePolicyRule{
		Key:             rule.Key,
		Title:           rule.Title,
		Text:            rule.Text,
		ExecutionMode:   rule.ExecutionMode,
		FixedText:       rule.FixedText,
		SourceLayer:     layer,
		SourceVersionID: versionID,
		Metadata:        rule.Metadata,
	}
}

func BuildEffective(industryCode string, l1, l2, l3 *model.LivePolicyVersion) model.LiveEffectivePolicy {
	result := model.LiveEffectivePolicy{
		IndustryCode: strings.TrimSpace(industryCode),
		L1:           l1,
		L2:           l2,
		L3:           l3,
		Rules:        make([]model.LiveEffectivePolicyRule, 0),
		Conflicts:    make([]model.LivePolicyConflict, 0),
	}
	if result.IndustryCode == "" {
		result.IndustryCode = "general"
	}

	locked := map[string]struct{}{}
	if l1 != nil {
		for _, raw := range l1.Rules {
			rule := normalizeRule(raw)
			if rule.Key == "" || !rule.Enabled {
				continue
			}
			locked[rule.Key] = struct{}{}
			result.Rules = append(result.Rules, effectiveRule(rule, model.LivePolicyLayerL1, l1.ID))
		}
	}

	business := map[string]model.LiveEffectivePolicyRule{}
	order := make([]string, 0)
	if l2 != nil {
		for _, raw := range l2.Rules {
			rule := normalizeRule(raw)
			if rule.Key == "" || !rule.Enabled {
				continue
			}
			if _, lockedByL1 := locked[rule.Key]; lockedByL1 {
				result.Conflicts = append(result.Conflicts, model.LivePolicyConflict{
					Code:    "l2_shadowed_by_l1",
					Key:     rule.Key,
					Message: "L2 使用了与 L1 相同的 key，L1 强制规则优先",
				})
				continue
			}
			if _, exists := business[rule.Key]; !exists {
				order = append(order, rule.Key)
			}
			business[rule.Key] = effectiveRule(rule, model.LivePolicyLayerL2, l2.ID)
		}
	}

	if l3 != nil {
		for _, raw := range l3.Overrides {
			item := normalizeOverride(raw)
			if item.Key == "" {
				continue
			}
			if _, isLocked := locked[item.Key]; isLocked {
				result.Conflicts = append(result.Conflicts, model.LivePolicyConflict{
					Code:    "l1_locked",
					Key:     item.Key,
					Message: "L3 尝试复用或修改 L1 强制规则 key，已忽略该覆盖",
				})
				continue
			}
			switch item.Operation {
			case model.LivePolicyOverrideDisable:
				delete(business, item.Key)
			case model.LivePolicyOverrideReplace:
				if _, exists := business[item.Key]; !exists {
					order = append(order, item.Key)
				}
				business[item.Key] = model.LiveEffectivePolicyRule{
					Key: item.Key, Title: item.Title, Text: item.Text,
					ExecutionMode: item.ExecutionMode, FixedText: item.FixedText,
					SourceLayer: model.LivePolicyLayerL3, SourceVersionID: l3.ID,
					Metadata: item.Metadata,
				}
			case model.LivePolicyOverrideAdd:
				key := item.Key
				if _, exists := business[key]; exists {
					key = uniqueL3Key(business, key)
				}
				order = append(order, key)
				business[key] = model.LiveEffectivePolicyRule{
					Key: key, Title: item.Title, Text: item.Text,
					ExecutionMode: item.ExecutionMode, FixedText: item.FixedText,
					SourceLayer: model.LivePolicyLayerL3, SourceVersionID: l3.ID,
					Metadata: item.Metadata,
				}
			default:
				result.Conflicts = append(result.Conflicts, model.LivePolicyConflict{
					Code: "invalid_override_operation", Key: item.Key, Message: "未知 L3 覆盖操作，已忽略",
				})
			}
		}
	}

	for _, key := range order {
		if rule, ok := business[key]; ok {
			if strings.TrimSpace(rule.ExecutionMode) == "" {
				rule.ExecutionMode = model.LivePolicyModeIntent
			}
			result.Rules = append(result.Rules, rule)
		}
	}
	result.PromptText = RenderPrompt(result)
	return result
}

func uniqueL3Key(existing map[string]model.LiveEffectivePolicyRule, base string) string {
	for i := 2; ; i++ {
		key := fmt.Sprintf("%s#l3-%d", base, i)
		if _, ok := existing[key]; !ok {
			return key
		}
	}
}

func RenderPrompt(p model.LiveEffectivePolicy) string {
	lines := []string{
		"【三层策略执行原则】",
		"1. L1 是通用判断与表达方法：先理解对方真实意图，再核对事实与约束，最后生成自然、热情、好听、可直接播出且不违规的表达。L1 不等于禁止清单。",
		"2. L2 是行业表达层：在 L1 方法上加入行业专业知识、常见问法、销售节奏、行业边界和表达习惯。",
		"3. L3 是直播间个性层：在 L1+L2 上加入当前商品、活动、主播风格、口头习惯、直播节奏和客户策略。",
		"4. L2/L3 可以让表达更贴合场景，但不能改变 L1 的事实判断、真实性和最终表达方法。",
		"5. 对方的原话不适合直接说时，不要把内部判断或审核口吻念给观众；应理解其目的，转换成一条同样能推进交流或销售、但更自然合适的直播表达。",
		"6. 能直接回答就积极热情地回答；信息不足时先承接，再说明以实时信息为准，并继续给出当前能确认的内容或下一步。",
		"7. 除确实没有任何可用表达的极端情况外，最终直播话术避免使用“拒绝”“不能回答”“违规”“系统不允许”等审核式措辞。",
		"8. execution_mode=verbatim 时在不与 L1 判断冲突的前提下逐字使用 fixed_text；execution_mode=intent 时保留意思、事实和约束，并允许按 L1/L2/L3 自然改写。",
		"",
		"【当前有效规则】",
	}
	for _, rule := range p.Rules {
		mode := rule.ExecutionMode
		if mode == "" {
			mode = model.LivePolicyModeIntent
		}
		content := rule.Text
		if mode == model.LivePolicyModeVerbatim && rule.FixedText != "" {
			content = rule.FixedText
		}
		lines = append(lines, fmt.Sprintf("- [%s][%s][%s] %s", rule.SourceLayer, rule.Key, mode, content))
	}
	if len(p.Conflicts) > 0 {
		lines = append(lines, "", "【配置冲突提示】")
		conflicts := append([]model.LivePolicyConflict(nil), p.Conflicts...)
		sort.SliceStable(conflicts, func(i, j int) bool { return conflicts[i].Key < conflicts[j].Key })
		for _, conflict := range conflicts {
			lines = append(lines, "- "+conflict.Message)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
