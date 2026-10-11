package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/agentstudio"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type cycleCancel struct {
	tenant, room int64
	run          string
	cancel       context.CancelFunc
}
type cycleCancels struct {
	sync.Mutex
	entries map[string]cycleCancel
}

func (s *Server) SetAnchorCycleRepository(repository anchorcycle.Repository) {
	s.anchorCycles = repository
}
func (s *Server) cyclePersistenceTimeout() time.Duration {
	if repository, ok := s.anchorCycles.(interface{ PersistenceTimeout() time.Duration }); ok {
		return repository.PersistenceTimeout()
	}
	return 10 * time.Second
}
func (s *Server) cycleContext(ctx context.Context, key string, tenant, room int64, run string) (context.Context, func()) {
	// A page refresh must not discard a generation already sent upstream. The
	// server collects this one attempt; only an explicit end or timeout cancels it.
	return s.cycleJobContext(ctx, key, tenant, room, run, true)
}
func (s *Server) cycleJobContext(ctx context.Context, key string, tenant, room int64, run string, detach bool) (context.Context, func()) {
	if detach {
		ctx = context.WithoutCancel(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	s.cycleCancels.Lock()
	if s.cycleCancels.entries == nil {
		s.cycleCancels.entries = map[string]cycleCancel{}
	}
	s.cycleCancels.entries[key] = cycleCancel{tenant, room, run, cancel}
	s.cycleCancels.Unlock()
	return ctx, func() { cancel(); s.cycleCancels.Lock(); delete(s.cycleCancels.entries, key); s.cycleCancels.Unlock() }
}
func (s *Server) endAnchorCyclesForRoom(ctx context.Context, tenant, room int64) error {
	if s.anchorCycles == nil {
		return nil
	}
	states, listErr := s.anchorCycles.RoomStates(ctx, tenant, room)
	s.cycleCancels.Lock()
	for _, entry := range s.cycleCancels.entries {
		if entry.tenant == tenant && entry.room == room {
			entry.cancel()
		}
	}
	s.cycleCancels.Unlock()
	if err := s.anchorCycles.EndRoom(ctx, tenant, room); err != nil {
		return err
	}
	for _, state := range states {
		if err := s.interruptAnchorCycle(ctx, state); err != nil {
			log.Printf("cycle upstream interrupt room=%d failed: %v", room, err)
		}
	}
	return listErr
}

func (s *Server) interruptAnchorCycle(ctx context.Context, state *anchorcycle.State) error {
	if state == nil || state.Config.TextProfileID != "" || (!state.Busy && !state.Uncertain) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	profile, err := s.store.ResolveSpeechModel(ctx, state.Config.CredentialID)
	if err != nil {
		return err
	}
	if profile == nil || profile.APIKey == "" {
		return errors.New("结束本场时阿里凭据不可用")
	}
	client, err := agentstudio.New(state.Config.WorkspaceID, state.Config.Region, profile.APIKey, 15*time.Second)
	if err != nil {
		return err
	}
	return client.InterruptSession(ctx, state.SessionID)
}
func (s *Server) anchorCycleScope(w http.ResponseWriter, r *http.Request, actor model.Actor) (int64, int64, int64, bool) {
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return 0, 0, 0, false
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return 0, 0, 0, false
	}
	room := int64(0)
	if raw := r.URL.Query().Get("room_id"); raw != "" && !productionPreviewRequest(r) {
		var err error
		room, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || room < 0 {
			writeError(w, 400, "直播间ID无效")
			return 0, 0, 0, false
		}
	}
	if _, _, _, _, ok = s.anchorStyleWorkbenchPlanData(w, r, tenant, plan); !ok {
		return 0, 0, 0, false
	}
	if room > 0 {
		if !s.unlimitedGenerationPreview(r.Context(), actor) {
			writeError(w, 403, "测试接口不连接正式房间，请使用实时声音试听页")
			return 0, 0, 0, false
		}
		if actor.IsInternalStaff() {
			authorizedRoom, valid := liveSupportScopeRoomID(r)
			if !valid || authorizedRoom != room {
				writeError(w, 403, "运行房间须与当前授权房间一致")
				return 0, 0, 0, false
			}
		}
		bound, err := s.store.GetLiveAgentPlanForRoom(r.Context(), tenant, room)
		if err != nil || bound.ID != plan {
			writeError(w, 403, "直播间不属于当前方案")
			return 0, 0, 0, false
		}
	}
	if s.anchorCycles == nil {
		writeError(w, 503, "Redis大循环未配置")
		return 0, 0, 0, false
	}
	return tenant, plan, room, true
}
func cycleReply(w http.ResponseWriter, r *http.Request, state *anchorcycle.State) {
	writeJSON(w, 200, map[string]any{"run": cyclePreviewProjectionForRequest(r, state)})
}
func cycleError(w http.ResponseWriter, err error) {
	code := 503
	if errors.Is(err, anchorcycle.ErrMissing) || errors.Is(err, anchorcycle.ErrStale) || errors.Is(err, anchorcycle.ErrBusy) {
		code = 409
	}
	writeError(w, code, err.Error())
}
func (s *Server) anchorStyleCycleGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	state, err := s.anchorCycles.Get(r.Context(), tenant, plan, room)
	if errors.Is(err, anchorcycle.ErrMissing) {
		cycleReply(w, r, nil)
		return
	}
	if err != nil {
		cycleError(w, err)
		return
	}
	if !s.requireCycleRunAccess(w, r, actor, state) {
		return
	}
	if productionPreviewRequest(r) {
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, 200, map[string]any{"run": cyclePreviewProjectionForRequest(r, state), "playback": s.previewPlaybackSnapshot(state)})
		return
	}
	cycleReply(w, r, state)
}
func (s *Server) anchorStyleCycleStart(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	var input struct {
		anchorcycle.Config
		TaskLimit         int             `json:"task_limit"`
		VoiceSourceRoomID int64           `json:"voice_source_room_id,omitempty"`
		GenerationOptions json.RawMessage `json:"generation_options,omitempty"`
		StreamVersion     int             `json:"stream_version,omitempty"`
	}
	if readJSON(w, r, &input) != nil || input.TaskLimit < 0 || input.TaskLimit > 500 {
		writeError(w, 400, "大循环参数无效")
		return
	}
	if productionPreviewRequest(r) {
		if input.VoiceSourceRoomID > 0 && !s.requireLiveStrategyRoomAccess(w, r, actor, tenant, input.VoiceSourceRoomID) {
			return
		}
		w, r = cycleProgressStream(w, r)
		settings, err := s.store.AnchorGenerationSettings(r.Context())
		if err != nil || settings.Validate() != nil {
			writeError(w, 409, "请先在后台完成主播生成与阿里会话配置")
			return
		}
		options, err := parseRealtimeOptions(input.GenerationOptions, realtimeOptionsDefaults(settings))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		voice, err := s.planSelectedVoice(r.Context(), tenant, plan, input.VoiceSourceRoomID)
		if err != nil {
			writeError(w, 409, err.Error())
			return
		}
		input.Config = settings.Cycle
		input.Config.TTS = &voice
		input.Config = applyRealtimeOptions(input.Config, options)
		// A customer preview is duration-driven. Continue rolling through fresh
		// phrasings until its server-owned 2-minute audio budget is actually full.
		input.Config.Rolling = true
		input.Config.MaxCycles = 0
		input.TaskLimit = 0
	}
	connections, connectionErr := s.store.SpeechModels(r.Context())
	if connectionErr != nil {
		writeError(w, 503, "读取统一模型配置失败")
		return
	}
	if connections.SystemDefaultID != "" {
		input.TextProfileID = connections.DefaultID
	}
	if input.TextProfileID != "" {
		input.FeedbackID = input.TextProfileID
	}
	if input.TextProfileID == "" && (strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.AgentID) == "" || strings.TrimSpace(input.CredentialID) == "" || strings.TrimSpace(input.FeedbackID) == "") {
		writeError(w, 400, "必须明确选择阿里母版、凭据和回灌模型，不会自动换模型")
		return
	}
	if input.Region == "" {
		input.Region = "cn-beijing"
	}
	meta, wheel, tasks, err := s.confirmedAnchorStyleAgentStudioAssets(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	card, err := s.store.GetActiveLiveAnchorStyleScorecard(r.Context(), tenant, plan)
	if err != nil || card.Status != "confirmed" || card.MetaPromptAssetID != meta.ID {
		writeError(w, 409, "请确认当前主播评分基准")
		return
	}
	if _, err = anchorstylescore.Parse(card.EffectiveScorecardJSON()); err != nil {
		writeError(w, 409, "评分基准无效")
		return
	}
	reportCycleProgress(r.Context(), 25)
	queue := []anchorcycle.Task{}
	for _, t := range tasks {
		queue = append(queue, anchorcycle.Task{SellingPoint: t.SellingPoint, Angle: t.Angle, Key: t.Key, TargetAudience: t.TargetAudience, PainPoint: t.PainPoint})
	}
	if input.TaskLimit > 0 && input.TaskLimit < len(queue) {
		queue = queue[:input.TaskLimit]
	}
	state, err := anchorcycle.New(queue, input.Config)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if productionPreviewRequest(r) {
		state.Surface = "generation_preview"
		state.StreamControlled = input.StreamVersion == 1
	}
	if room == 0 && !s.unlimitedGenerationPreview(r.Context(), actor) {
		state.PreviewLimitMS = 120000
	}
	if state.Config.TTS != nil {
		voice := state.Config.TTS
		if voice.Rate == 0 {
			voice.Rate = 1
		}
		if voice.BufferSeconds == 0 {
			voice.BufferSeconds = 120
		}
		retention, retentionErr := s.anchorVoiceCacheTTL(r.Context())
		if retentionErr != nil {
			writeError(w, 503, "读取声音缓存运维配置失败")
			return
		}
		voice.CacheTTLSeconds = int(retention.Seconds()) // Ignore client overrides.
		if voice.Rate < 0.5 || voice.Rate > 2 || voice.BufferSeconds < 20 || voice.BufferSeconds > 600 {
			writeError(w, 400, "TTS语速须为0.5～2，缓存时长须为20～600秒")
			return
		}
		if actor.IsInternalStaff() && voice.Source == "clone" && !s.requireLiveSupportSelectedVoice(w, r, actor, tenant, map[string]any{"profile_id": voice.ProfileID, "voice_id": voice.VoiceID}) {
			return
		}
		voiceID, modelName, e := s.resolveFormalVoice(r.Context(), tenant, fullShowVariantVoiceInput{Source: voice.Source, VoiceID: voice.VoiceID, ProfileID: voice.ProfileID, Model: voice.Model})
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
		if s.assetStorage == nil {
			writeError(w, 503, "媒体存储未配置，不能生成完整声音资产包")
			return
		}
		if _, e = s.cycleCacheOSS(); e != nil || s.anchorVoiceCache == nil {
			writeError(w, 503, "请配置OSS和声音复用缓存，生成声音不会存到本地")
			return
		}
		voice.VoiceID, voice.Model = voiceID, modelName
	}
	generatorID := input.CredentialID
	if input.TextProfileID != "" {
		generatorID = input.TextProfileID
	}
	profile, err := s.store.ResolveSpeechModel(r.Context(), generatorID)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		writeError(w, 400, "请选择可用的阿里API Key配置")
		return
	}
	feedback, err := s.store.ResolveSpeechModel(r.Context(), input.FeedbackID)
	if err != nil || feedback == nil || !feedback.Enabled || feedback.APIKey == "" {
		writeError(w, 400, "请选择可用的回灌模型")
		return
	}
	var client *agentstudio.Client
	if input.TextProfileID == "" {
		client, err = agentstudio.New(input.WorkspaceID, input.Region, profile.APIKey, agentStudioTimeout(profile.TimeoutMS))
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	token := anchorcycle.ID()
	reportCycleProgress(r.Context(), 45)
	if err = s.anchorCycles.Acquire(r.Context(), tenant, plan, room, token); err != nil {
		cycleError(w, err)
		return
	}
	defer s.anchorCycles.Release(context.WithoutCancel(r.Context()), tenant, plan, room, token)
	if _, err = s.anchorCycles.Get(r.Context(), tenant, plan, room); err == nil {
		writeError(w, 409, "已有本场大循环，请继续或先结束本场")
		return
	} else if !errors.Is(err, anchorcycle.ErrMissing) {
		cycleError(w, err)
		return
	}
	ctx, done := s.cycleContext(r.Context(), token, tenant, room, state.RunID)
	defer done()
	env := input.EnvironmentID
	if input.TextProfileID == "" && env == "" {
		environments, e := client.ListEnvironments(ctx)
		if e != nil || len(environments) == 0 {
			writeError(w, 400, "没有可用的阿里环境")
			return
		}
		env = environments[0].ID
	}
	state.StyleMD = meta.EffectiveMetaPromptMD()
	reportCycleProgress(ctx, 60)
	state.BaselineJSON = card.EffectiveScorecardJSON()
	planData, facts, benefits, links, ok := s.anchorStyleWorkbenchPlanData(w, r.WithContext(ctx), tenant, plan)
	if !ok {
		return
	}
	state.Facts = renderAnchorStyleWorkbenchFacts(planData, facts, benefits, links)
	if productionPreviewRequest(r) {
		safety, e := s.prepareDerivedSafety(ctx, tenant, plan, actor.UserID)
		if e != nil {
			writeError(w, 409, e.Error())
			return
		}
		var safeWheel anchorStyleSellingPointWheel
		if json.Unmarshal([]byte(safety.SafeWheelJSON), &safeWheel) != nil {
			writeError(w, 409, "安全卖点轮盘不可用，请重新初始化")
			return
		}
		state.Queue = nil
		for _, t := range flattenAnchorStyleWheelTasks(safeWheel) {
			state.Queue = append(state.Queue, anchorcycle.Task{SellingPoint: t.SellingPoint, Angle: t.Angle, Key: t.Key, TargetAudience: t.TargetAudience, PainPoint: t.PainPoint})
		}
		state.Facts = safety.SafeFacts
		state.DerivedSafetyAssetID = safety.ID
	}
	state.TenantID, state.PlanID, state.RoomID = tenant, plan, room
	state.ContentStrategy = s.loadContentStrategy(ctx, tenant, plan, state.Facts)
	state.ProductFocus, err = s.buildCycleProductFocus(ctx, &state, links)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if state.ProductFocus != nil {
		state.SelectProductTask()
		if state.Status == "spacing_blocked" || state.Status == "compliance_blocked" {
			writeError(w, 409, state.Error)
			return
		}
	}
	if err = installP0Mother(&state); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	reportCycleProgress(ctx, 75)
	openingAsset, assetErr := s.store.GetLiveOpeningHook(ctx, tenant, plan)
	if assetErr == nil {
		state.OpeningHookLibraryMD, state.OpeningHookLibrarySource, state.OpeningHookLibraryAssetID = openingAsset.LibraryMD, openingAsset.Source, openingAsset.ID
	} else if !errors.Is(assetErr, appdb.ErrLiveOpeningHookNotFound) {
		writeError(w, 500, "读取方案开场钩子失败")
		return
	}
	// Validate availability before creating a new upstream session. Confirmed
	// plan versions freeze per run; no override uses the latest project mother.
	if _, err = renderAnchorOpeningHook(&state); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = s.installCycleTextModel(ctx, &state); err != nil {
		writeError(w, 503, err.Error())
		return
	}
	var session struct {
		ID string `json:"id"`
	}
	if state.TextModel == nil {
		created, e := client.CreateSession(ctx, input.AgentID, env, "大循环口播测试", map[string]string{"source": "anchor-cycle-workbench", "plan_id": strconv.FormatInt(plan, 10), "tenant_id": strconv.FormatInt(tenant, 10), "room_id": strconv.FormatInt(room, 10), "run_id": state.RunID})
		err = e
		session.ID = created.ID
	} else {
		session.ID = state.SessionID
	}
	if err != nil {
		writeError(w, 502, "建立大循环会话失败："+err.Error())
		return
	}
	state.TenantID = tenant
	reportCycleProgress(ctx, 90)
	state.PlanID = plan
	state.RoomID = room
	if err = s.installPlanP0Allowances(ctx, &state); err != nil {
		writeError(w, 503, "读取本方案已确认内容失败")
		return
	}
	state.SessionID = session.ID
	state.Session, _ = json.Marshal(session)
	state.StyleID = meta.ID
	state.WheelID = wheel.ID
	state.ScorecardID = card.ID
	if err = s.anchorCycles.Create(ctx, &state, token); err != nil {
		cycleError(w, err)
		return
	}
	cycleReply(w, r, &state)
}

