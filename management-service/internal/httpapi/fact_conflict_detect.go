package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

// factConflictCheck 非阻塞地检测「待采纳/待编辑的事实」是否与当前方案的
// 既有事实、商品卡（规格/适用人数/价格）存在语义矛盾。
//
// 设计要点：
//   - 只做提示（warning），不阻止保存——因为「6人是上限、3-4人也适合」这类
//     区间关系是合理的，不能武断硬拦。
//   - 模型调用失败、超时、返回不可解析时，静默降级为「未检测」，绝不影响保存。
//   - 每次检测用受控的上下文（现有事实 + 商品卡 + 待检事实），不塞全量无关数据。
func (s *Server) factConflictCheck(
	ctx context.Context,
	tenantID, planID int64,
	candidate model.LiveAgentPlanFactCandidate,
) *model.FactConflictWarning {
	// 用独立超时，避免拖慢保存主链路。
	detectCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	existingFacts, err := s.store.ListLiveAgentPlanFacts(detectCtx, tenantID, planID)
	if err != nil || len(existingFacts) == 0 {
		// 没有既有事实可比对，跳过检测。
		return nil
	}
	links, _ := s.store.ListLiveAgentPlanProductLinks(detectCtx, tenantID, planID)

	prompt := buildFactConflictPrompt(existingFacts, links, candidate)

	response, err := agentgateway.NewFromContext(detectCtx).Complete(detectCtx, agentgateway.Request{
		Stage:    "fact_conflict_detect",
		Messages: []agentgateway.Message{
			{Role: "system", Content: factConflictSystemPrompt},
			{Role: "user", Content: prompt},
		},
		MaxTokens:      320,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        8 * time.Second,
	})
	if err != nil {
		return nil
	}

	var raw struct {
		HasConflict   bool     `json:"has_conflict"`
		Conflict      string   `json:"conflict"`
		Suggestion    string   `json:"suggestion"`
		RelatedFactKeys []string `json:"related_fact_keys"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(response.Text)), &raw); err != nil {
		return nil
	}
	if !raw.HasConflict {
		return nil
	}
	return &model.FactConflictWarning{
		Detected:     true,
		Conflict:     strings.TrimSpace(raw.Conflict),
		Suggestion:   strings.TrimSpace(raw.Suggestion),
		RelatedFacts: raw.RelatedFactKeys,
	}
}

const factConflictSystemPrompt = `你是直播带货「事实依据」的矛盾检测器。你的任务只有一件事：判断用户新输入的一条事实，是否与现有事实或商品卡（规格/适用人数/价格/份量等）在语义上互相冲突。

判定原则：
1. 「6人是上限，3-4人也适合」这类是【区间/包含】关系，不算矛盾；只有【互相否定/数字对不上/张冠李戴】才算矛盾。
2. 只报告明确的语义冲突，不臆测、不补全用户没写的信息。
3. 有矛盾时，给出简短建议：建议把两条事实合并成一条「区间+上限」的完整表述，或建议区分不同事实字段。

只返回严格 JSON，不要任何多余文字：
{"has_conflict": true|false, "conflict": "冲突点简述", "suggestion": "建议改写", "related_fact_keys": ["相关事实的名称"]}`

func buildFactConflictPrompt(
	existing []model.LiveAgentPlanFact,
	links []model.LiveAgentPlanProductLink,
	candidate model.LiveAgentPlanFactCandidate,
) string {
	var b strings.Builder
	b.WriteString("现有事实：\n")
	for _, f := range existing {
		b.WriteString("- [")
		b.WriteString(f.Category)
		b.WriteString("/")
		b.WriteString(f.Key)
		b.WriteString("] ")
		b.WriteString(f.Value)
		b.WriteString("\n")
	}
	if len(links) > 0 {
		b.WriteString("\n商品卡：\n")
		for _, l := range links {
			b.WriteString("- 链接「")
			b.WriteString(l.LinkKey)
			b.WriteString("」")
			if l.ProductName != "" {
				b.WriteString(" 商品=")
				b.WriteString(l.ProductName)
			}
			if l.Spec != "" {
				b.WriteString(" 规格=")
				b.WriteString(l.Spec)
			}
			if l.Audience != "" {
				b.WriteString(" 适用人数=")
				b.WriteString(l.Audience)
			}
			if l.DailyPrice != "" {
				b.WriteString(" 日常价=")
				b.WriteString(l.DailyPrice)
			}
			if l.Quantity != "" {
				b.WriteString(" 数量=")
				b.WriteString(l.Quantity)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n新输入的事实：\n- [")
	b.WriteString(candidate.Category)
	b.WriteString("/")
	b.WriteString(candidate.Key)
	b.WriteString("] ")
	b.WriteString(candidate.Value)
	b.WriteString("\n")
	return b.String()
}
