package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/agentmemory"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicecatalog"
)

const (
	defaultInterval = 800 * time.Millisecond
	errorBackoff    = 15 * time.Second
	maxWorkers      = 4
	maxSpeechRunes  = 300
)

type store interface {
	ListRunningLiveRuntimeSessions(context.Context) ([]model.LiveRuntimeSession, error)
	ListLiveAgentConfigVersions(context.Context, int64) ([]model.AgentConfigVersion, error)
	GetVoiceProfile(context.Context, int64, int64) (model.VoiceProfile, error)
	LoadLivePolicyLayers(context.Context, int64, int64) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error)
	GetLiveAgentPlanForRoom(context.Context, int64, int64) (model.LiveAgentPlan, error)
	ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error)
	RecordGeneratedSpeechHistory(context.Context, model.GeneratedSpeechHistoryInput) error
	AgentPromptValue(context.Context, string, string) string
	RenderAgentPrompt(context.Context, string, string, map[string]string) string
}

type coreDoer interface {
	DoRoom(context.Context, int64, int64, string, string, url.Values, any) (*http.Response, error)
}

type completer interface {
	Complete(context.Context, agentgateway.Request) (agentgateway.Response, error)
}

type synthesizer interface {
	SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error)
}

type leader interface {
	IsLeader() bool
}

type Worker struct {
	store    store
	core     coreDoer
	agent    completer
	tts      synthesizer
	leader   leader
	interval time.Duration
	now      func() time.Time

	mu         sync.Mutex
	inFlight   map[int64]bool
	retryAfter map[int64]time.Time
}

type decisionItem struct {
	ID                     string   `json:"id"`
	Topic                  string   `json:"topic"`
	Title                  string   `json:"title"`
	Summary                string   `json:"summary"`
	ReplyHint              string   `json:"reply_hint"`
	SampleQuestions        []string `json:"sample_questions"`
	ManualAction           string   `json:"manual_action"`
	ManualOrigin           string   `json:"manual_origin"`
	ExecutionMode          string   `json:"execution_mode"`
	FixedText              string   `json:"fixed_text"`
	PreviewInstruction     string   `json:"preview_instruction,omitempty"`
	PreviewMemoryType      string   `json:"preview_memory_type,omitempty"`
	PreviewMemoryKey       string   `json:"preview_memory_key,omitempty"`
	PreviewMatchedMemoryID int64    `json:"preview_matched_memory_id,omitempty"`
}

type claimResponse struct {
	Claimed bool          `json:"claimed"`
	Reason  string        `json:"reason"`
	Item    *decisionItem `json:"item"`
}

func New(s store, core coreDoer, agent completer, tts synthesizer, leaders ...leader) *Worker {
	w := &Worker{
		store:      s,
		core:       core,
		agent:      agent,
		tts:        tts,
		interval:   defaultInterval,
		now:        func() time.Time { return time.Now().UTC() },
		inFlight:   make(map[int64]bool),
		retryAfter: make(map[int64]time.Time),
	}
	if len(leaders) > 0 {
		w.leader = leaders[0]
	}
	return w
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.store == nil || w.core == nil || w.agent == nil || w.tts == nil {
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.runCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runCycle(ctx)
		}
	}
}

func (w *Worker) runCycle(ctx context.Context) {
	if w.leader != nil && !w.leader.IsLeader() {
		return
	}
	sessions, err := w.store.ListRunningLiveRuntimeSessions(ctx)
	if err != nil {
		log.Printf("decision executor list runtime sessions: %v", err)
		return
	}
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup
	for _, session := range sessions {
		if !strings.EqualFold(strings.TrimSpace(session.Status), "running") || !w.beginRoom(session.RoomID) {
			continue
		}
		session := session
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer w.endRoom(session.RoomID)
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := w.processRoom(ctx, session); err != nil {
				w.backoffRoom(session.RoomID)
				log.Printf("decision executor tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
			}
		}()
	}
	wg.Wait()
}

