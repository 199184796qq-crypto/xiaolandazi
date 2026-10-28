package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func productionPreviewRequest(r *http.Request) bool {
	return strings.Contains(r.URL.Path, "/realtime-preview")
}
func (s *Server) unlimitedGenerationPreview(ctx context.Context, actor model.Actor) bool {
	if actor.IsPlatformAdmin() {
		return true
	}
	if !actor.IsInternalStaff() {
		return false
	}
	access, err := s.store.GetStaffAccess(ctx, actor.UserID)
	return err == nil && staffHasPermission(access, "system.settings.liveops.manage")
}
func (s *Server) requireCycleRunAccess(w http.ResponseWriter, r *http.Request, actor model.Actor, state *anchorcycle.State) bool {
	if state != nil && state.Surface == "formal_live" {
		writeError(w, http.StatusForbidden, "正式主线由开播生命周期驱动，测试接口不能读写正式场次")
		return false
	}
	if state != nil && state.RoomID == 0 && state.PreviewLimitMS == 0 && !s.unlimitedGenerationPreview(r.Context(), actor) {
		writeError(w, http.StatusForbidden, "本场为运维不限时试听，客户不能读取或驱动该场；请启动客户独立试听")
		return false
	}
	return true
}
func (s *Server) planSelectedVoice(ctx context.Context, tenant, plan, sourceRoom int64) (anchorcycle.VoiceConfig, error) {
	var identity model.LiveAgentVoiceIdentity
	// sourceRoom is an obsolete client hint. A preview uses the selected plan's
	// latest saved voice, exactly like the next live in every room.
	versions, err := s.store.ListLiveAgentPlanVersionsForPlan(ctx, tenant, plan)
	if err != nil {
		return anchorcycle.VoiceConfig{}, err
	}
	if len(versions) > 0 {
		identity = versions[0].VoiceIdentity
	}
	if identity.VoiceID == "" {
		return anchorcycle.VoiceConfig{}, errors.New("请先在智能体方案选择并保存声音；试听不另选音色")
	}
	settings, err := s.store.GetPlanVoiceRate(ctx, tenant, plan)
	if err != nil {
		return anchorcycle.VoiceConfig{}, err
	}
	identity.Rate = settings.Rate
	return anchorcycle.VoiceConfig{Source: identity.Source, VoiceID: identity.VoiceID, ProfileID: identity.ProfileID, Model: identity.Model, Rate: identity.Rate, BufferSeconds: 120}, nil
}
func cyclePreviewProjection(state *anchorcycle.State) any {
	return cyclePreviewProjectionForRequest(nil, state)
}

// Browser-facing cycle state never exposes an OSS address. The server keeps
// the private object reference in Redis, authorizes this run and streams the
// bytes from the OSS internal endpoint through the same-origin API.
func cyclePreviewProjectionForRequest(r *http.Request, state *anchorcycle.State) any {
	state = cycleStateForBrowser(r, state)
	if state == nil || state.Surface != "generation_preview" {
		return state
	}
	raw, _ := json.Marshal(state)
	var output map[string]any
	_ = json.Unmarshal(raw, &output)
	for _, key := range []string{"session", "session_id", "style_md", "baseline_json", "facts", "opening_hook_library_md", "p0_policy_md", "p0_audit_pending", "p0_pending_repair", "pending_feedback", "warnings"} {
		delete(output, key)
	}
	if config, ok := output["config"].(map[string]any); ok {
		for _, key := range []string{"workspace_id", "agent_id", "environment_id", "credential_profile_id", "feedback_profile_id", "region"} {
			delete(config, key)
		}
	}
	var strip func(any)
	strip = func(value any) {
		switch object := value.(type) {
		case map[string]any:
			for _, key := range []string{"prompt", "raw_text", "request", "raw_response", "feedback", "retry_feedback", "event_ids", "opening_hook", "p0_patch"} {
				delete(object, key)
			}
			for _, child := range object {
				strip(child)
			}
		case []any:
			for _, child := range object {
				strip(child)
			}
		}
	}
	strip(output)
	return output
}

