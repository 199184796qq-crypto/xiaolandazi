package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

type interactionBusinessPlan struct {
	FormatWarning string                     `json:"-"`
	SourceSHA     string                     `json:"-"`
	Outline       InteractionAnswerOutline   `json:"answer_outline"`
	Basis         []string                   `json:"basis"`
	Requirements  []string                   `json:"requirements"`
	Alternatives  []interactionPlanCandidate `json:"alternatives,omitempty"`
}

type interactionPlanCandidate struct {
	Outline      InteractionAnswerOutline `json:"answer_outline"`
	Basis        []string                 `json:"basis"`
	Requirements []string                 `json:"requirements"`
}

// Segment source paragraphs for the one-question content task, preserving each
// sentence and its punctuation. This is source presentation, not semantic
// filtering or a model-created relation between records.
func businessSentenceUnits(text string) []string {
	var units []string
	var sentence strings.Builder
	for _, char := range text {
		sentence.WriteRune(char)
		if strings.ContainsRune("。！？!?\n", char) {
			if value := strings.TrimSpace(sentence.String()); value != "" {
				units = append(units, value)
			}
			sentence.Reset()
		}
	}
	if value := strings.TrimSpace(sentence.String()); value != "" {
		units = append(units, value)
	}
	return units
}

func decodeInteractionBusinessPlan(raw string) (*interactionBusinessPlan, error) {
	var fields map[string]json.RawMessage
	if err := contentstrategy.Decode(raw, &fields); err != nil {
		return nil, err
	}
	for key := range fields {
		if key != "answer_outline" && key != "basis" && key != "requirements" && key != "alternatives" {
			return nil, fmt.Errorf("互动业务规划含未知字段：%s", key)
		}
	}
	var alternatives []json.RawMessage
	if value, ok := fields["alternatives"]; ok {
		if string(value) == "null" || json.Unmarshal(value, &alternatives) != nil || len(alternatives) > 2 {
			return nil, fmt.Errorf("互动候选角度结构无效")
		}
	}
	delete(fields, "alternatives")
	encoded, _ := json.Marshal(fields)
	var plan interactionBusinessPlan
	if err := contentstrategy.Decode(string(encoded), &plan); err != nil {
		return nil, err
	}
	for _, candidateRaw := range alternatives {
		candidate, err := decodeInteractionBusinessPlan(string(candidateRaw))
		if err != nil {
			plan.FormatWarning = "部分候选角度格式不完整，已使用其余有效规划。"
			continue
		}
		plan.Alternatives = append(plan.Alternatives, interactionPlanCandidate{Outline: candidate.Outline, Basis: candidate.Basis, Requirements: candidate.Requirements})
	}
	check, _ := json.Marshal(map[string]any{"reply": "正文校验占位", "answer_outline": plan.Outline})
	if _, outline, _, err := decodeInteractionAnswer(string(check)); err != nil || outline == nil {
		return nil, fmt.Errorf("互动业务规划说明不完整")
	}
	if plan.Basis == nil || plan.Requirements == nil || len(plan.Basis) > 8 || len(plan.Requirements) > 6 {
		return nil, fmt.Errorf("互动业务规划依据或规则无效")
	}
	for _, group := range []struct {
		items []string
		limit int
	}{{plan.Basis, 700}, {plan.Requirements, 400}} {
		for _, text := range group.items {
			if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > group.limit {
				return nil, fmt.Errorf("互动业务规划内容无效或过长")
			}
		}
	}
	return &plan, nil
}