func (w *Worker) processRoom(ctx context.Context, session model.LiveRuntimeSession) error {
	claim, err := w.claim(ctx, session)
	if err != nil {
		return err
	}
	if !claim.Claimed || claim.Item == nil {
		return nil
	}
	item := claim.Item
	completed := false
	defer func() {
		if !completed {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := w.release(releaseCtx, session, item.ID); err != nil {
				log.Printf("decision executor release tenant=%d room=%d decision=%s: %v", session.TenantID, session.RoomID, item.ID, err)
			}
		}
	}()

	isSimulation := strings.EqualFold(strings.TrimSpace(item.ManualOrigin), "test_simulation")
	text, err := w.generateDecisionText(ctx, session, *item)
	if err != nil {
		return err
	}

	if isSimulation {
		planName := ""
		if plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID); planErr == nil {
			planName = strings.TrimSpace(plan.Name)
		}
		var userLayerVersion uint64
		if _, _, _, l3, layerErr := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID); layerErr == nil && l3 != nil {
			userLayerVersion = l3.VersionNo
		}
		executionMode := strings.TrimSpace(item.ExecutionMode)
		if executionMode == "" {
			executionMode = "intent"
		}
		if err := w.completeSimulation(ctx, session, *item, text, executionMode, planName, userLayerVersion); err != nil {
			return err
		}
		completed = true
		w.clearBackoff(session.RoomID)
		return nil
	}

	voice, ok, err := w.readyVoice(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("没有可用的默认声音，请先在声音中心选择可用音色")
	}

	ttsModel := voiceModel(voice)
	ttsCtx, ttsCancel := context.WithTimeout(ctx, 35*time.Second)
	defer ttsCancel()
	audio, err := w.tts.SynthesizeURL(ttsCtx, ttsgateway.SynthesizeRequest{
		Model:   ttsModel,
		VoiceID: voice.VoiceID,
		Text:    text,
		Rate:    voiceRate(voice),
	})
	if err != nil {
		return fmt.Errorf("TTS生成失败: %w", err)
	}
	if strings.TrimSpace(audio.AudioURL) == "" {
		return fmt.Errorf("TTS未返回音频地址")
	}

	action := "answer"
	if strings.EqualFold(strings.TrimSpace(item.ManualAction), "quick") {
		action = "quick"
	}
	if err := w.dispatch(ctx, session, *item, action, text, audio.AudioURL); err != nil {
		return err
	}
	sourceType := "interrupt_answer"
	if action == "quick" {
		sourceType = "interrupt_quick"
	}
	if historyErr := w.store.RecordGeneratedSpeechHistory(ctx, model.GeneratedSpeechHistoryInput{
		TenantID:          session.TenantID,
		RoomID:            session.RoomID,
		RuntimeSessionID:  session.ID,
		RuntimeExternalID: session.ExternalID,
		DecisionID:        item.ID,
		SourceType:        sourceType,
		QuestionText:      primaryQuestion(*item),
		GeneratedText:     text,
	}); historyErr != nil {
		// 播音已经成功下发，历史写入失败不能触发重试，否则会重复播音。
		log.Printf("decision executor history tenant=%d room=%d decision=%s: %v", session.TenantID, session.RoomID, item.ID, historyErr)
	}
	// Core 自己按 started_at + duration_ms 完成播音任务；终端回报只用于设备健康与排查。
	completed = true
	w.clearBackoff(session.RoomID)
	return nil
}

type SimulationOutput struct {
	Question         string `json:"question"`
	Reply            string `json:"reply"`
	ExecutionMode    string `json:"execution_mode"`
	PlanName         string `json:"plan_name,omitempty"`
	UserLayerVersion uint64 `json:"user_layer_version,omitempty"`
}

func (w *Worker) generateDecisionText(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	text := ""
	if strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
		// 100%原话只表示不主动改写；规则层仍然拥有最终播出否决权。
		text = strings.TrimSpace(item.FixedText)
		if text == "" {
			text = strings.TrimSpace(primaryQuestion(item))
		}
		if text == "" {
			return "", fmt.Errorf("100%%原话模式没有可播出的固定文字")
		}
	} else {
		prompt, err := w.answerPrompt(ctx, session, item)
		if err != nil {
			return "", err
		}
		answerCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		answerSystemPrompt := w.store.AgentPromptValue(ctx, "live.answer.system", "生成真实、自然、可直接播出的直播回答，不编造事实。")
		answer, err := w.agent.Complete(answerCtx, agentgateway.Request{
			Messages: []agentgateway.Message{
				{Role: "system", Content: answerSystemPrompt},
				{Role: "user", Content: prompt},
			},
			MaxTokens:      650,
			EnableThinking: false,
			Timeout:        20 * time.Second,
		})
		if err != nil {
			return "", fmt.Errorf("生成回答失败: %w", err)
		}
		text = strings.TrimSpace(answer.Text)
		if text == "" {
			return "", fmt.Errorf("生成回答为空")
		}
	}
	finalText, err := w.finalizeSpeechText(ctx, session, item, text)
	if err != nil {
		return "", err
	}
	text = limitSpeechText(finalText, maxSpeechRunes)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("最终播出文字为空")
	}
	return text, nil
}