func cycleStateForBrowser(r *http.Request, state *anchorcycle.State) *anchorcycle.State {
	if state == nil {
		return nil
	}
	copyState := *state
	// Preserve the API's always-array contract. append(nil, empty...) returns a
	// nil slice, which JSON encodes as null and breaks clients calling .map().
	copyState.Accepted = make([]anchorcycle.Segment, len(state.Accepted))
	copy(copyState.Accepted, state.Accepted)
	for i := range copyState.Accepted {
		voice := copyState.Accepted[i].Voice
		if voice == nil {
			continue
		}
		copyState.Accepted[i].Voice = cycleVoiceForBrowser(r, state, copyState.Accepted[i].Selected.ID, voice)
	}
	return &copyState
}

// Generation may finish the current sentence, but the browser receives only
// the remaining playback budget. Do not mutate the generated Redis/OSS bundle.
func cycleVoiceForBrowser(r *http.Request, state *anchorcycle.State, candidate string, voice *anchorcycle.VoiceAsset) *anchorcycle.VoiceAsset {
	if voice == nil {
		return nil
	}
	copyVoice := *voice
	copyVoice.Bundle = voice.Bundle
	if state.StreamControlled {
		copyVoice.Bundle.AudioURL = ""
		copyVoice.Bundle.AssetManifest = nil
		return &copyVoice
	}
	if copyVoice.Status != "ready" {
		return &copyVoice
	}
	if state.PreviewLimitMS > 0 && voice.PreviewReservedMS > 0 && voice.Bundle.AudioDurationMS > voice.PreviewReservedMS {
		timeline, text, err := previewTimelinePrefix(voice.Bundle.Timeline, voice.PreviewReservedMS, voice.Bundle.Text)
		if err != nil {
			copyVoice.Status, copyVoice.Error = "failed", err.Error()
			copyVoice.Bundle.AudioURL = ""
			return &copyVoice
		}
		copyVoice.Bundle.AudioDurationMS = voice.PreviewReservedMS
		copyVoice.Bundle.Timeline, copyVoice.Bundle.Text = timeline, text
		copyVoice.Bundle.SRT = liveAgentTimelineSRT(timeline)
		copyVoice.Bundle.SafePoints = buildFullShowSafePoints(timeline)
		copyVoice.Bundle.AssetManifest = make(map[string]any, len(voice.Bundle.AssetManifest))
		for key, value := range voice.Bundle.AssetManifest {
			copyVoice.Bundle.AssetManifest[key] = value
		}
		copyVoice.Bundle.AssetManifest["audio_duration_ms"] = voice.PreviewReservedMS
	}
	copyVoice.Bundle.AudioURL = cycleVoicePlaybackURL(r, state, candidate)
	return &copyVoice
}

func cycleVoicePlaybackURL(r *http.Request, state *anchorcycle.State, candidateID string) string {
	if state == nil {
		return ""
	}
	plan := strconv.FormatInt(state.PlanID, 10)
	run := url.PathEscape(state.RunID)
	candidate := url.PathEscape(candidateID)
	var path string
	if state.Surface == "generation_preview" {
		path = "/api/v1/live-agent-plans/" + plan + "/realtime-preview/" + run + "/voices/" + candidate + "/content"
	} else {
		path = "/api/v1/live-agent-plans/" + plan + "/anchor-style-workbench/cycle/" + run + "/segments/" + candidate + "/voice/content"
	}
	query := url.Values{}
	if r != nil {
		if tenantID := requestTenantID(r); tenantID > 0 {
			query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
		}
		if supportRoomID, ok := liveSupportScopeRoomID(r); ok {
			query.Set("support_room_id", strconv.FormatInt(supportRoomID, 10))
		}
	}
	if state.Surface != "generation_preview" && state.RoomID > 0 {
		query.Set("room_id", strconv.FormatInt(state.RoomID, 10))
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path
}
func previewAudioComplete(state *anchorcycle.State) bool {
	if state.StreamControlled || state.PreviewLimitMS <= 0 {
		return false
	}
	var used int64
	for _, segment := range state.Accepted {
		if segment.Voice != nil && segment.Voice.Status == "ready" {
			used += segment.Voice.Bundle.AudioDurationMS
		}
	}
	return used >= state.PreviewLimitMS
}