func renderAnchorCyclePrompt(state *anchorcycle.State) (string, error) {
	if state.Config.TextProfileID != "" {
		messages, err := renderCycleTextMessages(state)
		if err != nil {
			return "", err
		}
		return traceAnchorStyleWorkbenchMessages(messages).System + "\n\n" + traceAnchorStyleWorkbenchMessages(messages).User, nil
	}
	if state.P0PendingRepair != nil {
		return renderP0Repair(state)
	}
	md, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_SEGMENT_PROMPT_PATH", "configs/prompts/anchor-style/cycle-segment-generation.md", "大循环逐段提示词")
	if err != nil {
		return "", err
	}
	scheduling, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_SCHEDULING_PROMPT_PATH", "configs/prompts/anchor-style/cycle-scheduling-data.md", "大循环调度补充")
	if err != nil {
		return "", err
	}
	md += "\n\n" + scheduling
	stageRules := ""
	stagePath, stageEnv := "", ""
	if state.Phase == "opening" {
		stagePath = "configs/prompts/anchor-style/cycle-opening-reentry.md"
		stageEnv = "ANCHOR_STYLE_CYCLE_REENTRY_PROMPT_PATH"
		if state.OpeningVersion() == "完整版" {
			stagePath = "configs/prompts/anchor-style/cycle-opening-full.md"
			stageEnv = "ANCHOR_STYLE_CYCLE_FIRST_OPENING_PROMPT_PATH"
		}
	} else if state.Phase == "closing" {
		stagePath = "configs/prompts/anchor-style/cycle-closing.md"
		stageEnv = "ANCHOR_STYLE_CYCLE_CLOSING_PROMPT_PATH"
	}
	if stagePath != "" {
		stageRules, _, err = loadReplaceablePromptAsset(stageEnv, stagePath, "大循环环节提示词")
		if err != nil {
			return "", err
		}
		if state.Phase == "opening" {
			openingHook, hookErr := renderAnchorOpeningHook(state)
			if hookErr != nil {
				return "", hookErr
			}
			stageRules = strings.ReplaceAll(stageRules, "{{opening_hook}}", openingHook)
		}
	}
	retry := ""
	contentIssue := ""
	feedback := state.PendingFeedback
	if len(state.Attempts) > 0 {
		contentIssue = state.Attempts[len(state.Attempts)-1].ContentIssue
		retry, _, err = loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_RETRY_PROMPT_PATH", "configs/prompts/anchor-style/cycle-retry.md", "大循环重试提示词")
		if err != nil {
			return "", err
		}
		retry = strings.ReplaceAll(retry, "{{attempt}}", strconv.Itoa(len(state.Attempts)+1))
		feedback = strings.TrimSpace(feedback + "\n" + state.Attempts[len(state.Attempts)-1].RetryFeedback)
	}
	used := []string{}
	for _, segment := range state.Accepted {
		if segment.Phase != "closing" {
			used = append(used, segment.Task.Key)
		}
	}
	// Full used-combination memory stays in Redis; prompts remain bounded.
	if len(used) > 20 {
		used = used[len(used)-20:]
	}
	phases := map[string]string{"opening": "开场", "body": "中间", "closing": "本轮收口"}
	priceRules, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_PRICE_PROMPT_PATH", "configs/prompts/anchor-style/cycle-price-delivery.md", "塑价落地提示词")
	if err != nil {
		return "", err
	}
	hook, source := "", ""
	if state.PriceHook != nil {
		hook, source = state.PriceHook.Promise, state.PriceHook.SourceText
	}
	priceRules = strings.NewReplacer("{{hook_allowed}}", strconv.FormatBool(state.PriceHookAllowed()), "{{pending_hook}}", hook, "{{hook_source}}", source, "{{content_issue}}", contentIssue).Replace(priceRules)
	bodyPlan := strconv.Itoa(state.BodyLimit()) + "段；已采纳" + strconv.Itoa(state.BodyCount) + "段"
	if state.Config.BodyMin > 0 {
		bodyPlan += "；允许范围" + strconv.Itoa(state.Config.BodyMin) + "～" + strconv.Itoa(state.Config.BodyMax) + "段；重写不改变计划，必要承接只能在上限内延长"
	}
	task := state.Task()
	maxChars := state.Config.MaxChars
	if task.ProductMode == "auxiliary" {
		maxChars = task.ProductMaxChars
		bodyPlan = "1段；已采纳0段"
	}
	productScope, err := renderCycleProductScope(state)
	if err != nil {
		return "", err
	}
	reuse := ""
	if use := state.Used[task.Key]; state.Phase != "closing" && use.Count > 0 {
		reuse, _, err = loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_REUSE_PROMPT_PATH", "configs/prompts/anchor-style/cycle-reuse.md", "轮盘组合复用提示词")
		if err != nil {
			return "", err
		}
		reuse = strings.NewReplacer("{{use_count}}", strconv.Itoa(use.Count), "{{last_expression}}", use.LastSummary).Replace(reuse)
	}
	md = renderStablePrefixTemplate(md, "{{target_audience}}", task.TargetAudience, "{{pain_point}}", task.PainPoint, "{{price_rules}}", priceRules, "{{body_plan}}", bodyPlan, "{{content_retry}}", contentIssue, "{{phase}}", phases[state.Phase], "{{cycle_number}}", strconv.Itoa(max(1, state.CycleNumber)), "{{stage_rules}}", stageRules, "{{reuse}}", reuse, "{{task}}", task.SellingPoint+" × "+task.Angle, "{{max_chars}}", strconv.Itoa(maxChars), "{{history}}", strings.Join(state.RecentSummaries, "\n"), "{{used}}", strings.Join(used, "、"), "{{retry}}", retry, "{{feedback}}", feedback)
	if !state.BootstrapSent && len(state.Accepted) == 0 && len(state.Attempts) == 0 {
		bootstrap, _, e := loadReplaceablePromptAsset("ANCHOR_STYLE_CYCLE_BOOTSTRAP_PROMPT_PATH", "configs/prompts/anchor-style/cycle-bootstrap.md", "大循环建档提示词")
		if e != nil {
			return "", e
		}
		md = renderStablePrefixTemplate(bootstrap, "{{style}}", state.StyleMD, "{{baseline}}", state.BaselineJSON, "{{facts}}", state.ProductFacts(task), "{{segment}}", md)
	}
	boundary, err := renderP0Boundary(state)
	if err != nil {
		return "", err
	}
	if boundary != "" {
		md = boundary + "\n\n" + md
	}
	if productScope != "" {
		md = productScope + "\n\n" + md
	}
	// Optional content enrichment: existing task and product scoping always win.
	if strategy, e := state.ContentStrategy.Render("主线", state.ProductFacts(task)); e == nil && strategy != "" {
		md += "\n\n" + strategy
	} else if e != nil {
		state.Warn("内容策略母版不可用，本段按原生成流程：" + e.Error())
	}
	return md, nil
}