func (w *Worker) SimulateAnswer(ctx context.Context, tenantID, roomID int64, question string) (SimulationOutput, error) {
	return w.SimulateAnswerWithPreview(ctx, tenantID, roomID, question, "", "", "", 0)
}

func (w *Worker) SimulateAnswerWithPreview(
	ctx context.Context,
	tenantID, roomID int64,
	question, previewInstruction, previewMemoryType, previewMemoryKey string,
	previewMatchedMemoryID int64,
) (SimulationOutput, error) {
	question = strings.TrimSpace(question)
	if tenantID <= 0 || roomID <= 0 || question == "" {
		return SimulationOutput{}, fmt.Errorf("测试问题不能为空")
	}
	if w == nil || w.store == nil || w.agent == nil {
		return SimulationOutput{}, fmt.Errorf("测试智能体未初始化")
	}
	session := model.LiveRuntimeSession{TenantID: tenantID, RoomID: roomID}
	item := decisionItem{
		Title:                  "测试模拟观众提问",
		Summary:                "测试模式模拟真实观众问题，只生成最终回答，不播音",
		SampleQuestions:        []string{question},
		ManualAction:           "answer",
		ManualOrigin:           "test_simulation",
		ExecutionMode:          "intent",
		PreviewInstruction:     strings.TrimSpace(previewInstruction),
		PreviewMemoryType:      strings.TrimSpace(previewMemoryType),
		PreviewMemoryKey:       strings.TrimSpace(previewMemoryKey),
		PreviewMatchedMemoryID: previewMatchedMemoryID,
	}
	text, err := w.generateDecisionText(ctx, session, item)
	if err != nil {
		return SimulationOutput{}, err
	}
	result := SimulationOutput{Question: question, Reply: text, ExecutionMode: "intent"}
	if plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, tenantID, roomID); planErr == nil {
		result.PlanName = strings.TrimSpace(plan.Name)
	}
	if _, _, _, l3, layerErr := w.store.LoadLivePolicyLayers(ctx, tenantID, roomID); layerErr == nil && l3 != nil {
		result.UserLayerVersion = l3.VersionNo
	}
	return result, nil
}

func (w *Worker) claim(ctx context.Context, session model.LiveRuntimeSession) (claimResponse, error) {
	query := tenantQuery(session.TenantID)
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/claim", session.RoomID),
		query, nil,
	)
	if err != nil {
		return claimResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return claimResponse{}, fmt.Errorf("claim http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var result claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return claimResponse{}, err
	}
	return result, nil
}

