package room

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/core/internal/model"
)

var (
	ErrRoomNumberBusy    = errors.New("请先结束直播并停止直播间连接，再修改房间号")
	ErrRoomNumberChanged = errors.New("房间号已被修改，请刷新后重试")
	ErrRoomNumberInvalid = errors.New("请输入128个字符以内的有效房间号，仅支持字母、数字、下划线和短横线")
	roomNumberPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

type NumberUpdate struct {
	ExternalRoomID         string `json:"external_room_id"`
	ExpectedExternalRoomID string `json:"expected_external_room_id"`
}

func ValidRoomNumber(value string) bool { return roomNumberPattern.MatchString(value) }

// UpdateNumber preserves the internal room ID and all device/plan/history links.
// The lifecycle gate orders this change against connection control; the row lock
// and expected old number protect concurrent writes from other Core nodes.
func (s *Store) UpdateNumber(ctx context.Context, tenantID, roomID int64, input NumberUpdate) (model.Room, error) {
	input.ExternalRoomID = strings.TrimSpace(input.ExternalRoomID)
	if !ValidRoomNumber(input.ExternalRoomID) || input.ExpectedExternalRoomID == "" {
		return model.Room{}, ErrRoomNumberInvalid
	}
	currentRoom, err := s.Get(ctx, &tenantID, roomID)
	if err != nil {
		return model.Room{}, err
	}
	current := s.lifecycle.entry(currentRoom)
	if current == nil {
		return model.Room{}, ErrCollectorReplaced
	}
	current.transition.Lock()
	defer current.transition.Unlock()
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.retired {
		return model.Room{}, ErrCollectorReplaced
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Room{}, err
	}
	defer tx.Rollback()
	item, err := scanRoom(tx.QueryRowContext(ctx, `SELECT id, tenant_id, platform, external_room_id, source_url, name, status,
		collector_mode, monitor_enabled, monitor_started_at, device_online, online_count, last_event_at, created_at, updated_at
		FROM core_rooms WHERE id = ? AND tenant_id = ? FOR UPDATE`, roomID, tenantID))
	if err != nil {
		return model.Room{}, err
	}
	if item.MonitorEnabled || (item.Status != "stopped" && item.Status != "offline" && item.Status != "pending" && item.Status != "device_offline" && item.Status != "error") {
		return model.Room{}, ErrRoomNumberBusy
	}
	if item.ExternalRoomID != input.ExpectedExternalRoomID {
		return model.Room{}, ErrRoomNumberChanged
	}
	if item.ExternalRoomID == input.ExternalRoomID {
		return item, nil
	}
	if item.Platform != "douyin" {
		return model.Room{}, ErrRoomNumberInvalid
	}
	sourceURL := "https://live.douyin.com/" + input.ExternalRoomID + "?from=web_code_link"
	_, err = tx.ExecContext(ctx, `UPDATE core_rooms SET external_room_id = ?, source_url = ?, online_count = 0,
		last_event_at = NULL, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND tenant_id = ?`, input.ExternalRoomID, sourceURL, roomID, tenantID)
	if err != nil {
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return model.Room{}, ErrConflict
		}
		return model.Room{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Room{}, err
	}
	item.ExternalRoomID, item.SourceURL = input.ExternalRoomID, sourceURL
	item.OnlineCount, item.LastEventAt, item.UpdatedAt = 0, nil, time.Now().UTC()
	current.room, current.status, current.enabled = item, item.Status, false
	current.generation++
	current.owned, current.collection = false, "stopped"
	current.disconnectedAt = time.Time{}
	s.lifecycle.cancelTimerLocked(current)
	return item, nil
}
