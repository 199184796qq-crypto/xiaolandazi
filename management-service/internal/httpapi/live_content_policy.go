package httpapi

import (
	"context"
	"errors"
	"fmt"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
)

func (s *Server) registerLiveContentPolicyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/system/live-content-policy", s.systemLiveContentPolicy)
	mux.HandleFunc("PUT /api/v1/system/live-content-policy", s.systemLiveContentPolicy)
	mux.HandleFunc("GET /api/v1/liveops/rooms/{roomID}/content-policy", s.roomLiveContentPolicy)
	mux.HandleFunc("PUT /api/v1/liveops/rooms/{roomID}/content-policy", s.roomLiveContentPolicy)
	mux.HandleFunc("GET /api/v1/live/rooms/{roomID}/content-mode", s.roomLiveContentMode)
	mux.HandleFunc("PUT /api/v1/live/rooms/{roomID}/content-mode", s.roomLiveContentMode)
}

func (s *Server) applyLiveContentModeVersion(ctx context.Context, tenantID, roomID int64, input *model.CreateLiveAgentPlanVersionInput) error {
	access, err := s.store.GetLiveContentMode(ctx, tenantID, roomID)
	if err != nil {
		return err
	}
	if input.GenerationContext == nil {
		input.GenerationContext = map[string]any{}
	}
	if supplied, _ := input.GenerationContext["content_mode"].(string); supplied == model.LiveContentAIDynamic && !access.DynamicAuthorized {
		return db.ErrContentModeNotAuthorized
	}
	// Generation context is user content, never authority for timing or grants.
	delete(input.GenerationContext, "content_policy")
	delete(input.GenerationContext, "dynamic_authorized")
	input.GenerationContext["content_mode"] = access.ContentMode
	return s.validateContentModeAssets(ctx, tenantID, access.ContentMode, input.Variants)
}

func (s *Server) validateLiveContentVersion(ctx context.Context, tenantID, roomID int64, version model.LiveAgentPlanVersion) error {
	access, err := s.store.GetLiveContentMode(ctx, tenantID, roomID)
	if err != nil {
		return err
	}
	// Entitlement controls future automatic generation, not playback of already
	// approved/generated static assets. Revocation falls back to pregenerated.
	return s.validateContentModeAssets(ctx, tenantID, access.ContentMode, version.Variants)
}

func (s *Server) validateContentModeAssets(ctx context.Context, tenantID int64, mode string, variants []model.LiveAgentPlanVersionVariant) error {
	for _, variant := range variants {
		if !variant.IsFormal {
			continue
		}
		if variant.AudioAssetID <= 0 {
			return errors.New("正式声音必须使用已登记的音频资产")
		}
		asset, err := s.store.GetMediaAsset(ctx, tenantID, variant.AudioAssetID)
		if err != nil {
			return err
		}
		uploaded := fmt.Sprint(asset.Metadata["purpose"]) == "live_agent_custom_mainline_audio"
		if mode == model.LiveContentUserAudio && !uploaded {
			return errors.New("原始录音模式只能采用上传录音，请上传录音后重新保存发布")
		}
		if mode != model.LiveContentUserAudio && uploaded {
			return errors.New("当前为AI话术模式，请采用AI成品声音或切换到原始录音模式")
		}
	}
	return nil
}

func (s *Server) systemLiveContentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requirePlatformAdmin(w, r)
	if !ok {
		return
	}
	s.serveLiveContentPolicy(w, r, actor, 0, 0)
}

func (s *Server) roomLiveContentPolicy(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireLiveSupportRoomCapability(w, r, model.LiveSupportCapabilityL3Policy)
	if !ok {
		return
	}
	s.serveLiveContentPolicy(w, r, actor, tenantID, roomID)
}

func (s *Server) serveLiveContentPolicy(w http.ResponseWriter, r *http.Request, actor model.Actor, tenantID, roomID int64) {
	if r.Method == http.MethodPut {
		var input struct {
			Policy            model.LiveContentPolicy `json:"policy"`
			Inherit           bool                    `json:"inherit"`
			ExpectedRevision  int64                   `json:"expected_revision"`
			DynamicAuthorized *bool                   `json:"dynamic_authorized,omitempty"`
			ExpectedMode      string                  `json:"expected_mode"`
		}
		if err := readJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "更新策略格式错误")
			return
		}
		if err := input.Policy.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if roomID > 0 && input.ExpectedMode != model.LiveContentAIPregenerated && input.ExpectedMode != model.LiveContentUserAudio && input.ExpectedMode != model.LiveContentAIDynamic {
			writeError(w, http.StatusBadRequest, "请刷新后重新读取当前直播模式")
			return
		}
		if err := s.store.SaveLiveContentPolicy(r.Context(), tenantID, roomID, actor.UserID, input.ExpectedRevision, input.Policy, input.Inherit, input.DynamicAuthorized, input.ExpectedMode); err != nil {
			if errors.Is(err, db.ErrContentPolicyConflict) {
				writeError(w, http.StatusConflict, err.Error())
			} else if errors.Is(err, db.ErrContentPolicyConsent) || errors.Is(err, db.ErrContentModeNotAuthorized) {
				writeError(w, http.StatusForbidden, err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "保存内容更新策略失败")
			}
			return
		}
	}
	item, err := s.store.GetLiveContentPolicy(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取内容更新策略失败")
		return
	}
	if roomID > 0 {
		item.RefreshStatus, err = s.store.GetLatestLiveContentRefreshStatus(r.Context(), tenantID, roomID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取内容更新任务状态失败")
			return
		}
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) roomLiveContentMode(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPut {
		var input struct {
			ContentMode string `json:"content_mode"`
		}
		if err := readJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "仅允许修改内容模式，更新周期由后台配置")
			return
		}
		if input.ContentMode != model.LiveContentAIPregenerated && input.ContentMode != model.LiveContentUserAudio && input.ContentMode != model.LiveContentAIDynamic {
			writeError(w, http.StatusBadRequest, "无效的内容模式")
			return
		}
		if err := s.store.SaveLiveContentMode(r.Context(), tenantID, roomID, actor.UserID, input.ContentMode, actor.IsInternalStaff()); err != nil {
			if errors.Is(err, db.ErrContentModeNotAuthorized) || errors.Is(err, db.ErrContentPolicyConsent) {
				writeError(w, http.StatusForbidden, err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "保存内容模式失败")
			}
			return
		}
	}
	result, err := s.store.GetLiveContentMode(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取可用内容模式失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
