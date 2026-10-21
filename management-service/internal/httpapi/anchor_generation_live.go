package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"livecompanion/management/internal/agentstudio"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/errordetail"
	"livecompanion/management/internal/model"
)

// Formal production is owned by the room lifecycle, not a browser connection.
// A restarted process must NOT resume an old Redis cursor or upstream session.
func (s *Server) SetAnchorProducerContext(ctx context.Context) { s.anchorProducerContext = ctx }

func (s *Server) startLiveAnchorProducer(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
	if s.anchorCycles == nil || s.core == nil {
		return errors.New("正式主线生成或Core未配置")
	}
	if _, ok := s.anchorCycles.(anchorcycle.VoiceRepository); !ok {
		return errors.New("异步声音队列未配置")
	}
	if _, err := s.cycleCacheOSS(); err != nil || s.anchorVoiceCache == nil {
		return errordetail.New("audio_cache_unavailable", "声音缓存配置", "OSS声音缓存不可用", "检查OSS配置及具体连接原因。", err)
	}
	owners, ok := s.anchorCycles.(anchorcycle.LiveOwnerRepository)
	if !ok {
		return errors.New("正式主线心跳租约未配置")
	}
	ownerToken := anchorcycle.ID()
	claimed, err := owners.ClaimLiveOwner(ctx, tenant, room, runtimeID, ownerToken)
	if err != nil {
		return err
	}
	if !claimed {
		return errors.New("本场已有正式生成器，不重复启动")
	}
	if err := s.endAnchorCyclesForRoom(ctx, tenant, room); err != nil {
		_ = owners.ReleaseLiveOwner(context.WithoutCancel(ctx), tenant, room, runtimeID, ownerToken)
		return err
	}
	// Register cancellation before launching: stop always wins, including warm-up.
	parent := s.anchorProducerContext
	if parent == nil {
		parent = context.Background()
	}
	runCtx, cancel := context.WithCancel(parent)
	runCtx = agentgateway.WithRoomModelScope(runCtx, agentgateway.RoomModelScope{TenantID: tenant, RoomID: room, RuntimeID: runtimeID, RequireLease: true})
	key := "formal-" + anchorcycle.ID()
	s.cycleCancels.Lock()
	if s.cycleCancels.entries == nil {
		s.cycleCancels.entries = map[string]cycleCancel{}
	}
	s.cycleCancels.entries[key] = cycleCancel{tenant: tenant, room: room, cancel: cancel}
	s.cycleCancels.Unlock()
	go func() {
		defer func() {
			release, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = owners.ReleaseLiveOwner(release, tenant, room, runtimeID, ownerToken)
		}()
		var guardFailed atomic.Bool
		defer cancel()
		defer func() { s.cycleCancels.Lock(); delete(s.cycleCancels.entries, key); s.cycleCancels.Unlock() }()
		watchDone := make(chan struct{})
		go func() {
			defer close(watchDone)
			for waitLiveProducer(runCtx) == nil {
				checkCtx, stop := context.WithTimeout(runCtx, 10*time.Second)
				_, err := s.checkLiveAnchorOwner(checkCtx, tenant, room, runtimeID, version.ID)
				if err == nil {
					var renewed bool
					renewed, err = owners.RenewLiveOwner(checkCtx, tenant, room, runtimeID, ownerToken)
					if err == nil && !renewed {
						err = anchorcycle.ErrStale
					}
				}
				stop()
				if err != nil {
					guardFailed.Store(true)
					cancel()
					return
				}
			}
		}()
		defer func() { cancel(); <-watchDone }()
		if err := s.runLiveAnchorProducer(runCtx, tenant, room, runtimeID, version); err != nil && (runCtx.Err() == nil || guardFailed.Load()) {
			s.handleLiveAnchorProducerExit(tenant, room, runtimeID, version, err)
		}
	}()
	return nil
}

func (s *Server) hasLiveAnchorProducer(tenant, room int64) bool {
	s.cycleCancels.Lock()
	defer s.cycleCancels.Unlock()
	for key, entry := range s.cycleCancels.entries {
		if strings.HasPrefix(key, "formal-") && entry.tenant == tenant && entry.room == room {
			return true
		}
	}
	return false
}

