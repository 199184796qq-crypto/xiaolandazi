// Package styleoverlay compiles user-authored persona additions into bounded,
// provider-neutral delivery rules. It never owns product facts or policy.
package styleoverlay

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/stylecontract"
)

const MaxItems = 12

var allowedCategories = map[string]bool{
	"humor": true, "tone": true, "rhythm": true, "structure": true,
	"lexical": true, "storytelling": true, "interaction_delivery": true,
	"delivery_other": true, "strategy_numeric_comparison": true,
	"strategy_fact_recurrence": true,
}

var allowedApplications = map[string]bool{
	"always": true, "occasional": true, "conditional": true,
}

var allowedMicroActions = map[string]bool{
	"audience_address": true, "self_reference": true, "state_information": true,
	"direct_answer": true, "short_confirmation": true, "rephrase": true,
	"supplement": true, "bridge": true, "question": true, "scene_detail": true,
	"reaction": true, "conclusion": true, "reason": true, "example": true,
	"self_correction": true, "close": true,
}

func trim(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

func NewID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func NormalizeRule(input model.LiveAnchorStyleOverlayRule) (model.LiveAnchorStyleOverlayRule, error) {
	input.Version = strings.TrimSpace(input.Version)
	if input.Version != model.LiveAnchorStyleOverlayVersion {
		return model.LiveAnchorStyleOverlayRule{}, errors.New("叠加风格协议版本无效")
	}
	input.Category = strings.ToLower(strings.TrimSpace(input.Category))
	if !allowedCategories[input.Category] {
		return model.LiveAnchorStyleOverlayRule{}, errors.New("叠加风格类别无效")
	}
	input.Application = strings.ToLower(strings.TrimSpace(input.Application))
	if !allowedApplications[input.Application] {
		return model.LiveAnchorStyleOverlayRule{}, errors.New("叠加风格应用方式无效")
	}
	input.Label = trim(input.Label, 40)
	input.MainlineInstruction = trim(input.MainlineInstruction, 240)
	input.InteractionInstruction = trim(input.InteractionInstruction, 240)
	input.SeriousInstruction = trim(input.SeriousInstruction, 240)
	if input.Label == "" || input.MainlineInstruction == "" || input.InteractionInstruction == "" || input.SeriousInstruction == "" {
		return model.LiveAnchorStyleOverlayRule{}, errors.New("叠加风格缺少可执行场景规则")
	}
	if input.Strength < 0 || input.Strength > 100 || input.Confidence < 0 || input.Confidence > 100 {
		return model.LiveAnchorStyleOverlayRule{}, errors.New("叠加风格强度或理解置信度无效")
	}
	if input.MainlineMinPer1000Chars < 0 || input.MainlineMaxPer1000Chars < input.MainlineMinPer1000Chars || input.MainlineMaxPer1000Chars > 20 {
		return model.LiveAnchorStyleOverlayRule{}, errors.New("叠加风格长文频率无效")
	}
	if input.InteractionMaxOccurrences < 0 || input.InteractionMaxOccurrences > 3 {
		return model.LiveAnchorStyleOverlayRule{}, errors.New("叠加风格短答频率无效")
	}
	if input.Application == "always" {
		input.MainlineMinPer1000Chars = 0
		input.MainlineMaxPer1000Chars = 0
		input.InteractionMaxOccurrences = 0
	}
	microActions := make([]string, 0, len(input.MicroActions))
	for _, action := range input.MicroActions {
		action = strings.ToLower(strings.TrimSpace(action))
		if !allowedMicroActions[action] {
			return model.LiveAnchorStyleOverlayRule{}, fmt.Errorf("叠加风格局部动作无效：%s", action)
		}
		if len(microActions) > 0 && microActions[len(microActions)-1] == action {
			continue
		}
		microActions = append(microActions, action)
		if len(microActions) == 8 {
			break
		}
	}
	input.MicroActions = microActions
	seen := map[string]bool{}
	avoid := make([]string, 0, len(input.Avoid))
	for _, value := range input.Avoid {
		value = trim(value, 100)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		avoid = append(avoid, value)
		if len(avoid) == 8 {
			break
		}
	}
	input.Avoid = avoid
	// Serious-scene labels describe where an expressive behavior is disabled;
	// mentioning a complaint or after-sales context is not itself a business
	// claim. Remove only these fixed context labels before the purity scan.
	seriousForPurity := strings.NewReplacer("投诉", "", "售后", "", "事实澄清", "", "事实核对", "").Replace(input.SeriousInstruction)
	phrases := []string{input.Label, input.MainlineInstruction, input.InteractionInstruction, seriousForPurity}
	phrases = append(phrases, input.Avoid...)
	if input.Category == "strategy_numeric_comparison" {
		// This strategy is allowed to name the operation, never concrete values.
		// Strip only meta-language before the normal fact/policy purity scan.
		for index := range phrases {
			phrases[index] = strings.NewReplacer("数字比价", "表达动作", "比价", "表达动作", "连续算账", "表达动作", "算账", "表达动作", "价格", "当前正式数值", "单价", "当前正式数值", "组合价", "当前正式数值").Replace(phrases[index])
		}
	}
	purity := stylecontract.AssessPurity(model.LiveAgentPlanAnchorStyleProfile{ReusableRules: phrases})
	if !purity.Passed {
		return model.LiveAnchorStyleOverlayRule{}, fmt.Errorf("叠加风格混入商品事实或内容策略：%s", purity.Issues[0].Reason)
	}
	return input, nil
}

func NormalizeItems(items []model.LiveAnchorStyleOverlayItem) ([]model.LiveAnchorStyleOverlayItem, error) {
	if len(items) > MaxItems {
		return nil, fmt.Errorf("叠加风格最多%d条", MaxItems)
	}
	result := make([]model.LiveAnchorStyleOverlayItem, 0, len(items))
	seen := map[string]bool{}
	for index, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" || len(item.ID) > 64 || seen[item.ID] {
			return nil, fmt.Errorf("第%d条叠加风格标识无效", index+1)
		}
		seen[item.ID] = true
		item.SourceText = strings.TrimSpace(item.SourceText)
		item.ExplanationText = strings.TrimSpace(item.ExplanationText)
		if item.SourceText == "" || utf8.RuneCountInString(item.SourceText) > 300 || utf8.RuneCountInString(item.ExplanationText) > 600 {
			return nil, fmt.Errorf("第%d条叠加风格原话为空或过长", index+1)
		}
		var err error
		item.Rule, err = NormalizeRule(item.Rule)
		if err != nil {
			return nil, fmt.Errorf("第%d条叠加风格：%w", index+1, err)
		}
		item.InterpretationSource = trim(item.InterpretationSource, 80)
		item.LearningBasis = strings.ToLower(trim(item.LearningBasis, 32))
		switch item.LearningBasis {
		case "", "sample_evidence", "human_feedback", "plugin_manifest":
		default:
			return nil, fmt.Errorf("第%d条叠加风格学习依据无效", index+1)
		}
		evidenceSeen := map[string]bool{}
		evidence := make([]string, 0, len(item.EvidenceQuotes))
		for _, quote := range item.EvidenceQuotes {
			quote = trim(quote, 120)
			if quote == "" || evidenceSeen[quote] {
				continue
			}
			evidenceSeen[quote] = true
			evidence = append(evidence, quote)
			if len(evidence) == 6 {
				break
			}
		}
		if item.LearningBasis == "sample_evidence" && len(evidence) == 0 {
			return nil, fmt.Errorf("第%d条叠加风格缺少样本证据", index+1)
		}
		item.EvidenceQuotes = evidence
		result = append(result, item)
	}
	return result, nil
}

