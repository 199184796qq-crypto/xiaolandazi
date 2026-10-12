package decisionexecutor

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

type factGapVerdict struct {
	Verdict         string   `json:"verdict"`
	FabricatedFacts []string `json:"fabricated_facts"`
	MissingFacts    []string `json:"missing_facts"`
}

// checkFactGap 用审核模型核对「回答里有没有硬事实编造」——事实库里没有依据、
// 且属于硬事实（数字/产地/工艺/功效/功能/属性/适用人群）的断言。
// 发现编造时返回 missing_fact review（供 recordInteractionReview 记录 + 提醒商家），否则返回 nil。
// 只抓硬事实、不抓软延展（主观感受/场景/情绪），避免回到"审核过严"。
func (w *Worker) checkFactGap(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, text string) *model.InteractionLearningReview {
	if w == nil || w.agent == nil || w.store == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil || plan.ID <= 0 {
		return nil
	}
	productContext, err := w.interactionProductContext(ctx, session, plan.ID, item.BusinessCoreOnly)
	if err != nil {
		return nil
	}
	mother, err := interactionpolicy.Read("fact-gap-check.md")
	if err != nil {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	resp, err := w.agent.Complete(callCtx, agentgateway.Request{
		Stage:          "fact_gap_check",
		Messages:       []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: "【商品事实库】\n" + productContext + "\n\n【主播回答】\n" + text}},
		ResponseFormat: agentgateway.ResponseJSON,
		MaxTokens:      400,
		EnableThinking: false,
	})
	if err != nil {
		return nil
	}
	var verdict factGapVerdict
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Text)), &verdict); err != nil {
		log.Printf("fact gap check parse tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
		return nil
	}
	if verdict.Verdict != "fabricated" {
		return nil
	}
	reason := "回答存在事实库之外的硬事实编造"
	if len(verdict.FabricatedFacts) > 0 {
		reason += "：" + strings.Join(verdict.FabricatedFacts, "、")
	}
	if len(verdict.MissingFacts) == 0 {
		verdict.MissingFacts = []string{primaryQuestion(item)}
	}
	return &model.InteractionLearningReview{
		Kind:        "missing_fact",
		Meaning:     primaryQuestion(item),
		Context:     primaryQuestion(item),
		Reason:      reason,
		NeededFacts: verdict.MissingFacts,
	}
}