func (s *Server) completeCycleMacro(ctx context.Context, state *anchorcycle.State) {
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	if !state.FeedbackPending {
		return
	}
	needed := state.ShouldFeedback()
	state.PendingFeedback = ""
	state.FeedbackPending = false
	if !needed {
		return
	}
	response := anchorStyleRollingFeedbackResponse{Status: "no_issue", Report: state.MacroReport, ProfileID: state.Config.FeedbackID, StyleAssetID: state.StyleID, ScorecardAssetID: state.ScorecardID, SelectedAddresses: []string{}}
	md, source, err := loadReplaceablePromptAsset("ANCHOR_STYLE_ROLLING_FEEDBACK_PROMPT_PATH", "configs/prompts/anchor-style/rolling-feedback-generation.md", "1000字采纳后回灌")
	if err == nil {
		recent := []string{}
		for _, segment := range state.Accepted[max(0, len(state.Accepted)-5):] {
			recent = append(recent, segment.Selected.Text)
		}
		data, _ := json.Marshal(map[string]any{"style_meta_prompt": state.StyleMD, "report": state.MacroReport, "expected_categories": rollingExpectedCategories(state.MacroReport.Issues), "recent_texts": recent, "accepted_style_window": state.StyleWindow})
		messages := []agentgateway.Message{{Role: "system", Content: md}, {Role: "user", Content: string(data)}}
		response.PromptSource = source
		response.Request = traceAnchorStyleWorkbenchMessages(messages)
		result, actual, e := s.completeAnchorStyleWorkbench(withPinnedSpeechProfile(ctx, state.Config.FeedbackID), state.Config.FeedbackID, "accepted_macro_feedback", agentgateway.ResponseJSON, messages, 1000, false)
		err = e
		response.Request = traceAnchorStyleWorkbenchMessages(actual)
		response.RawResponse = result.Text
		response.Model = result.Model
		response.Provider = result.Provider
		response.LatencyMS = result.LatencyMS
		response.InputTokens = result.InputTokens
		response.OutputTokens = result.OutputTokens
		if err == nil {
			response.Prompt, response.SelectedAddresses, response.Warnings, response.CorrectionSources, err = normalizeRollingFeedbackDetailed(result.Text, response.Report.AllowedAddresses, response.Report.Issues)
		}
	}
	if err != nil {
		response.Status = "model_error"
		response.Error = err.Error()
		state.Warn("回灌失败，本场继续，未使用旧建议：" + err.Error())
	} else {
		response.Status = "ready"
		state.PendingFeedback = response.Prompt
	}
	if len(state.Accepted) > 0 {
		state.Accepted[len(state.Accepted)-1].Feedback, _ = json.Marshal(response)
	}
}