func (s *Server) createLiveAnchorRun(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) (*anchorcycle.State, error) {
	ctx = agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: tenant, RoomID: room, RuntimeID: runtimeID, RequireLease: true})
	s.reportLiveStartup(ctx, tenant, room, runtimeID, "reading_plan")
	scheme, err := realtimeScheme(version.GenerationContext)
	if err != nil {
		return nil, err
	}
	// 方案快照会一直沿用保存当时的风格。用户后来「取消使用」主播时，快照里的腔调必须失效，
	// 否则即使绑定已解除，本场仍会按旧主播的语气生成（表现为取消了还满口「哥哥姐姐」）。
	// 这里以当前绑定关系为准做一次兜底：绑定不在了就丢掉快照里的风格，按自由发挥跑。
	if scheme.StyleID != 0 {
		if bound, bindErr := s.store.GetLiveAnchorStyleForPlan(ctx, tenant, version.PlanID); errors.Is(bindErr, appdb.ErrLiveAnchorStyleNotFound) || (bindErr == nil && bound.ID != scheme.StyleID) {
			scheme.StyleID, scheme.ScorecardID = 0, 0
			scheme.StyleMD, scheme.BaselineJSON = "", ""
		} else if bindErr != nil && !errors.Is(bindErr, appdb.ErrLiveAnchorStyleNotFound) {
			return nil, errors.New("读取当前方案绑定的主播失败")
		}
	}
	ref := asSupportMap(version.GenerationContext["derived_safety"])
	hash, _ := ref["source_sha256"].(string)
	safety, err := s.store.GetLiveDerivedSafety(ctx, tenant, version.PlanID, hash)
	if err != nil {
		return nil, err
	}
	// A live session consumes the saved version. Source differences are reported
	// when Live Strategy loads the workspace, never by stopping its audio.
	state, err := s.prepareSavedAnchorTaskState(ctx, tenant, version.PlanID, version.GenerationContext, scheme, safety)
	if err != nil {
		return nil, err
	}
	identity := version.VoiceIdentity
	voiceID, modelName, err := s.resolveFormalVoice(ctx, tenant, fullShowVariantVoiceInput{Source: identity.Source, VoiceID: identity.VoiceID, ProfileID: identity.ProfileID, BindingID: identity.BindingID, Model: identity.Model})
	if err != nil {
		return nil, err
	}
	config := state.Config
	config.TTS = &anchorcycle.VoiceConfig{Source: identity.Source, VoiceID: voiceID, ProfileID: identity.ProfileID, Model: modelName, Rate: identity.Rate, BufferSeconds: scheme.BufferSeconds}
	state.Config = config
	state.TenantID, state.PlanID, state.RoomID = tenant, version.PlanID, room
	state.Surface = "formal_live"
	state.StyleID, state.ScorecardID, state.WheelID = scheme.StyleID, scheme.ScorecardID, scheme.WheelID
	state.StyleMD, state.BaselineJSON, state.Facts = scheme.StyleMD, scheme.BaselineJSON, safety.SafeFacts
	// 快照里没有评分基准 = 方案未绑主播风格：本场自由发挥、不打分。
	state.FreeStyle = strings.TrimSpace(scheme.BaselineJSON) == ""
	// 行业配置包：把行业的侧面池 + 分类→侧面映射塞进 State（文件即源），
	// 未选行业或加载失败时保持空、回退默认侧面/映射。
	if pack, packErr := s.loadIndustryPackForPlan(ctx, tenant, version.PlanID); packErr == nil && pack != nil {
		state.FissionSides = packFissionSides(pack.Sides)
		state.FactSideMap = packFactSideMap(pack.Sides)
		state.FactFamilyMap = packFactFamilyMap(pack.FactCategories)
	}
	state.ContentStrategy = s.loadContentStrategy(ctx, tenant, version.PlanID, state.Facts)
	state.DerivedSafetyAssetID = safety.ID
	state.Delivery = &anchorcycle.MainlineDelivery{VersionID: version.ID, RuntimeSessionID: runtimeID}
	state.Delivery.SourceFactsSHA, state.Delivery.SourceWheelSHA = derivedHash(safety.OriginalFacts), derivedHash(safety.OriginalWheelJSON)
	if err = installP0Mother(&state); err != nil {
		return nil, err
	}
	if err = s.installPlanP0Allowances(ctx, &state); err != nil {
		return nil, err
	}
	hook, err := s.store.GetLiveOpeningHook(ctx, tenant, version.PlanID)
	if err == nil {
		state.OpeningHookLibraryMD, state.OpeningHookLibrarySource, state.OpeningHookLibraryAssetID = hook.LibraryMD, hook.Source, hook.ID
	} else if !errors.Is(err, appdb.ErrLiveOpeningHookNotFound) {
		return nil, err
	}
	if _, err = renderAnchorOpeningHook(&state); err != nil {
		return nil, err
	}
	if err = s.installCycleTextModel(ctx, &state); err != nil {
		return nil, err
	}
	if state.TextModel != nil {
		token := anchorcycle.ID()
		if err = s.anchorCycles.Acquire(ctx, tenant, version.PlanID, room, token); err != nil {
			return nil, err
		}
		defer s.anchorCycles.Release(context.WithoutCancel(ctx), tenant, version.PlanID, room, token)
		if err = s.anchorCycles.Create(ctx, &state, token); err != nil {
			return nil, err
		}
		return &state, nil
	}
	profile, err := s.store.ResolveSpeechModel(ctx, config.CredentialID)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		return nil, errors.New("阿里凭据不可用")
	}
	client, err := agentstudio.New(config.WorkspaceID, config.Region, profile.APIKey, agentStudioTimeout(profile.TimeoutMS))
	if err != nil {
		return nil, err
	}
	token := anchorcycle.ID()
	if err = s.anchorCycles.Acquire(ctx, tenant, version.PlanID, room, token); err != nil {
		return nil, err
	}
	defer s.anchorCycles.Release(context.WithoutCancel(ctx), tenant, version.PlanID, room, token)
	env := config.EnvironmentID
	s.reportLiveStartup(ctx, tenant, room, runtimeID, "connecting_service")
	if env == "" {
		environments, e := client.ListEnvironments(ctx)
		if e != nil || len(environments) == 0 {
			return nil, errors.New("没有可用阿里环境")
		}
		env = environments[0].ID
	}
	scopeCtx := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: version.PlanID, RoomID: room, RunID: state.RunID})
	request := map[string]any{"agent": config.AgentID, "environment_id": env, "title": "直播主线", "metadata": map[string]string{"source": "formal-mainline", "run_id": state.RunID}}
	id, err := s.beginModelExchange(scopeCtx, "agent_session_create", config.CredentialID, request)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	session, err := client.CreateSession(scopeCtx, config.AgentID, env, "直播主线", map[string]string{"source": "formal-mainline", "run_id": state.RunID})
	err = s.finishModelExchange(scopeCtx, id, session, "", started, 0, 0, err)
	if err != nil {
		return nil, err
	}
	state.SessionID = session.ID
	state.Session, _ = json.Marshal(session)
	if err = s.anchorCycles.Create(ctx, &state, token); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.InterruptSession(cleanup, session.ID)
		return nil, err
	}
	return &state, nil
}

