package decisionexecutor

import (
	"context"
	"log"

	"livecompanion/management/internal/addressing"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

// resolveGreyAddresses 把这一批灰色昵称一次性交给模型，判定能不能念、该怎么念。
// 只在这一批里有「值得救」的灰名时才被调用，一次互动最多一次。
// 返回切片与入参一一对应；调用或解析失败时返回空，调用方退群体词。
func (w *Worker) resolveGreyAddresses(ctx context.Context, s model.LiveRuntimeSession, item *decisionItem, names []string, rules *addressing.Rules) []string {
	if w == nil || w.agent == nil || len(names) == 0 {
		return nil
	}
	candidates := make([]addressing.GreyCandidate, 0, len(names))
	for i, name := range names {
		candidates = append(candidates, addressing.GreyCandidate{
			Viewer:    addressing.Viewer{UserID: itoaIndex(i), RawName: name},
			Candidate: rules.Check(name).Text,
		})
	}
	system, user := addressing.GreyPrompt(candidates, rules)
	response, err := w.completePaidModel(ctx, s, agentgateway.Request{
		Stage:          "interaction_addressing_grey",
		Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		EnableThinking: false,
		MaxTokens:      60*len(candidates) + 80,
		Timeout:        6e9,
	}, item.ID+":grey")
	if err != nil {
		log.Printf("interaction grey unavailable tenant=%d room=%d decision=%s batch=%d reason=%v", s.TenantID, s.RoomID, item.ID, len(candidates), err)
		return nil
	}
	decisions, err := addressing.ParseGrey(response.Text, candidates, rules)
	if err != nil {
		log.Printf("interaction grey invalid tenant=%d room=%d decision=%s reason=%v", s.TenantID, s.RoomID, item.ID, err)
		return nil
	}
	out := make([]string, len(names))
	for i, decision := range decisions {
		if i >= len(out) || !decision.OK {
			continue
		}
		out[i] = decision.Base
	}
	return out
}

// itoaIndex 只用于给候选编号，不参与任何身份判断。
func itoaIndex(i int) string {
	if i == 0 {
		return "0"
	}
	digits := make([]byte, 0, 3)
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