func (s *Server) anchorStyleCycleAdvance(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	var input struct {
		ExpectedRevision *int64 `json:"expected_revision"`
	}
	if readJSON(w, r, &input) != nil || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
		writeError(w, 400, "必须提交当前大循环版本，避免重复生成")
		return
	}
	state, err := s.anchorCycles.Get(r.Context(), tenant, plan, room)
	if err != nil {
		cycleError(w, err)
		return
	}
	if !s.requireCycleRunAccess(w, r, actor, state) {
		return
	}
	if state.StreamControlled {
		writeError(w, 409, "本场由服务器供料，请通过连续声音流试听")
		return
	}
	w, r = cycleProgressStream(w, r)
	state, err = s.advanceAnchorCycle(r.Context(), tenant, plan, room, r.PathValue("runID"), *input.ExpectedRevision)
	if err != nil {
		cycleError(w, err)
		return
	}
	cycleReply(w, r, state)
}

// Shared by the isolated preview API and the server-owned formal producer.
// A room producer uses its published asset snapshot, never mutable active assets.
func (s *Server) advanceAnchorCycle(ctx context.Context, tenant, plan, room int64, run string, expected int64) (*anchorcycle.State, error) {
	token := anchorcycle.ID()
	if err := s.anchorCycles.Acquire(ctx, tenant, plan, room, token); err != nil {
		return nil, err
	}
	defer s.anchorCycles.Release(context.WithoutCancel(ctx), tenant, plan, room, token)
	state, err := s.anchorCycles.Get(ctx, tenant, plan, room)
	if err != nil {
		return nil, err
	}
	if state.RunID != run {
		return nil, anchorcycle.ErrStale
	}
	if state.Revision != expected {
		return nil, anchorcycle.ErrStale
	}
	if state.Uncertain {
		return nil, errors.New("上一轮生成结果未确认，请核对或结束本场重新开始，不能盲目重发")
	}
	if previewAudioComplete(state) {
		state.Status, state.Phase = "preview_complete", "ended"
		state.Error = "本场试听已完成，最多2分钟；原始话术保留，不影响正式直播。"
		if err = s.anchorCycles.Save(ctx, state); err != nil {
			return nil, err
		}
		return state, nil
	}
	if state.Phase == "ended" {
		return state, nil
	}
	if state.Status == "spacing_blocked" || state.Status == "content_blocked" || state.Status == "compliance_blocked" {
		return nil, errors.New(state.Error)
	}
	if state.Surface != "formal_live" {
		meta, cardErr := s.store.GetActiveLiveAnchorStyleMetaPrompt(ctx, tenant, plan)
		card, scoreErr := s.store.GetActiveLiveAnchorStyleScorecard(ctx, tenant, plan)
		wheel, wheelErr := s.store.GetActiveLiveSellingPointWheel(ctx, tenant, plan)
		if cardErr != nil || scoreErr != nil || wheelErr != nil || meta.ID != state.StyleID || card.ID != state.ScorecardID || wheel.ID != state.WheelID || meta.Status != "confirmed" || card.Status != "confirmed" || wheel.Status != "confirmed" {
			return nil, errors.New("本场风格、基准或轮盘已变化，请结束本场后重新开始")
		}
	}
	b, err := anchorstylescore.Parse(state.BaselineJSON)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	reportCycleProgress(ctx, 15)
	if state.TextModel != nil {
		state.BeginLecture()
	}
	ctx, done := s.cycleJobContext(ctx, token, tenant, room, state.RunID, state.Surface != "formal_live" && !state.StreamControlled)
	defer done()
	if err = installP0Mother(state); err != nil {
		return nil, errors.New(err.Error())
	}
	if state.FeedbackPending {
		s.completeCycleMacro(ctx, state)
		if err = s.anchorCycles.Save(ctx, state); err != nil {
			return nil, err
		}
	}
	prompt, err := renderAnchorCyclePrompt(state)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	// Planning precedes text-cache lookup so an old angle's voice cannot bypass
	// this request's selected hook. No TTS or audio protocol is changed.
	if state.TextModel != nil && cycleAnglePlanEnabled() {
		s.ensureCycleAnglePlan(ctx, state, *state.TextModel)
	}
	reuseEntry := (*anchorcycle.SpeechPoolEntry)(nil)
	if state.ShouldReuse() {
		reuseEntry = state.PickReuse(state.Task().Key)
	}
	reportCycleProgress(ctx, 25)
	var text, rawText string
	var eventIDs []string
	var generationMS int64
	var p0PatchError string
	patch := state.P0PendingRepair
	if reuseEntry != nil {
		text = reuseEntry.Text
		state.MarkSpeechReused(reuseEntry)
		prompt = "" // 话术池复用命中，不调用模型。
	} else if state.Config.TextProfileID != "" {
		state.Busy, state.Status, state.Error = true, "running", ""
		if err = s.anchorCycles.Save(ctx, state); err != nil {
			return nil, err
		}
		started := time.Now()
		result, actual, callErr := s.completeCycleText(ctx, state)
		state.Busy = false
		prompt = traceAnchorStyleWorkbenchMessages(actual).System + "\n\n" + traceAnchorStyleWorkbenchMessages(actual).User
		if callErr != nil {
			state.SkipGeneration("text_generation", callErr.Error())
			s.queueGenerationSkip(state, "text_generation", callErr.Error())
			persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cyclePersistenceTimeout())
			defer cancel()
			if err = s.anchorCycles.Save(persist, state); err != nil {
				return nil, err
			}
			return state, nil
		}
		text, rawText = strings.TrimSpace(result.Text), result.Text
		if text == "" {
			state.SkipGeneration("text_output_protocol", "正文为空")
			s.queueGenerationSkip(state, "text_output_protocol", "正文为空")
			if err = s.anchorCycles.Save(ctx, state); err != nil {
				return nil, err
			}
			return state, nil
		}
		generationMS = time.Since(started).Milliseconds()
		state.BootstrapSent = true
		if patch != nil {
			text, err = patch.Apply(result.Text, state.Config.MaxChars)
			if err != nil {
				p0PatchError = err.Error()
				text = patch.BaseText
			}
		}
	} else {
		profile, err := s.store.ResolveSpeechModel(ctx, state.Config.CredentialID)
		if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
			return nil, errors.New("本场阿里凭据不可用")
		}
		client, err := agentstudio.New(state.Config.WorkspaceID, state.Config.Region, profile.APIKey, agentStudioTimeout(profile.TimeoutMS))
		if err != nil {
			return nil, errors.New(err.Error())
		}
		state.Busy = true
		state.Status = "running"
		state.Error = ""
		if err = s.anchorCycles.Save(ctx, state); err != nil {
			return nil, err
		}
		ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan, RoomID: room, RunID: state.RunID})
		exchangeID, err := s.beginModelExchange(ctx, "agent_cycle_turn", state.Config.CredentialID, map[string]any{"session_id": state.SessionID, "input": []map[string]any{{"role": "user", "type": "message", "content": []map[string]string{{"type": "text", "text": prompt}}}}})
		if err != nil {
			state.Busy = false
			_ = s.anchorCycles.Save(ctx, state)
			return nil, err
		}
		started := time.Now()
		result, err := client.SendMessageAndWait(ctx, state.SessionID, prompt)
		err = s.finishModelExchange(ctx, exchangeID, result, result.RawText, started, 0, 0, err)
		state.Busy = false
		if err != nil {
			state.Uncertain = true
			state.Status = "failed"
			state.Error = "生成结果未确认：" + err.Error()
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cyclePersistenceTimeout())
			defer cancel()
			if saveErr := s.anchorCycles.Save(persistCtx, state); saveErr != nil {
				return nil, saveErr
			}
			return state, nil
		}
		text = strings.TrimSpace(result.Text)
		rawText, eventIDs = result.RawText, result.EventIDs
		generationMS = time.Since(started).Milliseconds()
		state.BootstrapSent = true
		if patch != nil {
			text, err = patch.Apply(result.Text, state.Config.MaxChars)
			if err != nil {
				p0PatchError = err.Error()
				text = patch.BaseText
			}
		}
	}
	// 复读检测：实时生成的话术若与历史逐字重复，换角度重试（程序侧硬约束，不靠模型自觉）。
	if reuseEntry == nil && state.IsDuplicateSpeech(text) {
		if state.RotateAfterDuplicate() {
			s.queueGenerationSkip(state, "speech_duplicate", "复读历史话术，已换角度")
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cyclePersistenceTimeout())
			_ = s.anchorCycles.Save(persistCtx, state)
			cancel()
			return state, nil
		}
		state.Warn("话术复读，但角度池已轮尽，接受本段")
	}
	reportCycleProgress(ctx, 60)
	score := anchorstylescore.Score(text, b, state.Task().SellingPoint+" × "+state.Task().Angle)
	retry := buildAnchorStyleScoreFeedback(score, b)
	candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: text, RawText: rawText, ScoreScope: "model_raw_text", Prompt: prompt, Score: score, GenerationMS: generationMS, EventIDs: eventIDs, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	candidate.AnglePlan = state.CurrentAnglePlan()
	candidate.AngleHook = state.CurrentAngleHook()
	candidate.HookCalculations, _ = state.CurrentHookCalculations()
	if patch != nil {
		candidate.P0Patch = patch
		candidate.P0PatchError = p0PatchError
		candidate.P0RewriteNumber = patch.RewriteNumber
		candidate.ScoreScope = "p0_local_patch_assembled"
	}
	if reuseEntry != nil {
		candidate.OutputText, candidate.ScoreScope = reuseEntry.Text, "speech_pool_reused"
	} else if state.Config.RepairPunctuation {
		postprocessStarted := time.Now()
		candidate.OutputText, candidate.PunctuationEdits = anchorcycle.RepairPunctuation(text, state.StyleMD, state.Config.MaxChars)
		candidate.PostprocessMS = time.Since(postprocessStarted).Milliseconds()
	}
	if retry.Status == "ready" {
		candidate.RetryFeedback = retry.Prompt
	} else if retry.Status != "no_issue" {
		state.Warn("单段回灌不可用，仍按基准重试：" + retry.Error)
	}
	candidate.OutputText = cycleVoiceText(candidate)
	cleanCycleOpeningInstruction(state, &candidate, "")
	cleanCycleConsecutiveSpeech(state, &candidate)
	if candidate.OutputText == "" {
		return s.skipExactDuplicateCandidate(ctx, state, candidate)
	}
	if textControlLeak(candidate.OutputText) {
		candidate.P0PatchError = "正文包含明确的内部控制字段"
	}
	if err = state.CheckP0Candidate(&candidate); err != nil {
		return nil, errors.New(err.Error())
	}
	s.reviewCycleP0Context(ctx, state, &candidate)
	s.auditLectureCandidate(ctx, state, &candidate)
	cleanCycleConsecutiveSpeech(state, &candidate)
	if candidate.OutputText == "" {
		return s.skipExactDuplicateCandidate(ctx, state, candidate)
	}
	if candidate.ExactDuplicateRemovals > 0 {
		log.Printf("mainline exact duplicate removed room=%d candidate=%s count=%d", room, candidate.ID, candidate.ExactDuplicateRemovals)
	}
	if len(candidate.OpeningInstructionEdits) > 0 || candidate.ExactDuplicateRemovals > 0 {
		candidate.Score = anchorstylescore.Score(candidate.OutputText, b, state.Task().SellingPoint+" × "+state.Task().Angle)
		candidate.ScoreScope = "opening_instruction_removed"
		if candidate.ExactDuplicateRemovals > 0 {
			candidate.ScoreScope = "consecutive_exact_duplicate_removed"
		}
		if finalFeedback := buildAnchorStyleScoreFeedback(candidate.Score, b); finalFeedback.Status == "ready" {
			candidate.RetryFeedback = finalFeedback.Prompt
		} else {
			candidate.RetryFeedback = ""
		}
	}
	s.auditFinalMainlineContent(ctx,state,&candidate)
	reviewState := *state
	adopted, err := state.AddCandidate(candidate, b)
	for _, segment := range state.Accepted {
		if segment.Selected.ID == candidate.ID {
			candidate = segment.Selected
		}
	}
	for _, attempt := range state.Attempts {
		if attempt.ID == candidate.ID {
			candidate = attempt
		}
	}
	s.queueP0Review(&reviewState, candidate)
	if len(state.GenerationSkips) > 0 && (len(reviewState.GenerationSkips) == 0 || state.GenerationSkips[len(state.GenerationSkips)-1].CreatedAt != reviewState.GenerationSkips[len(reviewState.GenerationSkips)-1].CreatedAt) {
		item := state.GenerationSkips[len(state.GenerationSkips)-1]
		if item.Stage != "text_review" {
			s.queueGenerationSkip(state, item.Stage, item.Reason)
		}
	}
	reportCycleProgress(ctx, 80)
	if err != nil {
		state.Status = "failed"
		state.Error = err.Error()
		state.Uncertain = true
	}
	if state.Phase != "ended" && !state.Uncertain && state.Status != "spacing_blocked" && state.Status != "content_blocked" && state.Status != "compliance_blocked" {
		state.Status = "ready"
	}
	state.Busy = adopted && state.FeedbackPending && ctx.Err() == nil
	// Commit the adoption before the optional auxiliary call. A later failure
	// cannot cause the accepted segment to be regenerated or removed.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cyclePersistenceTimeout())
	err = s.anchorCycles.Save(persistCtx, state)
	cancel()
	if err != nil {
		return nil, err
	}
	if adopted && state.FeedbackPending && ctx.Err() == nil {
		reportCycleProgress(ctx, 90)
		s.completeCycleMacro(ctx, state)
		state.Busy = false
		feedbackPersist, cancelFeedbackPersist := context.WithTimeout(context.WithoutCancel(ctx), s.cyclePersistenceTimeout())
		err = s.anchorCycles.Save(feedbackPersist, state)
		cancelFeedbackPersist()
		if err != nil {
			return nil, err
		}
	}
	return state, nil
}
func (s *Server) anchorStyleCycleEnd(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	run := r.PathValue("runID")
	state, err := s.anchorCycles.Get(r.Context(), tenant, plan, room)
	if err != nil && !errors.Is(err, anchorcycle.ErrMissing) {
		cycleError(w, err)
		return
	}
	if state != nil && state.RunID != run {
		cycleError(w, anchorcycle.ErrStale)
		return
	}
	if !s.requireCycleRunAccess(w, r, actor, state) {
		return
	}
	s.stopPreviewStream(state)
	s.cycleCancels.Lock()
	for _, entry := range s.cycleCancels.entries {
		if entry.tenant == tenant && entry.room == room && entry.run == run {
			entry.cancel()
		}
	}
	s.cycleCancels.Unlock()
	if err := s.anchorCycles.Delete(r.Context(), tenant, plan, room, run); err != nil {
		cycleError(w, err)
		return
	}
	if err = s.interruptAnchorCycle(r.Context(), state); err != nil {
		log.Printf("cycle end interrupt run=%s failed: %v", run, err)
		writeJSON(w, 200, map[string]any{"run": nil, "warning": "本地状态已清理，阿里中断请求失败；晚到结果不会采纳"})
		return
	}
	cycleReply(w, r, nil)
}