func (w *Worker) release(ctx context.Context, session model.LiveRuntimeSession, decisionID string) error {
	query := tenantQuery(session.TenantID)
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/release", session.RoomID, url.PathEscape(decisionID)),
		query, nil,
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("release http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (w *Worker) completeSimulation(
	ctx context.Context,
	session model.LiveRuntimeSession,
	item decisionItem,
	text, executionMode, planName string,
	userLayerVersion uint64,
) error {
	query := tenantQuery(session.TenantID)
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/simulation-complete", session.RoomID, url.PathEscape(item.ID)),
		query,
		map[string]any{
			"reply":              text,
			"execution_mode":     executionMode,
			"plan_name":          planName,
			"user_layer_version": userLayerVersion,
		},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("complete simulation http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (w *Worker) dispatch(
	ctx context.Context,
	session model.LiveRuntimeSession,
	item decisionItem,
	action, text, audioURL string,
) error {
	query := tenantQuery(session.TenantID)
	resp, err := w.core.DoRoom(
		ctx, session.TenantID, session.RoomID, http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/audio/interaction", session.RoomID),
		query,
		map[string]any{
			"decision_id": item.ID,
			"session_id":  session.ExternalID,
			"action":      action,
			"audio_url":   audioURL,
			"question":    primaryQuestion(item),
			"reply_text":  text,
			"topic":       item.Topic,
		},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("audio dispatch http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (w *Worker) readyVoice(ctx context.Context, tenantID, roomID int64) (model.VoiceProfile, bool, error) {
	versions, err := w.store.ListLiveAgentConfigVersions(ctx, tenantID)
	if err != nil {
		return model.VoiceProfile{}, false, err
	}
	selected := selectedRoomVoice(versions, roomID)
	if len(selected) > 0 {
		source := strings.ToLower(strings.TrimSpace(fmt.Sprint(selected["source"])))
		if source == "clone" {
			if profileID := mapInt64(selected["profile_id"]); profileID > 0 {
				profile, profileErr := w.store.GetVoiceProfile(ctx, tenantID, profileID)
				if profileErr == nil && strings.EqualFold(strings.TrimSpace(profile.CloneStatus), "ready") && strings.TrimSpace(profile.VoiceID) != "" {
					return profile, true, nil
				}
			}
		}
		if source == "official" {
			if voice, exists := voicecatalog.Find(strings.TrimSpace(fmt.Sprint(selected["voice_id"]))); exists {
				return officialVoiceProfile(voice), true, nil
			}
		}
	}
	voice := voicecatalog.Default()
	if strings.TrimSpace(voice.ID) == "" || strings.TrimSpace(voice.Model) == "" {
		return model.VoiceProfile{}, false, nil
	}
	return officialVoiceProfile(voice), true, nil
}

func selectedRoomVoice(versions []model.AgentConfigVersion, roomID int64) map[string]any {
	roomKey := strconv.FormatInt(roomID, 10)
	for _, version := range versions {
		if !strings.EqualFold(strings.TrimSpace(version.LifecycleStatus), "active") {
			continue
		}
		rooms, _ := version.SpeechConfig["rooms"].(map[string]any)
		room, _ := rooms[roomKey].(map[string]any)
		voice, _ := room["selected_voice"].(map[string]any)
		if len(voice) > 0 {
			return voice
		}
	}
	return nil
}

func mapInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func officialVoiceProfile(voice voicecatalog.Voice) model.VoiceProfile {
	return model.VoiceProfile{
		Name:        voice.Name,
		Provider:    "aliyun_qwen",
		VoiceID:     voice.ID,
		CloneStatus: "ready",
		IsDefault:   voice.ID == voicecatalog.DefaultVoiceID,
		Config: map[string]any{
			"source":       "system_official",
			"target_model": voice.Model,
			"rate":         1.0,
		},
	}
}

var quotedRiskTermPattern = regexp.MustCompile("[“\"「『]([^”\"」』]{1,24})[”\"」』]")

var builtInAbsoluteRiskTerms = []string{
	"绝对不踩雷",
	"百分百",
	"100%",
	"绝对",
	"保证",
	"包你",
	"肯定",
	"永远",
	"零风险",
	"全网最低",
	"全网第一",
	"第一名",
}

var unsupportedClaimPhrases = []string{
	"正规渠道",
	"品质保障",
	"严格筛选",
	"官方正品",
	"绝对正品",
}

var unsupportedShippingClaimPhrases = []string{
	"合作的主流快递",
	"主流快递",
	"系统匹配",
	"仓库实际发货",
	"48小时内",
	"四十八小时内",
}

func (w *Worker) finalizeSpeechText(
	ctx context.Context,
	session model.LiveRuntimeSession,
	item decisionItem,
	text string,
) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("最终播出文字为空")
	}

	industry, l1, l2, l3, err := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", fmt.Errorf("规则层终审读取失败: %w", err)
	}
	effective := policy.BuildEffective(industry, l1, l2, l3)
	runtimeInstruction := w.store.AgentPromptValue(ctx, "policy.runtime.execution", "")
	if strings.TrimSpace(runtimeInstruction) != "" {
		effective.PromptText = strings.TrimSpace(runtimeInstruction + "\n\n" + effective.PromptText)
	}
	contextText, _ := w.answerPrompt(ctx, session, item)
	riskTerms, reasons := finalSpeechRisks(text, contextText, effective)
	for _, term := range memoryWordingRiskTerms(contextText) {
		if term != "" && strings.Contains(text, term) {
			riskTerms = append(riskTerms, term)
			reasons = append(reasons, "当前直播间用词规范禁止表达："+term)
		}
	}
	riskTerms = uniqueNonEmptyStrings(riskTerms)
	reasons = uniqueNonEmptyStrings(reasons)
	hasAgentMemories := hasAgentMemoryPrompt(contextText)
	if len(reasons) == 0 && !hasAgentMemories {
		return text, nil
	}

	reviewCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	reviewSystemPrompt := w.store.AgentPromptValue(ctx, "live.final_review.system", "只返回修正后的可直接播出正文，删除无依据事实和绝对化承诺。")
	if hasAgentMemories {
		reviewSystemPrompt += "\n\n【智能体记忆终审硬要求】\n当前直播间智能体记忆都是用户已经采用、正在生效的约束。必须逐条核对与当前问题相关的记忆，不能遗漏：\n- semantic：必须结合当前问题、商品和上下文判断含义，禁止无条件字符串替换；\n- fact：不得与已确认事实冲突，不得把未知事实补成确定事实；\n- wording：禁止使用 avoid/blocked 类表达，并优先采用已确认的表达口径；\n- style：只能改变说话方式，不能改变事实。\n若待播话术已经正确，保持原意和自然度；若存在冲突，直接修正。只返回最终可播正文。"
	}
	if strings.TrimSpace(item.PreviewInstruction) != "" {
		reviewSystemPrompt += "\n\n【候选修正预览测试】\n本次正在测试尚未采用的候选修正。候选修正与同一用户层记忆直接冲突时，以候选修正为准；规则层、行业层和其他不冲突的已采用记忆继续执行。只评估本次回答，不得声称候选已经采用、发布或保存。"
	}
	review, reviewErr := w.agent.Complete(reviewCtx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{
				Role:    "system",
				Content: reviewSystemPrompt,
			},
			{
				Role: "user",
				Content: "【当前有效规则】\n" + effective.PromptText +
					"\n\n【事实与现场上下文】\n" + contextText +
					"\n\n【待播话术】\n" + text +
					"\n\n【已检出风险】\n" + finalReviewReasonText(reasons, hasAgentMemories),
			},
		},
		MaxTokens:      650,
		EnableThinking: false,
		Timeout:        16 * time.Second,
	})
	candidate := text
	if reviewErr != nil && hasAgentMemories {
		return "", fmt.Errorf("智能体记忆终审失败: %w", reviewErr)
	}
	if reviewErr == nil && strings.TrimSpace(review.Text) != "" {
		candidate = strings.TrimSpace(review.Text)
	} else if hasAgentMemories {
		return "", fmt.Errorf("智能体记忆终审未返回可播文字")
	}

	// 终审后再做一次确定性的硬过滤，确保已知绝对化词和用词禁词不能进入 TTS。
	candidate = hardSanitizeSpeech(candidate, riskTerms, contextText)
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("规则层终审后没有可播出文字")
	}
	return strings.TrimSpace(candidate), nil
}

