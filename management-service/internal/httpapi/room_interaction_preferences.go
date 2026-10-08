package httpapi

import (
	"errors"
	"fmt"
	"io"
	appdb "livecompanion/management/internal/db"
	"net/http"
	"strings"

	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

func validateRoomInteractionPreferences(input *model.RoomInteractionPreferencesInput) error {
	if input == nil {
		return fmt.Errorf("互动偏好不能为空")
	}
	for label, value := range map[string]int{
		"整体互动": input.OverallInteraction,
		"回答问题": input.QuestionPreference,
		"欢迎新人": input.WelcomePreference,
		"点赞关注": input.EngagementPreference,
		"聊天互动": input.ChatPreference,
		"成交互动": input.ConversionPreference,
	} {
		if value < 0 || value > 100 {
			return fmt.Errorf("%s必须在 0 到 100 之间", label)
		}
	}
	if input.Schedule != nil {
		return interactionpolicy.Validate(*input.Schedule)
	}
	return nil
}

func (s *Server) roomInteractionPreferences(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}

	if r.Method == http.MethodGet {
		item, err := s.store.GetRoomInteractionPreferences(r.Context(), tenantID, roomID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取互动偏好失败")
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}

	var input model.RoomInteractionPreferencesInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "互动偏好格式错误")
		return
	}
	if err := validateRoomInteractionPreferences(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.store.UpsertRoomInteractionPreferences(r.Context(), tenantID, roomID, actor.UserID, input)
	if err != nil {
		if errors.Is(err, appdb.ErrInteractionScheduleConflict) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "保存互动偏好失败")
		return
	}
	if s.core != nil {
		resp, pushErr := s.core.PutRoomInteractionPreferences(r.Context(), item)
		if pushErr != nil {
			writeError(w, http.StatusBadGateway, "偏好已保存，但热同步 Core 失败："+pushErr.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
			writeError(w, http.StatusBadGateway, fmt.Sprintf("偏好已保存，但 Core 热同步失败：HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw))))
			return
		}
	}
	writeJSON(w, http.StatusOK, item)
}