func (w *Worker) planInteractionAnswer(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (*interactionBusinessPlan, error) {
	raw, err := interactionpolicy.Read("interaction-agent-plan.md")
	if err != nil {
		return nil, err
	}
	item.BusinessCoreOnly = true
	item.BusinessPlanning = true
	item.BusinessPlan = nil
	item.StrategyConstraints = nil
	item.HiddenStrategyGuidance = nil
	item.MissionID = ""
	item.CurrentMainline, item.ResumeMainline, item.OriginMainline = "", "", ""
	prompt, err := w.answerPrompt(ctx, session, item)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(item.ManualOrigin, "test_simulation") {
		history, _ := json.Marshal(w.playedInteractionContent(ctx, session))
		prompt += "\n【近期实际使用目的与角度：不是事实来源】\n" + string(history)
	}
	request := agentgateway.Request{Stage: "interaction_agent_plan", Messages: []agentgateway.Message{{Role: "system", Content: string(raw)}, {Role: "user", Content: prompt}}, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 1400, EnableThinking: false, Timeout: 22 * time.Second}
	response, err := w.completeInteractionAnswer(ctx, session, request)
	if err != nil {
		return nil, err
	}
	plan, err := decodeInteractionBusinessPlan(response.Text)
	if err == nil {
		plan.SourceSHA = contentstrategy.SHA(string(raw) + prompt)
		used := []string{}
		for _, previous := range item.RecentAnswers {
			if strings.TrimSpace(previous.Question) == strings.TrimSpace(primaryQuestion(item)) {
				used = append(used, previous.Angle)
			}
		}
		if item.ManualOrigin != "test_simulation" {
			for _, played := range w.playedInteractionContent(ctx, session) {
				for _, question := range played.SampleQuestions {
					if strings.TrimSpace(question) == strings.TrimSpace(primaryQuestion(item)) {
						used = append(used, played.ContentAngle)
						break
					}
				}
			}
		}
		selectInteractionPlanAngle(plan, used)
	}
	return plan, err
}

// Candidates belong to this one program-owned question request. Selecting a
// content candidate never derives an entity reference from a model label.
func selectInteractionPlanAngle(plan *interactionBusinessPlan, used []string) {
	candidates := append([]interactionPlanCandidate{{Outline: plan.Outline, Basis: plan.Basis, Requirements: plan.Requirements}}, plan.Alternatives...)
	best, bestScore := 0, int(^uint(0)>>1)
	for i, candidate := range candidates {
		score := 0
		for j, angle := range used {
			if strings.TrimSpace(angle) == strings.TrimSpace(candidate.Outline.Angle) {
				score += 10 + j
			}
		}
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	selected := candidates[best]
	plan.Outline, plan.Basis, plan.Requirements = selected.Outline, selected.Basis, selected.Requirements
	plan.Alternatives = nil
}

type interactionExchangeStore interface {
	BeginModelExchange(context.Context, appdb.ModelExchangeScope, string, string, any) (int64, error)
	CompleteModelExchange(context.Context, int64, any, string, int64, *int64, *int64, *int64, error) error
}

var _ interactionExchangeStore = (*appdb.Store)(nil)

func (w *Worker) completeInteractionAnswer(ctx context.Context, session model.LiveRuntimeSession, request agentgateway.Request) (agentgateway.Response, error) {
	if session.ID > 0 {
		ctx = agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: session.TenantID, RoomID: session.RoomID, RuntimeID: session.ID, RequireLease: true})
	}
	source, ok := w.store.(interactionExchangeStore)
	if !ok {
		return w.agent.Complete(ctx, request)
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return agentgateway.Response{}, err
	}
	identity := ""
	if gateway, ok := w.agent.(interface {
		ModelRoleIdentity(context.Context, string) (string, error)
	}); ok {
		identity, err = gateway.ModelRoleIdentity(ctx, request.Stage)
		if err != nil {
			return agentgateway.Response{}, err
		}
	} else if gateway, ok := w.agent.(interface {
		TextGenerationIdentity(context.Context) (string, error)
	}); ok {
		identity, err = gateway.TextGenerationIdentity(ctx)
		if err != nil {
			return agentgateway.Response{}, err
		}
	}
	stage := request.Stage
	if stage == "speech_generation" {
		stage = "interaction_agent_answer"
	}
	id, err := source.BeginModelExchange(ctx, appdb.ModelExchangeScope{TenantID: session.TenantID, RoomID: session.RoomID, PlanID: plan.ID, RunID: session.ExternalID}, stage, identity, request)
	if err != nil {
		return agentgateway.Response{}, fmt.Errorf("互动请求记录失败: %w", err)
	}
	started := time.Now()
	response, callErr := w.agent.Complete(ctx, request)
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()
	var input, output, cached *int64
	if response.InputTokens > 0 {
		input = &response.InputTokens
	}
	if response.OutputTokens > 0 {
		output = &response.OutputTokens
	}
	if response.CachedInputTokens > 0 {
		cached = &response.CachedInputTokens
	}
	if err := source.CompleteModelExchange(persist, id, response, response.Text, time.Since(started).Milliseconds(), input, output, cached, callErr); err != nil {
		return agentgateway.Response{}, fmt.Errorf("互动返回记录失败: %w", err)
	}
	return response, callErr
}