func hasAgentMemoryPrompt(contextText string) bool {
	return strings.Contains(contextText, "【当前直播间智能体记忆】")
}

func finalReviewReasonText(reasons []string, hasAgentMemories bool) string {
	if len(reasons) > 0 {
		return strings.Join(reasons, "；")
	}
	if hasAgentMemories {
		return "未检出显式关键词风险；仍必须逐条核对当前直播间已采用的智能体记忆，确认无遗漏后再输出。"
	}
	return "无"
}

func finalSpeechRisks(
	text string,
	contextText string,
	effective model.LiveEffectivePolicy,
) ([]string, []string) {
	terms := append([]string(nil), builtInAbsoluteRiskTerms...)
	terms = append(terms, configuredL1RiskTerms(effective)...)
	terms = uniqueNonEmptyStrings(terms)

	reasons := make([]string, 0)
	for _, term := range terms {
		if term != "" && strings.Contains(text, term) {
			reasons = append(reasons, "绝对化或规则层高风险表达："+term)
		}
	}
	for _, phrase := range unsupportedClaimPhrases {
		if !strings.Contains(text, phrase) {
			continue
		}
		if strings.Contains(contextText, phrase) {
			continue
		}
		reasons = append(reasons, "缺少事实依据的承诺或事实："+phrase)
		terms = append(terms, phrase)
	}
	for _, phrase := range unsupportedShippingClaimPhrases {
		if !strings.Contains(text, phrase) {
			continue
		}
		if strings.Contains(contextText, phrase) {
			continue
		}
		reasons = append(reasons, "直播方案/策略中没有依据的物流事实："+phrase)
		terms = append(terms, phrase)
	}
	return uniqueNonEmptyStrings(terms), uniqueNonEmptyStrings(reasons)
}

func memoryWordingRiskTerms(contextText string) []string {
	const marker = "【用词硬校验词】"
	result := make([]string, 0)
	for _, line := range strings.Split(contextText, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, marker) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, marker))
		for _, term := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '｜' || r == '|' || r == ',' || r == '，' || r == '、' || r == ';' || r == '；'
		}) {
			if term = strings.TrimSpace(term); term != "" {
				result = append(result, term)
			}
		}
	}
	return uniqueNonEmptyStrings(result)
}