func (s *Server) checkLiveAnchorOwner(ctx context.Context, tenant, room, runtimeID, versionID int64) (bool, error) {
	session, err := s.store.GetLiveRuntimeByRoom(ctx, tenant, room)
	if err != nil {
		return false, err
	}
	if session.ID != runtimeID || !session.BelongsToExecutionRealm(s.executionRealm) || (session.Status != "running" && session.Status != "paused") {
		return false, anchorcycle.ErrStale
	}
	mode, err := s.store.GetLiveContentMode(ctx, tenant, room)
	if err != nil {
		return false, err
	}
	if mode.ContentMode != model.LiveContentAIDynamic {
		return false, errors.New("实时生成内容模式已变化")
	}
	// Saving a new snapshot must not cancel this session's startup snapshot.
	selected, err := s.store.GetLiveAgentPlanForRoom(ctx, tenant, room)
	if err != nil {
		return false, err
	}
	pinned, err := s.store.GetLiveAgentPlanVersion(ctx, tenant, selected.ID, versionID)
	if err != nil || pinned.PlanID != selected.ID {
		return false, errors.New("本场方案关联已变化，请重新开播")
	}
	return session.Status == "paused", nil
}

// Cold start hands the first accepted and voiced segment to Core immediately.
// Once the program is running, text may get one segment ahead of TTS and each
// refill remains bounded to two segments. This preserves the steady-state
// buffer without making the first audible segment wait for a second model turn.
// This uses the same scorer/feedback/compliance and complete voice bundle path.
func liveAnchorBatchTarget(state *anchorcycle.State) int {
	if state != nil && state.Delivery != nil && state.Delivery.ProgramID == "" {
		return 1
	}
	return 2
}