func isViewerQuestionAnswer(item decisionItem) bool {
	if len(item.SampleQuestions) == 0 || strings.EqualFold(item.ExecutionMode, "verbatim") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.ManualOrigin)) {
	case "agent_input", "agent_input_preview":
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.MissionKind)) {
	case "", "reply_question", "reply_chat":
		return true
	}
	return item.Scene == "product_question" || item.Scene == "complaint"
}

func isBriefViewerQuestion(item decisionItem) bool {
	if !isViewerQuestionAnswer(item) || len(item.SampleQuestions) != 1 {
		return false
	}
	return utf8.RuneCountInString(strings.TrimSpace(item.SampleQuestions[0])) <= 14
}

type InteractionAnswerOutline = contentstrategy.AnswerOutline

func (item *decisionItem) attachInteractionAnswerOutline() {
	if item.AnswerOutline == nil {
		if item.SelectedContentPlan != nil {
			item.SelectedContentPlan.AnswerOutline = nil
		}
		return
	}
	item.ContentAngle = item.AnswerOutline.Angle
	if item.SelectedContentPlan == nil {
		item.SelectedContentPlan = &contentstrategy.Selection{Reason: "interaction_agent"}
	}
	item.SelectedContentPlan.AnswerOutline = item.AnswerOutline
}

type InteractionRecentAnswer struct {
	Question string `json:"question"`
	Purpose  string `json:"purpose"`
	Angle    string `json:"angle"`
	Reply    string `json:"reply"`
}

func ValidateInteractionRecentAnswers(items []InteractionRecentAnswer) error {
	if len(items) > 8 {
		return fmt.Errorf("近期测试记录最多8条")
	}
	for _, item := range items {
		if strings.TrimSpace(item.Question) == "" || utf8.RuneCountInString(item.Question) > 1000 || utf8.RuneCountInString(item.Reply) > 1000 || utf8.RuneCountInString(item.Purpose) > 180 || utf8.RuneCountInString(item.Angle) > 180 {
			return fmt.Errorf("近期测试记录无效或过长")
		}
	}
	return nil
}

func decodeInteractionAnswer(raw string) (string, *InteractionAnswerOutline, string, error) {
	var fields map[string]json.RawMessage
	if err := contentstrategy.Decode(raw, &fields); err != nil {
		// Legacy plain-text responses remain usable, but are never presented as a
		// completed business plan. Broken JSON must not leak into spoken text.
		plain := strings.TrimSpace(raw)
		if plain != "" && !strings.HasPrefix(plain, "{") && !strings.HasPrefix(plain, "[") && !strings.HasPrefix(plain, "```") {
			return plain, nil, "本次回答说明未完成，可以重新测试。", nil
		}
		return "", nil, "", fmt.Errorf("互动回答不是有效JSON: %w", err)
	}
	var reply string
	if err := json.Unmarshal(fields["reply"], &reply); err != nil || strings.TrimSpace(reply) == "" {
		return "", nil, "", fmt.Errorf("互动回答正文缺失或无效")
	}
	reply = strings.TrimSpace(reply)
	// Advisory formatting failures must not discard a usable answer or invent an explanation.
	invalid := func() (string, *InteractionAnswerOutline, string, error) {
		return reply, nil, "本次未返回完整的回答说明，可以重新测试。", nil
	}
	for key := range fields {
		if key != "reply" && key != "answer_outline" && key != "needs_review" && key != "review" {
			return invalid()
		}
	}
	var outlineFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["answer_outline"], &outlineFields); err != nil || len(outlineFields) != 4 {
		return invalid()
	}
	var outline InteractionAnswerOutline
	if err := json.Unmarshal(fields["answer_outline"], &outline); err != nil {
		return invalid()
	}
	for _, name := range []string{"purpose", "angle", "aspects", "avoid"} {
		value, ok := outlineFields[name]
		if !ok || string(value) == "null" {
			return invalid()
		}
	}
	outline.Purpose = strings.TrimSpace(outline.Purpose)
	outline.Angle = strings.TrimSpace(outline.Angle)
	if outline.Purpose == "" || outline.Angle == "" || utf8.RuneCountInString(outline.Purpose) > 180 || utf8.RuneCountInString(outline.Angle) > 180 || len(outline.Aspects) < 1 || len(outline.Aspects) > 4 || len(outline.Avoid) > 6 {
		return invalid()
	}
	for _, items := range [][]string{outline.Aspects, outline.Avoid} {
		for i, value := range items {
			items[i] = strings.TrimSpace(value)
			if items[i] == "" || utf8.RuneCountInString(items[i]) > 200 {
				return invalid()
			}
		}
	}
	return reply, &outline, "", nil
}