func configuredL1RiskTerms(effective model.LiveEffectivePolicy) []string {
	result := make([]string, 0)
	for _, rule := range effective.Rules {
		if rule.SourceLayer != model.LivePolicyLayerL1 {
			continue
		}
		for _, key := range []string{"forbidden_terms", "blocked_terms", "risk_terms", "absolute_terms"} {
			value, ok := rule.Metadata[key]
			if !ok {
				continue
			}
			switch typed := value.(type) {
			case []string:
				result = append(result, typed...)
			case []any:
				for _, item := range typed {
					if term := strings.TrimSpace(fmt.Sprint(item)); term != "" {
						result = append(result, term)
					}
				}
			case string:
				for _, term := range strings.FieldsFunc(typed, func(r rune) bool {
					return r == ',' || r == '，' || r == '、' || r == ';' || r == '；'
				}) {
					if term = strings.TrimSpace(term); term != "" {
						result = append(result, term)
					}
				}
			}
		}
		ruleText := strings.TrimSpace(rule.Text)
		if ruleText == "" {
			continue
		}
		if !strings.Contains(ruleText, "避免") &&
			!strings.Contains(ruleText, "不得") &&
			!strings.Contains(ruleText, "不要") &&
			!strings.Contains(ruleText, "禁止") &&
			!strings.Contains(ruleText, "绝对") &&
			!strings.Contains(ruleText, "极限词") {
			continue
		}
		for _, match := range quotedRiskTermPattern.FindAllStringSubmatch(ruleText, -1) {
			if len(match) > 1 {
				if term := strings.TrimSpace(match[1]); term != "" {
					result = append(result, term)
				}
			}
		}
	}
	return uniqueNonEmptyStrings(result)
}

func hardSanitizeSpeech(text string, riskTerms []string, contextText string) string {
	text = strings.TrimSpace(text)
	replacements := []struct {
		old string
		new string
	}{
		{"绝对不踩雷", "更值得放心看看"},
		{"绝对不会", "一般不会"},
		{"绝对没有", "目前没有发现"},
		{"百分百", "尽量做到"},
		{"100%", "尽量做到"},
		{"零风险", "尽量降低风险"},
		{"全网最低", "直播间当前价格"},
		{"全网第一", "很受大家关注"},
		{"第一名", "表现很不错"},
		{"包你满意", "希望能让你满意"},
		{"包你好吃", "喜欢这口的可以试试"},
		{"保证", "尽量做到"},
		{"肯定", "更可能"},
		{"永远", "长期"},
		{"绝对", ""},
	}
	for _, item := range replacements {
		text = strings.ReplaceAll(text, item.old, item.new)
	}
	for _, phrase := range unsupportedClaimPhrases {
		if strings.Contains(contextText, phrase) {
			continue
		}
		switch phrase {
		case "正规渠道":
			text = strings.ReplaceAll(text, phrase, "直播间里给大家介绍的")
		case "品质保障":
			text = strings.ReplaceAll(text, phrase, "选品会认真把关")
		case "严格筛选":
			text = strings.ReplaceAll(text, phrase, "认真挑选")
		case "官方正品", "绝对正品":
			text = strings.ReplaceAll(text, phrase, "页面展示的商品")
		}
	}
	for _, term := range riskTerms {
		term = strings.TrimSpace(term)
		if term == "" || !strings.Contains(text, term) {
			continue
		}
		text = strings.ReplaceAll(text, term, "")
	}
	for strings.Contains(text, "，，") {
		text = strings.ReplaceAll(text, "，，", "，")
	}
	for strings.Contains(text, "。。") {
		text = strings.ReplaceAll(text, "。。", "。")
	}
	return strings.TrimSpace(strings.Trim(text, "，,。 "))
}

