package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/agentmemory"
	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/interactionlearning"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
	"livecompanion/management/internal/ttsgateway"
)

type returnBridgePlan struct {
	AllowConsecutiveRepeat bool   `json:"allow_consecutive_repeat,omitempty"`
	ReturnAllowsRepeat     bool   `json:"return_allows_repeat,omitempty"`
	ProgramID              string `json:"program_id"`
	TaskID                 string `json:"task_id"`
	Token                  string `json:"token"`
	BeforeText             string `json:"before_text"`
	ReplyText              string `json:"reply_text"`
	Units                  []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"units"`
	SkippedUnits     []string  `json:"skipped_units"`
	AudioStartedAt   time.Time `json:"audio_started_at"`
	Deadline         time.Time `json:"deadline"`
	AnswerDurationMS int       `json:"answer_duration_ms"`
}
type bridgeText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func validateBridge(raw string, plan returnBridgePlan) (bridgeText, error) {
	var fields map[string]json.RawMessage
	if err := contentstrategy.Decode(raw, &fields); err != nil || fields["type"] == nil || fields["text"] == nil || string(fields["type"]) == "null" || string(fields["text"]) == "null" {
		return bridgeText{}, fmt.Errorf("bridge requires type and text")
	}
	var output bridgeText
	if err := json.Unmarshal(fields["type"], &output.Type); err != nil {
		return output, err
	}
	if err := json.Unmarshal(fields["text"], &output.Text); err != nil {
		return output, err
	}
	allowed := map[string]bool{"recall_continue": true, "same_topic": true, "topic_shift": true, "close_return": true, "reanchor": true, "none": true}
	if !allowed[output.Type] {
		return output, fmt.Errorf("unknown bridge type")
	}
	output.Text = strings.TrimSpace(output.Text)
	if output.Type == "none" {
		if output.Text != "" {
			return output, fmt.Errorf("none bridge contains speech")
		}
		return output, nil
	}
	if n := len([]rune(output.Text)); n < 6 || n > 24 {
		return output, fmt.Errorf("bridge length outside 6-24 characters")
	}
	return output, nil
}

func (w *Worker) bridgeBudget() time.Duration {
	w.mu.Lock()
	samples := append([]time.Duration(nil), w.bridgeSamples...)
	w.mu.Unlock()
	if len(samples) == 0 {
		return 8 * time.Second
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return max(5*time.Second, samples[(len(samples)*95-1)/100]+time.Second)
}
func (w *Worker) recordBridgeElapsed(elapsed time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bridgeSamples = append(w.bridgeSamples, elapsed)
	w.bridgeSamples = w.bridgeSamples[max(0, len(w.bridgeSamples)-32):]
}

func (w *Worker) bridgeModel(ctx context.Context) (agentgateway.SpeechModel, error) {
	source, ok := w.store.(agentgateway.SpeechModelResolver)
	if !ok {
		return agentgateway.SpeechModel{}, fmt.Errorf("interaction model registry unavailable")
	}
	id := ""
	if roles, ok := w.store.(agentgateway.TextRoleResolver); ok {
		var err error
		id, err = roles.ResolveTextRole(ctx, "speech_generation")
		if err != nil {
			return agentgateway.SpeechModel{}, err
		}
	}
	profile, err := source.ResolveSpeechModel(ctx, id)
	if err != nil || profile == nil || !profile.Enabled {
		return agentgateway.SpeechModel{}, fmt.Errorf("interaction model unavailable")
	}
	return *profile, nil
}

func (w *Worker) loadBridgePlan(ctx context.Context, session model.LiveRuntimeSession, taskID, decisionID string) (returnBridgePlan, error) {
	query := tenantQuery(session.TenantID)
	query.Set("task_id", taskID)
	query.Set("decision_id", decisionID)
	response, err := w.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/audio/interaction-return", session.RoomID), query, nil)
	if err != nil {
		return returnBridgePlan{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return returnBridgePlan{}, fmt.Errorf("return scope http %d", response.StatusCode)
	}
	var result returnBridgePlan
	err = json.NewDecoder(response.Body).Decode(&result)
	return result, err
}

// Begins only after approved core TTS is ready. Core exposes a reservation
// once it confirms the actual safe cut; failed dispatch cancels this producer.
func (w *Worker) startReturnBridge(session model.LiveRuntimeSession, item decisionItem, scope, memory string) context.CancelFunc {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	if _, ok := w.store.(agentgateway.SpeechModelResolver); !ok {
		cancel()
		return cancel
	}
	if item.ManualOrigin == "test_simulation" || item.ManualOrigin == "agent_input_preview" {
		cancel()
		return cancel
	}
	select {
	case w.bridgeSlots <- struct{}{}:
	default:
		cancel()
		return cancel
	}
	go func() {
		defer func() { <-w.bridgeSlots; cancel() }()
		var plan returnBridgePlan
		var err error
		for attempt := 0; attempt < 30; attempt++ {
			plan, err = w.loadBridgePlan(ctx, session, "", item.ID)
			if err == nil {
				break
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		if err != nil || len(plan.Units) == 0 {
			log.Printf("bridge skipped room=%d reason=return_unavailable", session.RoomID)
			return
		}
		if remaining := time.Until(plan.Deadline); remaining < w.bridgeBudget()+3*time.Second {
			log.Printf("bridge skipped room=%d remaining_ms=%d reason=insufficient_playback_budget", session.RoomID, remaining.Milliseconds())
			return
		}
		deadlineCtx, deadlineCancel := context.WithDeadline(ctx, plan.Deadline)
		defer deadlineCancel()
		began := time.Now()
		err = w.generateAndStageBridge(deadlineCtx, session, item, plan, scope, memory)
		if err != nil {
			log.Printf("bridge discarded room=%d decision=%s error=%v", session.RoomID, item.ID, err)
		}
		if err == nil || deadlineCtx.Err() != nil {
			w.recordBridgeElapsed(time.Since(began))
		}
	}()
	return cancel
}

func (w *Worker) generateAndStageBridge(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, plan returnBridgePlan, scope, memory string) error {
	if session.ID > 0 {
		ctx = agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: session.TenantID, RoomID: session.RoomID, RuntimeID: session.ID, RequireLease: true})
	}
	profile, err := w.bridgeModel(ctx)
	if err != nil {
		return err
	}
	template, err := interactionpolicy.Read("interaction-return-bridge-generation.md")
	if err != nil {
		return err
	}
	memories, err := w.store.ListActiveAgentMemories(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return err
	}
	planID := int64(0)
	if current, e := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID); e == nil {
		planID = current.ID
	}
	memories = interactionlearning.GenerationMemories(memories, session.TenantID, session.RoomID, planID)
	effective, err := policy.LoadGeneration()
	if err != nil {
		return err
	}
	input, _ := json.Marshal(map[string]any{"切点前完整上下文": plan.BeforeText, "核心回答": plan.ReplyText, "实际回归及后续完整内容": plan.Units, "已跳过完整重复单元": plan.SkippedUnits, "已采用用户纠正": agentmemory.Prompt(memories), "有效P0": effective.PromptText})
	result, err := w.agent.Complete(ctx, agentgateway.Request{Stage: "interaction_bridge", Provider: "anchor:" + profile.ID, Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: string(template)}, {Role: "user", Content: string(input)}}, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 128, EnableThinking: false, Timeout: 4 * time.Second})
	if err != nil {
		return err
	}
	bridge, err := validateBridge(result.Text, plan)
	if err != nil {
		return err
	}
	if bridge.Type == "none" {
		return nil
	}
	entry := ""
	if len(plan.Units) > 0 {
		entry = plan.Units[0].Text
	}
	if contentstrategy.BridgeDuplicatesAdjacent(bridge.Text, plan.ReplyText, entry, plan.AllowConsecutiveRepeat || plan.ReturnAllowsRepeat) {
		log.Printf("bridge omitted before TTS room=%d decision=%s reason=consecutive_exact_duplicate", session.RoomID, item.ID)
		return nil
	}
	if contentstrategy.BridgePreviewsEntry(bridge.Text, entry) {
		log.Printf("bridge omitted before TTS room=%d decision=%s reason=previews_entry_topic text=%q", session.RoomID, item.ID, bridge.Text)
		return nil
	}
	voice, ok, err := w.readyVoice(ctx, session.TenantID, session.RoomID)
	if err != nil || !ok {
		return fmt.Errorf("bridge voice unavailable")
	}
	audio, err := w.tts.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{Provider: voiceProvider(voice), Model: voiceModel(voice), VoiceID: voice.VoiceID, Text: bridge.Text, Rate: voiceRate(voice)})
	if err != nil || audio.AudioURL == "" {
		return fmt.Errorf("bridge TTS unavailable: %v", err)
	}
	currentScope, err := w.interactionPreparationScope(ctx, session, item)
	if err != nil || currentScope != scope {
		return fmt.Errorf("bridge facts/session/voice changed")
	}
	currentMemory, err := w.interactionMemoryFingerprint(ctx, session)
	if err != nil || currentMemory != memory {
		return fmt.Errorf("bridge corrections changed")
	}
	response, err := w.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodPost, fmt.Sprintf("/internal/v1/rooms/%d/audio/interaction-bridge", session.RoomID), tenantQuery(session.TenantID), map[string]any{"decision_id": item.ID, "task_id": plan.TaskID, "token": plan.Token, "text": bridge.Text, "audio_url": audio.AudioURL})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("bridge missed scope/deadline: http %d", response.StatusCode)
	}
	var staged struct {
		Staged     bool   `json:"staged"`
		Reason     string `json:"reason"`
		DurationMS int    `json:"duration_ms"`
	}
	if err := json.NewDecoder(response.Body).Decode(&staged); err != nil {
		return err
	}
	if !staged.Staged {
		log.Printf("bridge omitted room=%d decision=%s reason=%s", session.RoomID, item.ID, staged.Reason)
		return nil
	}
	log.Printf("bridge staged room=%d decision=%s type=%s model=%s text=%q", session.RoomID, item.ID, bridge.Type, profile.Model, bridge.Text)
	w.archiveSessionAudio(ctx, session, "bridge:"+item.ID, "bridge", bridge.Text, audio.AudioURL, staged.DurationMS)
	return nil
}