// Render emits only code-validated rules. The original user sentence and
// retrieved vector examples are deliberately absent from the generation prompt.
func Render(profile model.LiveAgentPlanStyleOverlayProfile) string {
	var lines []string
	for _, item := range profile.Items {
		if !item.Enabled {
			continue
		}
		rule, err := NormalizeRule(item.Rule)
		if err != nil {
			continue
		}
		line := fmt.Sprintf("- %s（强度%d/100，%s）：主线%s；短互动%s；严肃场景%s", rule.Label, rule.Strength, applicationLabel(rule.Application), rule.MainlineInstruction, rule.InteractionInstruction, rule.SeriousInstruction)
		if rule.Application != "always" && rule.MainlineMaxPer1000Chars > 0 {
			line += fmt.Sprintf("；长文每1000字%d到%d次，短答最多%d次", rule.MainlineMinPer1000Chars, rule.MainlineMaxPer1000Chars, rule.InteractionMaxOccurrences)
		}
		if len(rule.MicroActions) > 0 {
			actions := make([]string, 0, len(rule.MicroActions))
			for _, action := range rule.MicroActions {
				actions = append(actions, microActionLabel(action))
			}
			line += "；局部推进=" + strings.Join(actions, "→")
		}
		if len(rule.Avoid) > 0 {
			line += "；避免：" + strings.Join(rule.Avoid, "、")
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "【方案级风格与策略外挂】\n以下规则叠加在稳定主播风格之上。表达规则不新增事实；数字比价与事实回环策略也只能使用当前授权事实，不得覆盖正式事实及L1/L2。\n" + strings.Join(lines, "\n")
}

func applicationLabel(value string) string {
	switch value {
	case "always":
		return "持续习惯"
	case "occasional":
		return "偶尔出现"
	default:
		return "按场景出现"
	}
}

func microActionLabel(value string) string {
	labels := map[string]string{
		"audience_address": "称呼观众", "self_reference": "主播方自指", "state_information": "陈述信息",
		"direct_answer": "直接回答", "short_confirmation": "短确认", "rephrase": "换句话说明",
		"supplement": "补充一句", "bridge": "自然转场", "question": "提出问题",
		"scene_detail": "场景细节", "reaction": "自然反应", "conclusion": "先给结论",
		"reason": "说明原因", "example": "简短举例", "self_correction": "回头修正", "close": "自然收束",
	}
	if label := labels[value]; label != "" {
		return label
	}
	return value
}

func MemoryText(item model.LiveAnchorStyleOverlayItem) string {
	rule, err := NormalizeRule(item.Rule)
	if err != nil {
		return ""
	}
	source := safeMemoryPhrase(item.SourceText)
	explanation := safeMemoryPhrase(item.ExplanationText)
	return fmt.Sprintf("用户表达：%s\n用户解释：%s\n已确认理解：类别=%s；标签=%s；应用=%s；主线=%s；短答=%s；严肃=%s；局部推进=%s；每1000字=%d-%d次；短答最多=%d次；避免=%s",
		source, explanation, rule.Category, rule.Label, rule.Application,
		rule.MainlineInstruction, rule.InteractionInstruction, rule.SeriousInstruction, strings.Join(rule.MicroActions, "→"),
		rule.MainlineMinPer1000Chars, rule.MainlineMaxPer1000Chars, rule.InteractionMaxOccurrences, strings.Join(rule.Avoid, "、"))
}

func safeMemoryPhrase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Scene labels are useful to learn phrases such as “售后时别开玩笑”, but
	// product/transaction content must never become reusable semantic memory.
	probe := strings.NewReplacer("投诉", "严肃场景", "售后", "严肃场景", "事实澄清", "严肃场景", "事实核对", "严肃场景").Replace(value)
	if report := stylecontract.AssessPurity(model.LiveAgentPlanAnchorStyleProfile{ReusableRules: []string{probe}}); !report.Passed {
		return "[已省略混入的业务内容]"
	}
	return value
}
