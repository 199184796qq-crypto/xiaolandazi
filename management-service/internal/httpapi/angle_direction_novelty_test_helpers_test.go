package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
)

type directionNoveltyFeedback struct {
	Novel      bool   `json:"novel"`
	Reason     string `json:"reason"`
	Raw        string `json:"raw"`
	ExchangeID int64  `json:"exchange_id"`
}

func parseDirectionNovelty(raw string) (directionNoveltyFeedback, error) {
	lines := strings.SplitN(strings.TrimSpace(raw), "\n", 2)
	label := strings.TrimSpace(lines[0])
	if label != "新增" && label != "重复" {
		return directionNoveltyFeedback{}, errors.New("方向新增反馈未返回约定判断")
	}
	f := directionNoveltyFeedback{Novel: label == "新增", Raw: raw}
	if len(lines) == 2 {
		f.Reason = strings.TrimSpace(lines[1])
	}
	return f, nil
}

// A single content feedback request, never a model-maintained relation table.
// This assesses duplication only; it does not approve facts or spoken content.
func (s *Server) directionNovelty(ctx context.Context, state *anchorcycle.State, profile agentgateway.SpeechModel, candidate string, old []string) (directionNoveltyFeedback, error) {
	if err := requireCycleP0(state); err != nil {
		return directionNoveltyFeedback{}, err
	}
	mother, err := interactionpolicy.Read("angle-direction-novelty.md")
	if err != nil {
		return directionNoveltyFeedback{}, err
	}
	// Compare the most recently retained direction first, so adjacent paraphrases
	// are not lost at the end of a growing historical list.
	recentFirst := append([]string(nil), old...)
	for left, right := 0, len(recentFirst)-1; left < right; left, right = left+1, right-1 {
		recentFirst[left], recentFirst[right] = recentFirst[right], recentFirst[left]
	}
	packet, _ := json.Marshal(map[string]any{"本次角度": generationTaskView(state.Task()), "候选方向": candidate, "已有方向（最近保留的排在前面）": recentFirst, "有效P0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)})
	zero := 0.0
	request := agentgateway.Request{Stage: "mainline_direction_novelty", Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: string(packet)}}, Temperature: &zero, MaxTokens: 250, ResponseFormat: agentgateway.ResponseText, EnableThinking: false, Timeout: 8 * time.Second}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	id, err := s.beginModelExchange(ctx, request.Stage, profile.ID, request)
	if err != nil {
		return directionNoveltyFeedback{}, err
	}
	started := time.Now()
	response, err := agentgateway.CompleteSpeechEndpoint(ctx, profile, request)
	response.Provider, response.Model = "anchor:"+profile.ID, profile.Model
	err = s.finishModelExchange(ctx, id, response, response.Text, started, response.InputTokens, response.OutputTokens, err)
	if err != nil {
		return directionNoveltyFeedback{}, err
	}
	feedback, err := parseDirectionNovelty(response.Text)
	feedback.ExchangeID = id
	return feedback, err
}

// Bound the whole optional supplement check. Unconfirmed new cards stay in raw
// history; existing usable cards keep supplying the live stream.
func (s *Server) filterAngleSupplement(ctx context.Context, state *anchorcycle.State, profile agentgateway.SpeechModel, plan, previous *anchorcycle.AnglePlan) {
	plan.GeneratedCards = append([]anchorcycle.AngleHook(nil), plan.Cards...)
	if previous == nil || len(previous.Cards) == 0 {
		plan.Cards = anchorcycle.DistinctDirectionCards(plan.Cards)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	old := []string{}
	seen := map[string]bool{}
	for _, c := range previous.Cards {
		old = append(old, c.Action)
		seen[strings.TrimSpace(c.Action)] = true
	}
	kept := []anchorcycle.AngleHook{}
	for _, c := range plan.GeneratedCards {
		decision := anchorcycle.DirectionSelection{HookID: c.ID, Reason: "与已有方向原文相同"}
		key := strings.TrimSpace(c.Action)
		if !seen[key] {
			feedback, err := s.directionNovelty(ctx, state, profile, c.Action, old)
			decision.Kept, decision.Reason, decision.ExchangeID = feedback.Novel, feedback.Reason, feedback.ExchangeID
			if err != nil {
				decision.Kept = false
				decision.Reason = "新增方向反馈未确认，本次不采用，保留原始内容"
			}
		}
		if decision.Kept {
			kept = append(kept, c)
			old = append(old, c.Action)
			seen[key] = true
		}
		plan.DirectionSelections = append(plan.DirectionSelections, decision)
	}
	plan.Cards = kept
}
