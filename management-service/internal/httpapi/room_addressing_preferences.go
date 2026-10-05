package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

func validAddressingPreference(value string, allowed ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateAddressingTerms(label string, values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if utf8.RuneCountInString(value) > 20 {
			return nil, fmt.Errorf("%s单项不能超过20字", label)
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) > 12 {
			return nil, fmt.Errorf("%s最多12项", label)
		}
	}
	return result, nil
}

func validateRoomAddressingPreferences(input *model.RoomAddressingPreferencesInput) error {
	if input == nil {
		return fmt.Errorf("称呼习惯不能为空")
	}
	input.NamingPreference = strings.ToLower(strings.TrimSpace(input.NamingPreference))
	if !validAddressingPreference(input.NamingPreference, "less", "natural", "more") {
		return fmt.Errorf("点名偏好设置无效")
	}
	preferred, err := validateAddressingTerms("常用称呼", input.PreferredTerms)
	if err != nil {
		return err
	}
	blocked, err := validateAddressingTerms("不喜欢的称呼", input.BlockedTerms)
	if err != nil {
		return err
	}
	input.PreferredTerms = preferred
	input.BlockedTerms = blocked
	return nil
}

func (s *Server) roomAddressingPreferences(w http.ResponseWriter, r *http.Request) {
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
		item, err := s.store.GetRoomAddressingPreferences(r.Context(), tenantID, roomID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取称呼习惯失败")
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}

	var input model.RoomAddressingPreferencesInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "称呼习惯格式错误")
		return
	}
	if err := validateRoomAddressingPreferences(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.store.UpsertRoomAddressingPreferences(r.Context(), tenantID, roomID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存称呼习惯失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
