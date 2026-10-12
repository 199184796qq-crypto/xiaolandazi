package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/interactionlearning"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
)

func interactionScene(kind string) string {
	switch kind {
	case "reply_question":
		return "product_question"
	case "conversion_signal":
		return "order"
	case "welcome_named", "welcome_batch":
		return "welcome"
	case "reply_like", "reply_follow":
		return "like_follow"
	case "reply_chat":
		return "smalltalk"
	default:
		return kind
	}
}

type playedContent struct {
	ContentGoal         string          `json:"content_goal"`
	ContentAngle        string          `json:"content_angle"`
	Scene               string          `json:"scene"`
	SampleQuestions     []string        `json:"sample_questions,omitempty"`
	SelectedContentPlan json.RawMessage `json:"selected_content_plan,omitempty"`
	Purpose             string          `json:"purpose,omitempty"`
}

func (w *Worker) playedInteractionContent(ctx context.Context, s model.LiveRuntimeSession) []playedContent {
	if w.core == nil {
		return nil
	}
	resp, err := w.core.DoRoom(ctx, s.TenantID, s.RoomID, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions", s.RoomID), tenantQuery(s.TenantID), nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var snapshot struct {
		Played []playedContent `json:"played_history"`
	}
	if json.NewDecoder(resp.Body).Decode(&snapshot) != nil {
		return nil
	}
	if len(snapshot.Played) > 8 {
		snapshot.Played = snapshot.Played[len(snapshot.Played)-8:]
	}
	for i := range snapshot.Played {
		played := &snapshot.Played[i]
		var plan struct {
			Outline *InteractionAnswerOutline `json:"answer_outline"`
		}
		if json.Unmarshal(played.SelectedContentPlan, &plan) == nil && plan.Outline != nil {
			played.Purpose = plan.Outline.Purpose
		}
		// History is usage evidence only. Do not re-inject old fact packages.
		played.SelectedContentPlan = nil
	}
	return snapshot.Played
}

// A phase is recognized only from the actual mainline content. No wall-clock
// estimate turns a room into a conversion stage.
func interactionProgress(text string) string {
	switch {
	case strings.Contains(text, "最后再确认") || strings.Contains(text, "今天就到这里"):
		return "closing"
	case strings.Contains(text, "点击购买") || strings.Contains(text, "点击下方链接") || strings.Contains(text, "下单步骤"):
		return "purchase"
	case strings.Contains(text, "怎么选") || strings.Contains(text, "规格区别") || strings.Contains(text, "适合哪种"):
		return "selection"
	case strings.Contains(text, "欢迎来到") || strings.Contains(text, "今天给大家介绍"):
		return "opening"
	default:
		return "explanation"
	}
}
func containsContentValue(values []string, value string) bool {
	if len(values) == 0 {
		return true
	}
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func chooseContentGoal(rule model.InteractionEventRule, heat, stage string, history []playedContent, scene string) (model.InteractionGoal, string, error) {
	bestScore := -100000
	var best model.InteractionGoal
	angle := ""
	for _, g := range rule.Goals {
		if !containsContentValue(g.Heats, heat) || !containsContentValue(g.Stages, stage) {
			continue
		}
		specificity := 0
		if len(g.Heats) > 0 {
			specificity += 40
		}
		if len(g.Stages) > 0 {
			specificity += 30
		}
		for _, a := range g.Angles {
			score := specificity
			for i, p := range history {
				if p.Scene != scene {
					continue
				}
				penalty := 5 + i
				if p.ContentGoal == g.Key {
					score -= penalty
				}
				if p.ContentAngle == a {
					score -= penalty * 2
				}
			}
			if score > bestScore {
				bestScore = score
				best = g
				angle = a
			}
		}
	}
	if best.Key == "" {
		return best, "", fmt.Errorf("当前热度和主线阶段缺少适用互动目的")
	}
	return best, angle, nil
}
func (w *Worker) interactionContentGuidance(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, model.InteractionGoal, string, error) {
	var cfg model.InteractionSchedule
	var err error
	if len(item.Schedule) > 0 {
		cfg, err = interactionpolicy.Decode(item.Schedule)
		if err == nil {
			err = interactionpolicy.Validate(cfg)
		}
	} else {
		cfg, err = interactionpolicy.Load()
	}
	if err != nil {
		return "", model.InteractionGoal{}, "", err
	}
	scene := interactionScene(item.MissionKind)
	rule, ok := cfg.Events[scene]
	if !ok {
		return "", model.InteractionGoal{}, "", fmt.Errorf("未知互动场景: %s", scene)
	}
	current := item.CurrentMainline
	if current == "" {
		current = item.OriginMainline
	}
	heat := strings.ToUpper(strings.TrimSpace(item.InteractionDecision.Heat))
	if cfg.HeatLabels[heat] == "" {
		heat = "WARM"
	}
	stage := interactionProgress(current)
	history := w.playedInteractionContent(ctx, session)
	goal, angle, err := chooseContentGoal(rule, heat, stage, history, scene)
	if err != nil {
		return "", goal, "", err
	}
	data := map[string]any{"场景": rule.Name, "本次目的": goal.Name, "期望观众行动": goal.Action, "本次角度": angle, "热度": cfg.HeatLabels[heat], "在线人数": item.InteractionDecision.OnlineCount, "近30秒独立发言人数": item.InteractionDecision.UniqueChatters30s, "问题压力": item.InteractionDecision.QuestionPressure, "当前主线阶段": cfg.StageLabels[stage], "当前正在讲": current, "事件发生时正在讲": item.OriginMainline, "订单依据来源": item.OrderSource, "聚合事件数": item.MissionEventCount, "近期实际播过的目的与角度": history, "房间配置版本": cfg.Version}
	if item.OrderSource == "" && scene == "order" {
		data["订单依据来源"] = "viewer_claim"
	}
	data["涉及多人"] = item.MissionEventCount > 1 || len(item.Nicknames) > 1
	data["本批观众昵称"] = item.promptNicknames()
	if item.PromptNicknames != nil {
		data["昵称已按称呼规划处理"] = true
	}
	raw, err := interactionpolicy.Read("interaction-core-content.md")
	if err != nil {
		return "", goal, "", err
	}
	encoded, _ := json.Marshal(data)
	return strings.ReplaceAll(string(raw), "{{SCENE_DATA}}", string(encoded)), goal, angle, nil
}
func (w *Worker) interactionMemoryFingerprint(ctx context.Context, s model.LiveRuntimeSession) (string, error) {
	memories, err := w.store.ListActiveAgentMemories(ctx, s.TenantID, s.RoomID)
	if err != nil {
		return "", err
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, s.TenantID, s.RoomID)
	if err != nil {
		return "", err
	}
	memories = interactionlearning.GenerationMemories(memories, s.TenantID, s.RoomID, plan.ID)
	raw, err := json.Marshal(memories)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}
