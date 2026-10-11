package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

// The same adopted ledger gates simulated and live interaction before TTS.
func (w *Worker) verifySpeechExpressionConstraints(ctx context.Context, session model.LiveRuntimeSession, text string) (string, error) {
	source, ok := w.store.(expressionConstraintStore)
	if !ok {
		return text, nil
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", err
	}
	rules, err := source.CurrentSpeechExpressionConstraints(ctx, session.TenantID, plan.ID)
	if err != nil {
		return "", err
	}
	if len(rules.Corrections) == 0 {
		return text, nil
	}
	ids := []int64{}
	for _, p := range rules.Products {
		ids = append(ids, p.ID)
	}
	adopted := contentpools.CorrectionView(rules, ids)
	if len(adopted) == 0 {
		return text, nil
	}
	mother, err := interactionpolicy.Read("speech-constraint-text-review.md")
	if err != nil {
		return "", err
	}
	p0, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		return "", err
	}
	industry, l1, l2, l3, err := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", err
	}
	effective := policy.BuildEffective(industry, l1, l2, l3)
	payload, _ := json.Marshal(map[string]any{"text": text, "adopted_constraints": adopted, "effective_p0": string(p0), "effective_runtime_rules": effective.PromptText})
	response, err := w.agent.Complete(ctx, agentgateway.Request{Stage: "speech_constraint_text_review", Messages: []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: string(payload)}}, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 900, EnableThinking: false})
	if err != nil {
		return "", fmt.Errorf("用户表达约束审核未完成：%w", err)
	}
	result, err := agentgateway.ParseExpressionReview(response.Text, text)
	if err != nil {
		return "", fmt.Errorf("用户表达约束审核结果不完整：%w", err)
	}
	if !*result.Passed {
		return "", fmt.Errorf("话术未遵守已采用约束：%s", result.Reason)
	}
	return text, nil
}
