package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voicebatch"
	"net/http"
	"strconv"
	"time"
)

// Only this CAS helper writes voice runtime state. Manual requests survive
// concurrent progress updates; immutable saved plans are never modified.
func (s *Server) updateScriptVoiceBatch(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, change func(*voicebatch.State)) (voicebatch.State, error) {
	for retry := 0; retry < 6; retry++ {
		state, err := s.store.RuntimeVoiceBatch(ctx, tenant, room, session, v.ID)
		if errors.Is(err, sql.ErrNoRows) {
			state = voicebatch.State{Tenant: tenant, Room: room, Session: session, Version: v.ID, Active: v.VoiceIdentity, Batch: 1, ObservedAt: time.Now().UTC()}
		} else if err != nil {
			return state, err
		}
		change(&state)
		ok, err := s.store.SaveRuntimeVoiceBatch(ctx, state)
		if err != nil {
			return state, err
		}
		if ok {
			state.Revision++
			return state, nil
		}
		if err = ctx.Err(); err != nil {
			return state, err
		}
	}
	return voicebatch.State{}, errors.New("本场声音状态已经变化，请重试")
}

func voiceBatchAudioKey(piece string, asset int64) string {
	return piece + ":" + strconv.FormatInt(asset, 10)
}

func scriptAuthorizedPlaybackVoice(v model.LiveAgentPlanVersion, voices []recordingScriptVoice) bool {
	for _, voice := range voices {
		if voice.Identity != nil && voicebatch.Same(*voice.Identity, v.VoiceIdentity) {
			return true
		}
	}
	return false
}

func scriptVoiceBatchTarget(state voicebatch.State, voices []recordingScriptVoice, current, base model.LiveAgentPlanVersion) (model.LiveAgentPlanVersion, bool) {
	identity := state.Requested
	if state.Pending != nil {
		identity = state.Pending
	}
	if identity != nil {
		for _, voice := range voices {
			target := scriptVersionForVoice(base, voice)
			if voicebatch.Same(*identity, target.VoiceIdentity) {
				return target, !voicebatch.Same(current.VoiceIdentity, target.VoiceIdentity)
			}
		}
		return current, false
	}
	if !state.Due() || len(voices) < 2 {
		return current, false
	}
	for i, voice := range voices {
		target := scriptVersionForVoice(base, voice)
		if voicebatch.Same(target.VoiceIdentity, state.Active) {
			return scriptVersionForVoice(base, voices[(i+1)%len(voices)]), true
		}
	}
	return current, false
}

// Re-evaluate inside the CAS. A different manual choice made while TTS was
// running must win; its predecessor's paid audio stays reusable in the cache.
func stageScriptHostVoice(state *voicebatch.State, voices []recordingScriptVoice, current, base, prepared model.LiveAgentPlanVersion, instance string) bool {
	if state.Pending != nil {
		return false
	}
	target, ok := scriptVoiceBatchTarget(*state, voices, current, base)
	if !ok || !voicebatch.Same(target.VoiceIdentity, prepared.VoiceIdentity) {
		return false
	}
	identity := prepared.VoiceIdentity
	state.Pending = &identity
	state.PendingInstance = instance
	return true
}

func scriptVoiceFullTargets(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) []string {
	policy := scriptPlanPools(recordingCacheMediaRoot(), v, units)
	var keys []string
	for i, unit := range units {
		pool := scriptCompatiblePool(recordingCacheMediaRoot(), v, scriptUnitForVoice(v, unit))
		if len(pool.Versions) < policy.Caps[i] {
			return nil
		}
		for _, version := range pool.Versions {
			asset, _ := strconv.ParseInt(version.AudioRef, 10, 64)
			keys = append(keys, voiceBatchAudioKey(unit.poolID(), asset))
		}
	}
	return keys
}

