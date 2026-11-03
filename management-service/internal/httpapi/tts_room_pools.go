package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/ttsscheduler"
)

func (s *Server) registerTTSRoomPoolRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/system/tts-voice-display", s.systemTTSVoiceDisplay)
	mux.HandleFunc("PUT /api/v1/system/tts-voice-display", s.systemTTSVoiceDisplay)
	mux.HandleFunc("GET /api/v1/system/tts-room-pools", s.systemTTSRoomPools)
	mux.HandleFunc("PUT /api/v1/system/tts-room-pools", s.systemTTSRoomPools)
}

func (s *Server) systemTTSRoomPools(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		tenant, _ := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
		room, _ := strconv.ParseInt(r.URL.Query().Get("room_id"), 10, 64)
		if tenant <= 0 || room <= 0 {
			writeError(w, 400, "请选择直播间")
			return
		}
		if _, err := s.getCoreRoomState(r.Context(), tenant, room); err != nil {
			writeError(w, 404, "直播间不存在或不属于所选账号")
			return
		}
		result, err := s.store.GetTTSRoomPoolPolicy(r.Context(), tenant, room)
		if err != nil {
			writeError(w, 500, "读取直播间语音池失败")
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var input db.TTSRoomPoolPolicy
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "语音池分配格式错误")
		return
	}
	if input.TenantID <= 0 || input.RoomID <= 0 {
		writeError(w, 400, "请选择直播间")
		return
	}
	if _, err := s.getCoreRoomState(r.Context(), input.TenantID, input.RoomID); err != nil {
		writeError(w, 404, "直播间不存在或不属于所选账号")
		return
	}
	result, err := s.store.SaveTTSRoomPoolPolicy(r.Context(), input, actor.UserID)
	if errors.Is(err, ttsscheduler.ErrRevision) {
		writeError(w, 409, err.Error())
		return
	}
	if errors.Is(err, ttsscheduler.ErrConfig) {
		writeError(w, 400, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "保存直播间语音池失败")
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) ttsPoolConfigured(ctx context.Context, pool string) (bool, error) {
	snapshot, err := s.store.TTSPoolSnapshot(ctx)
	if err != nil {
		return false, err
	}
	for _, account := range snapshot.Accounts {
		if account.Enabled && account.Configured && ttsscheduler.PoolForProvider(account.Provider) == pool {
			return true, nil
		}
	}
	return false, nil
}
