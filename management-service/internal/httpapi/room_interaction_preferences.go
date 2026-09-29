package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

func validInteractionPreference(value string, allowed ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateRoomInteractionPreferences(input *model.RoomInteractionPreferencesInput) error {
	if input == nil {
		return fmt.Errorf("互动偏好不能为空")
	}
	input.OverallInteraction = strings.ToLower(strings.TrimSpace(input.OverallInteraction))
	input.QuestionPreference = strings.ToLower(strings.TrimSpace(input.QuestionPreference))
	input.WelcomePreference = strings.ToLower(strings.TrimSpace(input.WelcomePreference))
	input.EngagementPreference = strings.ToLower(strings.TrimSpace(input.EngagementPreference))
	input.ChatPreference = strings.ToLower(strings.TrimSpace(input.ChatPreference))
	input.ConversionPreference = strings.ToLower(strings.TrimSpace(input.ConversionPreference))
	if !validInteractionPreference(input.OverallInteraction, "quiet", "natural", "active") {
		return fmt.Errorf("整体互动设置无效")
	}
	for label, value := range map[string]string{
		"回答问题": input.QuestionPreference,
		"欢迎新人": input.WelcomePreference,
		"点赞关注": input.EngagementPreference,
		"聊天互动": input.ChatPreference,
	} {
		if !validInteractionPreference(value, "less", "natural", "more") {
			return fmt.Errorf("%s设置无效", label)
		}
	}
	if !validInteractionPreference(input.ConversionPreference, "steady", "natural", "active") {
		return fmt.Errorf("成交互动设置无效")
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
		writeError(w, http.StatusInternalServerError, "保存互动偏好失败")
		return
	}
	if s.core != nil {
		query := url.Values{}
		query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
		resp, pushErr := s.core.DoRoom(
			r.Context(),
			tenantID,
			roomID,
			http.MethodPut,
			fmt.Sprintf("/internal/v1/rooms/%d/interaction-preferences", roomID),
			query,
			item,
		)
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
