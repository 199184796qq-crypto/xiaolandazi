package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func validateRoomHumanBehaviorProfile(input *model.RoomHumanBehaviorProfileInput) error {
	if input == nil {
		return fmt.Errorf("主播行为配置不能为空")
	}
	input.TraitText = strings.TrimSpace(input.TraitText)
	input.StateText = strings.TrimSpace(input.StateText)
	if len([]rune(input.TraitText)) > 1000 {
		return fmt.Errorf("主播习惯不能超过1000字")
	}
	if len([]rune(input.StateText)) > 600 {
		return fmt.Errorf("当前状态不能超过600字")
	}
	if input.StateText == "" {
		input.StateExpiresAt = nil
		return nil
	}
	if input.StateExpiresAt == nil {
		expires := time.Now().UTC().Add(2 * time.Hour)
		input.StateExpiresAt = &expires
		return nil
	}
	expires := input.StateExpiresAt.UTC()
	if !expires.After(time.Now().UTC()) {
		return fmt.Errorf("当前状态有效期必须晚于现在")
	}
	input.StateExpiresAt = &expires
	return nil
}

func (s *Server) roomHumanBehaviorProfile(w http.ResponseWriter, r *http.Request) {
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
		item, err := s.store.GetRoomHumanBehaviorProfile(r.Context(), tenantID, roomID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取主播行为配置失败")
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}

	var input model.RoomHumanBehaviorProfileInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "主播行为配置格式错误")
		return
	}
	if err := validateRoomHumanBehaviorProfile(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.store.UpsertRoomHumanBehaviorProfile(r.Context(), tenantID, roomID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存主播行为配置失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
