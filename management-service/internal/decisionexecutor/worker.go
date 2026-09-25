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
	ID              string   `json:"id"`
	Topic           string   `json:"topic"`
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	ReplyHint       string   `json:"reply_hint"`
	SampleQuestions []string `json:"sample_questions"`
	ManualAction    string   `json:"manual_action"`
	ManualOrigin    string   `json:"manual_origin"`
	ExecutionMode   string   `json:"execution_mode"`
	FixedText       string   `json:"fixed_text"`
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

	voice, ok, err := w.readyVoice(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("没有可用的默认声音，请先在声音中心选择可用音色")
	}

	text := ""
	if strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
		// 100%原话只表示不主动改写；规则层仍然拥有最终播出否决权。
		text = strings.TrimSpace(item.FixedText)
		if text == "" {
			text = strings.TrimSpace(primaryQuestion(*item))
		}
		if text == "" {
			return fmt.Errorf("100%%原话模式没有可播出的固定文字")
		}
	} else {
		prompt, err := w.answerPrompt(ctx, session, *item)
		if err != nil {
			return err
		}
		answerCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		answer, err := w.agent.Complete(answerCtx, agentgateway.Request{
			Messages: []agentgateway.Message{
				{Role: "system", Content: "你是直播间实时口播回答生成器。严格遵守规则层、行业层、用户层策略；只说可直接播出的正文，不输出解释、标题、Markdown或JSON。不得编造事实。最终正文最多300个中文字符。"},
				{Role: "user", Content: prompt},
			},
			MaxTokens:      650,
			EnableThinking: false,
			Timeout:        20 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("生成回答失败: %w", err)
		}
		text = strings.TrimSpace(answer.Text)
		if text == "" {
			return fmt.Errorf("生成回答为空")
		}
	}

	finalText, err := w.finalizeSpeechText(ctx, session, *item, text)
	if err != nil {
		return err
	}
	text = limitSpeechText(finalText, maxSpeechRunes)
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("最终播出文字为空")
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
	// 播音回调负责把 decision 标成 completed；这里成功只表示已交给分发层。
	completed = true
	w.clearBackoff(session.RoomID)
	return nil
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
	contextText, _ := w.answerPrompt(ctx, session, item)
	riskTerms, reasons := finalSpeechRisks(text, contextText, effective)
	if len(reasons) == 0 {
		return text, nil
	}

	reviewCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	review, reviewErr := w.agent.Complete(reviewCtx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{
				Role:    "system",
				Content: "你是直播口播的规则层最终闸门。规则层优先级最高。只输出修正后的可直接播出正文，不解释、不列规则、不输出Markdown或JSON。删除绝对化承诺、无依据事实和无法从上下文确认的保证；保留原意、语气和销售推进能力，改成自然好听的可播表达。",
			},
			{
				Role: "user",
				Content: "【当前有效规则】\n" + effective.PromptText +
					"\n\n【事实与现场上下文】\n" + contextText +
					"\n\n【待播话术】\n" + text +
					"\n\n【已检出风险】\n" + strings.Join(reasons, "；") +
					"\n\n请只返回终审后的直播口播正文。",
			},
		},
		MaxTokens:      650,
		EnableThinking: false,
		Timeout:        16 * time.Second,
	})
	candidate := text
	if reviewErr == nil && strings.TrimSpace(review.Text) != "" {
		candidate = strings.TrimSpace(review.Text)
	}

	// 无论终审模型是否可用，都再做一次确定性的硬过滤，确保已知绝对化词不能进入TTS。
	candidate = hardSanitizeSpeech(candidate, riskTerms, contextText)
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("规则层终审后没有可播出文字")
	}
	return strings.TrimSpace(candidate), nil
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
	return uniqueNonEmptyStrings(terms), uniqueNonEmptyStrings(reasons)
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
	rulesJSON, _ := json.Marshal(effective.Rules)
	termValues := make([]string, 0, len(plan.Terms))
	for _, term := range plan.Terms {
		if len(termValues) >= 30 {
			break
		}
		if value := strings.TrimSpace(term.CanonicalText); value != "" {
			termValues = append(termValues, value)
		}
	}
	planContext := ""
	if plan.ID > 0 {
		planContext = "\n当前智能体直播方案：\n方案名称：" + strings.TrimSpace(plan.Name)
		if description := strings.TrimSpace(plan.Description); description != "" {
			planContext += "\n方案说明：" + description
		}
		if len(termValues) > 0 {
			planContext += "\n方案专用词：" + strings.Join(termValues, "、")
		}
	}
	questions := item.SampleQuestions
	if len(questions) > 8 {
		questions = questions[:8]
	}
	if len(questions) == 0 {
		questions = []string{primaryQuestion(item)}
	}
	if strings.EqualFold(strings.TrimSpace(item.ManualOrigin), "agent_input") {
		return planContext +
			"\n当前直播策略规则：" + string(rulesJSON) +
			"\n这是直播操作者通过智能体输入框发出的现场口播指令，不是观众提问。" +
			"\n操作者指令：" + strings.Join(questions, "；") +
			"\n任务摘要：" + strings.TrimSpace(item.Summary) +
			"\n额外要求：" + strings.TrimSpace(item.ReplyHint) +
			"\n请严格理解操作者限定词和语气要求，在不编造事实的前提下生成可直接播出的自然中文口播。只输出最终口播正文。", nil
	}
	return planContext +
		"\n当前直播策略规则：" + string(rulesJSON) +
		"\n当前问题类别：" + strings.TrimSpace(item.Title) +
		"\n观众原话：" + strings.Join(questions, "；") +
		"\n任务摘要：" + strings.TrimSpace(item.Summary) +
		"\n回答提示：" + strings.TrimSpace(item.ReplyHint) +
		"\n请生成约10到20秒的自然中文直播口播。能直接回答就热情回答；需要纠偏就给可播替代说法。不要补充策略和观众原话中没有依据的具体价格、库存、时间、功效或承诺。只输出最终口播正文。", nil
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