func (s *Server) produceLiveAnchorBatch(ctx context.Context, state *anchorcycle.State) ([]coreclient.ProgramRefreshTrack, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	target := liveAnchorBatchTarget(state)
	type item struct {
		state     *anchorcycle.State
		id        string
		err       error
		voiceDone chan struct{}
	}
	jobs := make(chan item, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(jobs)
		for accepted := 0; accepted < target; {
			if state.Delivery == nil {
				select {
				case jobs <- item{err: anchorcycle.ErrStale}:
				case <-ctx.Done():
				}
				return
			}
			for {
				paused, err := s.checkLiveAnchorOwner(ctx, state.TenantID, state.RoomID, state.Delivery.RuntimeSessionID, state.Delivery.VersionID)
				if err != nil {
					select {
					case jobs <- item{err: err}:
					case <-ctx.Done():
					}
					return
				}
				if !paused {
					break
				}
				if waitLiveProducer(ctx) != nil {
					return
				}
			}
			latest, err := s.anchorCycles.Get(ctx, state.TenantID, state.PlanID, state.RoomID)
			if err == nil && latest.RunID != state.RunID {
				err = anchorcycle.ErrStale
			}
			before := 0
			if err == nil {
				before = len(latest.Accepted)
				advanceCtx := ctx
				if latest.Delivery.ProgramID == "" {
					s.reportLiveStartup(ctx, latest.TenantID, latest.RoomID, latest.Delivery.RuntimeSessionID, "preparing_content")
					advanceCtx = context.WithValue(ctx, cycleProgressKey{}, cycleProgressReporter(func(percent int) {
						if percent >= 60 {
							s.reportLiveStartup(ctx, state.TenantID, state.RoomID, state.Delivery.RuntimeSessionID, "checking_content")
						}
					}))
				}
				latest, err = s.advanceAnchorCycle(advanceCtx, state.TenantID, state.PlanID, state.RoomID, state.RunID, latest.Revision)
			}
			if err == nil && (latest.Uncertain || latest.Phase == "ended" || latest.Status == "failed") {
				err = fmt.Errorf("本场不能继续生成：%s", latest.Error)
			}
			if err != nil {
				select {
				case jobs <- item{err: err}:
				case <-ctx.Done():
				}
				return
			}
			if len(latest.Accepted) == before {
				continue
			} // bounded engine retries/skips
			id := latest.Accepted[len(latest.Accepted)-1].Selected.ID
			var voiceDone chan struct{}
			if latest.Delivery.ProgramID == "" {
				s.reportLiveStartup(ctx, latest.TenantID, latest.RoomID, latest.Delivery.RuntimeSessionID, "checking_content")
			}
			select {
			case jobs <- item{state: latest, id: id, voiceDone: voiceDone}:
				accepted++
			case <-ctx.Done():
				return
			}
			if voiceDone != nil {
				select {
				case <-voiceDone:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-done }() // no orphan turn on a voice failure
	var tracks []coreclient.ProgramRefreshTrack
	for job := range jobs {
		if job.err != nil {
			return nil, job.err
		}
		if job.state.Delivery.ProgramID == "" {
			s.reportLiveStartup(ctx, job.state.TenantID, job.state.RoomID, job.state.Delivery.RuntimeSessionID, "generating_voice")
		}
		voice, err := s.synthesizeAnchorCycleVoice(ctx, job.state, job.id, false)
		if err != nil {
			return nil, err
		}
		if voice == nil || voice.Status != "ready" {
			return nil, errors.New("正式主线声音未完成")
		}
		if job.state.Delivery.ProgramID == "" {
			s.reportLiveStartup(ctx, job.state.TenantID, job.state.RoomID, job.state.Delivery.RuntimeSessionID, "voice_ready")
		}
		for _, segment := range job.state.Accepted {
			if segment.Selected.ID == job.id {
				segment.Voice = voice
				track, err := liveCycleMainlineTrack(job.state, segment)
				if err != nil {
					return nil, err
				}
				tracks = append(tracks, track)
			}
		}
		if job.voiceDone != nil {
			close(job.voiceDone)
		}
	}
	if len(tracks) != target {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("正式主线预备声音不完整")
	}
	return tracks, nil
}

func waitLiveProducer(ctx context.Context) error {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Server) runLiveAnchorProducerBatched(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
	state, err := s.createLiveAnchorRun(ctx, tenant, room, runtimeID, version)
	if err != nil {
		return err
	}
	for {
		paused, err := s.checkLiveAnchorOwner(ctx, tenant, room, runtimeID, version.ID)
		if err != nil {
			return err
		}
		if paused {
			if err = waitLiveProducer(ctx); err != nil {
				return err
			}
			continue
		}
		if state.Delivery.ProgramID != "" {
			snapshot, err := s.core.GetRoomProgramSnapshot(ctx, tenant, room)
			if err != nil {
				return err
			}
			if !snapshot.Running || snapshot.ProgramID != state.Delivery.ProgramID || snapshot.VersionID != version.ID {
				return anchorcycle.ErrStale
			}
			if snapshot.Suspended || bufferedMainlineMS(snapshot, state.Delivery.CurrentTrackDurations) >= int64(state.Config.TTS.BufferSeconds)*1000 {
				if err = waitLiveProducer(ctx); err != nil {
					return err
				}
				continue
			}
		}
		tracks, err := s.produceLiveAnchorBatch(ctx, state)

		if err != nil {
			return err
		}
		if len(tracks) == 0 {
			state, err = s.anchorCycles.Get(ctx, tenant, version.PlanID, room)
			if err != nil {
				return err
			}
			continue
		}
		// Recheck after slow model/TTS calls, before any room mutation.
		for {
			paused, err = s.checkLiveAnchorOwner(ctx, tenant, room, runtimeID, version.ID)
			if err != nil {
				return err
			}
			if !paused {
				break
			}
			if err = waitLiveProducer(ctx); err != nil {
				return err
			}
		}
		latest, err := s.anchorCycles.Get(ctx, tenant, version.PlanID, room)
		if err != nil || latest.RunID != state.RunID {
			return anchorcycle.ErrStale
		}
		state = latest
		if state.Delivery.ProgramID == "" {
			s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
			query := url.Values{"tenant_id": {strconv.FormatInt(tenant, 10)}}
			resp, err := s.core.DoRoom(ctx, tenant, room, http.MethodPost, fmt.Sprintf("/internal/v1/rooms/%d/audio/program/start", room), query, map[string]any{"session_id": fmt.Sprintf("live-runtime-%d", runtimeID), "label": fmt.Sprintf("实时主线 V%d", version.VersionNo), "version_id": version.ID, "version_no": version.VersionNo, "sequential": true, "tracks": tracks})
			if err != nil {
				return err
			}
			var snapshot coreclient.ProgramRefreshSnapshot
			err = json.NewDecoder(resp.Body).Decode(&snapshot)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 || !snapshot.Sequential || snapshot.VersionID != version.ID {
				return errors.New("正式声音主线启动未确认")
			}
			state.Delivery.ProgramID, state.Delivery.Generation = snapshot.ProgramID, snapshot.Generation
			state.Delivery.CurrentTrackDurations = trackDurations(tracks)
			for _, track := range tracks {
				state.Delivery.DeliveredCandidates = append(state.Delivery.DeliveredCandidates, track.ID)
			}
			if err = s.anchorCycles.Save(ctx, state); err != nil {
				return err
			}
			s.reportLiveStartup(ctx, tenant, room, runtimeID, "waiting_playback")
			continue
		}
		if err = s.deliverLiveAnchorBatch(ctx, state, tracks, func(ctx context.Context) (bool, error) {
			return s.checkLiveAnchorOwner(ctx, tenant, room, runtimeID, version.ID)
		}); err != nil {
			return err
		}
		removed := state.CompactDeliveredHistory()
		if len(removed) > 0 {
			if err = s.anchorCycles.Save(ctx, state); err != nil {
				return err
			}
			if pruner, ok := s.anchorCycles.(interface {
				PruneDeliveredVoices(context.Context, int64, int64, int64, string, []string) error
			}); ok {
				if err = pruner.PruneDeliveredVoices(ctx, tenant, version.PlanID, room, state.RunID, removed); err != nil {
					return err
				}
			}
		}
	}
}

// Persist intent before external mutation. Lost acknowledgements are reconciled
// by the exact program/job/generation, never by inventing another live session.
func (s *Server) deliverLiveAnchorBatch(ctx context.Context, state *anchorcycle.State, tracks []coreclient.ProgramRefreshTrack, owner func(context.Context) (bool, error)) error {
	runner := liveAnchorDeliveryRunner{core: s.core, save: s.anchorCycles.Save, owner: owner, now: time.Now, wait: waitLiveProducer}
	return runner.send(ctx, state, tracks)
}

type liveAnchorDeliveryRunner struct {
	core  contentRefreshCore
	save  func(context.Context, *anchorcycle.State) error
	owner func(context.Context) (bool, error)
	now   func() time.Time
	wait  func(context.Context) error
}

func (r liveAnchorDeliveryRunner) send(ctx context.Context, state *anchorcycle.State, tracks []coreclient.ProgramRefreshTrack) error {
	if state.Surface != "formal_live" || state.RoomID <= 0 || state.PreviewLimitMS != 0 || state.Delivery == nil || len(tracks) == 0 {
		return errors.New("只能交付独立正式主线")
	}
	d := state.Delivery
	job := "mainline-" + state.RunID + "-" + strconv.FormatUint(d.Generation+1, 10)
	d.PendingJobID = job
	d.PendingCandidates = nil
	for _, track := range tracks {
		d.PendingCandidates = append(d.PendingCandidates, track.ID)
	}
	if err := r.save(ctx, state); err != nil {
		return err
	}
	input := coreclient.ProgramRefreshInput{JobID: job, ExpectedProgramID: d.ProgramID, ExpectedVersionID: d.VersionID, ExpectedGeneration: d.Generation, Generation: d.Generation + 1, NewVersionID: d.VersionID, NewVersionNo: 1, Tracks: tracks}
	// Version number is descriptive; preserve the existing Core scheme number.
	deadline := r.now().Add(10 * time.Minute)
	for r.now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		paused, err := r.owner(ctx)
		if err != nil {
			return err
		}
		snapshot, err := r.core.GetRoomProgramSnapshot(ctx, state.TenantID, state.RoomID)
		if err != nil {
			if err = r.wait(ctx); err != nil {
				return err
			}
			continue
		}
		if !snapshot.Running || snapshot.ProgramID != d.ProgramID || snapshot.VersionID != d.VersionID || !snapshot.Sequential {
			return anchorcycle.ErrStale
		}
		if snapshot.LastAppliedJobID == job && snapshot.Generation == input.Generation {
			d.CurrentTrackDurations = trackDurations(tracks)
			d.Generation = input.Generation
			d.DeliveredCandidates = append(d.DeliveredCandidates, d.PendingCandidates...)
			d.PendingJobID = ""
			d.PendingCandidates = nil
			if len(d.DeliveredCandidates) > 128 {
				d.DeliveredCandidates = d.DeliveredCandidates[len(d.DeliveredCandidates)-128:]
			}
			return r.save(ctx, state)
		}
		if snapshot.Generation != d.Generation || (snapshot.PendingJobID != "" && snapshot.PendingJobID != job) {
			return anchorcycle.ErrStale
		}
		if paused || snapshot.Suspended {
			deadline = r.now().Add(10 * time.Minute)
		}
		if !paused && !snapshot.Suspended {
			if snapshot.PendingJobID == job {
				_, err = r.core.RenewRoomProgramRefresh(ctx, state.TenantID, state.RoomID, job, d.ProgramID, r.now().Add(30*time.Second))
			} else {
				input.NewVersionNo = snapshot.VersionNo
				input.ValidUntil = r.now().Add(30 * time.Second)
				_, err = r.core.RefreshRoomProgram(ctx, state.TenantID, state.RoomID, input)
			}
			if errors.Is(err, coreclient.ErrProgramRefreshConflict) {
				return err
			}
		} else if snapshot.PendingJobID == job {
			// Pause freezes output, but retain the already checked pending handoff.
			_, err = r.core.RenewRoomProgramRefresh(ctx, state.TenantID, state.RoomID, job, d.ProgramID, r.now().Add(30*time.Second))
		}
		if err = r.wait(ctx); err != nil {
			return err
		}
	}
	return errors.New("正式声音交付超过等待时限，已暂停，不会无限积累文字")
}

func trackDurations(tracks []coreclient.ProgramRefreshTrack) []int {
	result := make([]int, 0, len(tracks))
	for _, track := range tracks {
		result = append(result, track.DurationMS)
	}
	return result
}

func bufferedMainlineMS(snapshot coreclient.ProgramRefreshSnapshot, durations []int) int64 {
	if !snapshot.Running || snapshot.WaitingForContent {
		return 0
	}
	if snapshot.TrackIndex < 0 || snapshot.TrackIndex >= len(durations) {
		return 0
	}
	var total int64
	for _, duration := range durations[snapshot.TrackIndex:] {
		total += int64(duration)
	}
	return max(0, total-int64(max(0, snapshot.CurrentMS)))
}