func limitSpeechText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	cut := maxRunes
	searchStart := maxRunes - 80
	if searchStart < 0 {
		searchStart = 0
	}
	for index := maxRunes - 1; index >= searchStart; index-- {
		switch runes[index] {
		case '。', '！', '？', '；', '!', '?', ';':
			cut = index + 1
			index = -1
		}
	}
	result := strings.TrimSpace(string(runes[:cut]))
	if result == "" {
		return ""
	}
	last := []rune(result)
	if len(last) > 0 {
		switch last[len(last)-1] {
		case '。', '！', '？', '!', '?':
			return result
		}
	}
	return strings.TrimRight(result, "，、；;：:, ") + "。"
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func liveAgentPlanPromptContext(plan model.LiveAgentPlan) string {
	if plan.ID <= 0 {
		return "\n【当前智能体直播方案】\n当前直播间未配置可用直播方案。遇到物流、价格、库存、发货时效等事实问题，不得按行业常识猜测；没有事实依据时只做不确定表达。"
	}

	var builder strings.Builder
	builder.WriteString("\n【当前智能体直播方案：事实与回答口径，优先执行】")
	builder.WriteString("\n方案名称：")
	builder.WriteString(strings.TrimSpace(plan.Name))
	if description := strings.TrimSpace(plan.Description); description != "" {
		builder.WriteString("\n方案说明：")
		builder.WriteString(description)
	}
	builder.WriteString("\n执行要求：观众问题如果能从本方案得到明确答案，必须直接按本方案的事实和口径回答；不得把明确方案改写成‘默认/一般/通常/主流快递/系统匹配’之类通用兜底，也不得增加方案没有写明的快递公司、发货时效、仓库流程、价格、库存或承诺。")

	termCount := 0
	for _, term := range plan.Terms {
		if termCount >= 30 || !strings.EqualFold(strings.TrimSpace(term.Status), "active") {
			continue
		}
		canonical := strings.TrimSpace(term.CanonicalText)
		note := strings.TrimSpace(term.Note)
		if canonical == "" && note == "" {
			continue
		}
		termCount++
		builder.WriteString("\n- 方案条目")
		if termType := strings.TrimSpace(term.TermType); termType != "" {
			builder.WriteString("[")
			builder.WriteString(termType)
			builder.WriteString("]")
		}
		builder.WriteString("：")
		builder.WriteString(canonical)
		if note != "" {
			builder.WriteString("；说明：")
			builder.WriteString(note)
		}
		variants := make([]string, 0, 3)
		for _, variant := range term.Variants {
			if len(variants) >= 3 {
				break
			}
			if value := strings.TrimSpace(variant.VariantText); value != "" {
				variants = append(variants, value)
			}
		}
		if len(variants) > 0 {
			builder.WriteString("；已确认表达：")
			builder.WriteString(strings.Join(variants, "、"))
		}
	}
	return builder.String()
}