// Observe exact PCM receipts; a queue append is not a host switch or full play.
func (s *Server) observeScriptVoiceBatch(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, units []volcengineScriptUnit, cursor *volcengineScriptCursor, p coreclient.ProgramRefreshSnapshot) (voicebatch.State, error) {
	now := time.Now().UTC()
	return s.updateScriptVoiceBatch(ctx, tenant, room, session, v, func(state *voicebatch.State) {
		state.Observe(now)
		started := map[string]bool{}
		complete := map[string]bool{}
		for _, r := range p.MainlinePlaybackReceipts {
			if r.StartedAt != "" {
				started[r.TrackID] = true
			}
		}
		for _, r := range p.PlaybackReceipts {
			if r.PlayedMS > 0 {
				started[r.TrackID] = true
			}
			if r.Complete {
				complete[r.TrackID] = true
			}
		}
		for _, track := range cursor.Delivered {
			if started[track.TrackID] && state.Started(track.InstanceID, now) {
				break
			}
		}
		if state.Completed == nil {
			state.Completed = map[string]bool{}
		}
		instances := map[string][]scriptDeliveredTrack{}
		for _, track := range cursor.Delivered {
			instances[track.InstanceID] = append(instances[track.InstanceID], track)
		}
		for _, tracks := range instances {
			first := tracks[0]
			if len(tracks) < max(1, first.TotalBlocks) {
				continue
			}
			if first.VoiceID != state.Active.VoiceID || first.Model != state.Active.Model || first.Batch != state.Batch || first.Unit < 0 || first.Unit >= len(units) {
				continue
			}
			all := true
			for _, track := range tracks {
				if !complete[track.TrackID] && !track.Completed {
					all = false
				}
			}
			if all {
				state.Completed[voiceBatchAudioKey(units[first.Unit].poolID(), first.AssetID)] = true
			}
		}
		if len(state.FullTargets) == 0 && state.Pending == nil {
			active := scriptVersionForVoice(v, recordingScriptVoice{Identity: &state.Active, VoiceID: state.Active.VoiceID, Model: state.Active.Model})
			state.FullTargets = scriptVoiceFullTargets(active, units)
		}
	})
}

type runtimeVoiceChoice struct {
	VoiceID string `json:"voice_id"`
	Model   string `json:"model"`
	Name    string `json:"name"`
}

func (s *Server) liveRuntimeVoice(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	room, ok := pathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.tenantForRoom(w, r, actor, room)
	if !ok {
		return
	}
	session, err := s.store.GetLiveRuntimeByRoom(r.Context(), tenant, room)
	if err != nil || (session.Status != "running" && session.Status != "paused") {
		writeError(w, 409, "当前没有可切换声音的直播场次")
		return
	}
	if !session.BelongsToExecutionRealm(s.executionRealm) {
		writeError(w, http.StatusConflict, "当前直播间AI由另一运行端控制")
		return
	}
	v, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(r.Context(), tenant, room)
	if err != nil || !usesScriptFirstParagraphs(v) {
		writeError(w, 409, "当前方案不支持直播切换主播声音")
		return
	}
	voices := scriptVoicesForPlayback(v)
	if r.Method == http.MethodPost {
		if !s.requireLiveStrategyRoomAccess(w, r, actor, tenant, room) {
			return
		}
		var input struct {
			Session int64  `json:"session_id"`
			Version int64  `json:"version_id"`
			Voice   string `json:"voice_id"`
			Model   string `json:"model"`
		}
		if readJSON(w, r, &input) != nil {
			writeError(w, 400, "声音选择格式错误")
			return
		}
		if input.Session != session.ID || input.Version != v.ID {
			writeError(w, 409, "直播场次已变化，请刷新后重试")
			return
		}
		var identity *model.LiveAgentVoiceIdentity
		for _, voice := range voices {
			if voice.VoiceID == input.Voice && voice.Model == input.Model {
				target := scriptVersionForVoice(v, voice)
				identity = &target.VoiceIdentity
				break
			}
		}
		if identity == nil {
			writeError(w, 400, "只能选择本场方案允许使用的声音")
			return
		}
		if err = s.validatePlanVoiceIdentity(r.Context(), tenant, identity); err != nil {
			writeError(w, 409, "所选声音当前不可用，请检查声音授权")
			return
		}
		if err = s.store.CheckTTSRoomPool(r.Context(), tenant, room, identity.Provider); err != nil {
			writeError(w, 409, "直播间未获授权使用所选语音池")
			return
		}
		_, err = s.updateScriptVoiceBatch(r.Context(), tenant, room, session.ID, v, func(state *voicebatch.State) {
			if voicebatch.Same(state.Active, *identity) && state.Pending == nil {
				state.Requested = nil
			} else {
				state.Requested = identity
			}
		})
		if err != nil {
			writeError(w, 409, "本场声音状态已变化，请重试")
			return
		}
	}
	state, err := s.store.RuntimeVoiceBatch(r.Context(), tenant, room, session.ID, v.ID)
	if errors.Is(err, sql.ErrNoRows) {
		state = voicebatch.State{Tenant: tenant, Room: room, Session: session.ID, Version: v.ID, Active: v.VoiceIdentity, Batch: 1}
		err = nil
	}
	if err != nil {
		writeError(w, 500, "读取本场声音状态失败")
		return
	}
	choices := []runtimeVoiceChoice{}
	for i, voice := range voices {
		target := scriptVersionForVoice(v, voice)
		name := target.VoiceIdentity.Name
		if name == "" {
			name = fmt.Sprintf("主播%d", i+1)
		}
		choices = append(choices, runtimeVoiceChoice{voice.VoiceID, target.VoiceIdentity.Model, name})
	}
	writeJSON(w, 200, map[string]any{"state": state, "voices": choices})
}