func (w *Worker) answerPrompt(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	plan, planErr := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if planErr != nil && !errors.Is(planErr, appdb.ErrLiveAgentPlanNotFound) {
		return "", fmt.Errorf("读取智能体直播方案失败: %w", planErr)
	}
	industry, l1, l2, l3, err := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", fmt.Errorf("读取直播策略失败: %w", err)
	}
	effective := policy.BuildEffective(industry, l1, l2, l3)
	runtimeInstruction := w.store.AgentPromptValue(ctx, "policy.runtime.execution", "")
	if strings.TrimSpace(runtimeInstruction) != "" {
		effective.PromptText = strings.TrimSpace(runtimeInstruction + "\n\n" + effective.PromptText)
	}
	memories, memoryErr := w.store.ListActiveAgentMemories(ctx, session.TenantID, session.RoomID)
	if memoryErr != nil {
		return "", fmt.Errorf("读取智能体记忆失败: %w", memoryErr)
	}
	if strings.TrimSpace(item.PreviewInstruction) != "" {
		filtered := memories[:0]
		for _, memory := range memories {
			if item.PreviewMatchedMemoryID > 0 && memory.ID == item.PreviewMatchedMemoryID {
				continue
			}
			if item.PreviewMemoryKey != "" && strings.EqualFold(strings.TrimSpace(memory.MemoryKey), strings.TrimSpace(item.PreviewMemoryKey)) &&
				(item.PreviewMemoryType == "" || strings.EqualFold(strings.TrimSpace(memory.MemoryType), strings.TrimSpace(item.PreviewMemoryType))) {
				continue
			}
			filtered = append(filtered, memory)
		}
		memories = filtered
	}
	memoryPrompt := agentmemory.Prompt(memories)
	rulesJSON, _ := json.Marshal(effective.Rules)
	planContext := liveAgentPlanPromptContext(plan)
	questions := item.SampleQuestions
	if len(questions) > 8 {
		questions = questions[:8]
	}
	if len(questions) == 0 {
		questions = []string{primaryQuestion(item)}
	}
	variables := map[string]string{
		"question":             strings.Join(questions, "；"),
		"room_name":            fmt.Sprintf("room-%d", session.RoomID),
		"plan_name":            strings.TrimSpace(plan.Name),
		"industry_policy":      industry,
		"user_policy":          string(rulesJSON),
		"reference_answer":     strings.TrimSpace(item.ReplyHint),
		"conversation_history": strings.TrimSpace(item.Summary),
	}
	if strings.EqualFold(strings.TrimSpace(item.ManualOrigin), "agent_input") {
		requirement := w.store.RenderAgentPrompt(ctx, "live.answer.operator", "按操作者指令生成可直接播出的口播正文。", variables)
		return planContext +
			"\n当前直播策略规则：" + string(rulesJSON) +
			"\n当前直播间智能体记忆：" + memoryPrompt +
			"\n操作者指令：" + strings.Join(questions, "；") +
			"\n任务摘要：" + strings.TrimSpace(item.Summary) +
			"\n额外要求：" + strings.TrimSpace(item.ReplyHint) +
			"\n当前场景生成要求：" + requirement, nil
	}
	requirement := w.store.RenderAgentPrompt(ctx, "live.answer.audience", "根据当前方案和策略回答观众问题。", variables)
	previewPrompt := ""
	if strings.EqualFold(strings.TrimSpace(item.ManualOrigin), "test_simulation") {
		testRequirement := w.store.RenderAgentPrompt(ctx, "test.simulation.answer", "测试模式只返回模拟回答，不执行真实播音。", variables)
		requirement = strings.TrimSpace(requirement + "\n" + testRequirement)
		if preview := strings.TrimSpace(item.PreviewInstruction); preview != "" {
			previewPrompt = "\n【本次候选修正预览】\n" + preview +
				"\n这是尚未采用的候选修正，仅本次测试临时生效。若它与同一用户记忆发生直接冲突，以本候选为准；其他不冲突的已采用记忆继续执行。不得声称已经采用或保存。"
		}
	}
	return planContext +
		"\n当前直播策略规则：" + string(rulesJSON) +
		"\n当前直播间智能体记忆：" + memoryPrompt + previewPrompt +
		"\n当前问题类别：" + strings.TrimSpace(item.Title) +
		"\n观众原话：" + strings.Join(questions, "；") +
		"\n任务摘要：" + strings.TrimSpace(item.Summary) +
		"\n回答提示：" + strings.TrimSpace(item.ReplyHint) +
		"\n当前场景生成要求：" + requirement, nil
}

func primaryQuestion(item decisionItem) string {
	for _, question := range item.SampleQuestions {
		if value := strings.TrimSpace(question); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(item.Summary); value != "" {
		return value
	}
	return strings.TrimSpace(item.Title)
}

func voiceModel(profile model.VoiceProfile) string {
	if profile.Config != nil {
		if raw, ok := profile.Config["target_model"]; ok {
			if value := strings.TrimSpace(fmt.Sprint(raw)); value != "" {
				return value
			}
		}
	}
	return ""
}

func voiceRate(profile model.VoiceProfile) float64 {
	if profile.Config == nil {
		return 1
	}
	raw, ok := profile.Config["rate"]
	if !ok {
		return 1
	}
	switch value := raw.(type) {
	case float64:
		if value >= 0.5 && value <= 2 {
			return value
		}
	case float32:
		rate := float64(value)
		if rate >= 0.5 && rate <= 2 {
			return rate
		}
	case int:
		rate := float64(value)
		if rate >= 0.5 && rate <= 2 {
			return rate
		}
	}
	return 1
}

func tenantQuery(tenantID int64) url.Values {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	return query
}

func (w *Worker) beginRoom(roomID int64) bool {
	if roomID <= 0 {
		return false
	}
	now := w.now()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inFlight[roomID] || now.Before(w.retryAfter[roomID]) {
		return false
	}
	w.inFlight[roomID] = true
	return true
}

func (w *Worker) endRoom(roomID int64) {
	w.mu.Lock()
	delete(w.inFlight, roomID)
	w.mu.Unlock()
}

func (w *Worker) backoffRoom(roomID int64) {
	w.mu.Lock()
	w.retryAfter[roomID] = w.now().Add(errorBackoff)
	w.mu.Unlock()
}

func (w *Worker) clearBackoff(roomID int64) {
	w.mu.Lock()
	delete(w.retryAfter, roomID)
	w.mu.Unlock()
}
