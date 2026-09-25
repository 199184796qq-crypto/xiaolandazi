package userblock

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	ID         int64     `json:"id"`
	TenantID   int64     `json:"tenant_id"`
	RoomID     int64     `json:"room_id"`
	SubjectKey string    `json:"subject_key"`
	UserID     string    `json:"user_id,omitempty"`
	Nickname   string    `json:"nickname,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	BlockedAt  time.Time `json:"blocked_at"`
}

type Store struct {
	db *sql.DB

	mu    sync.RWMutex
	rooms map[int64]map[string]Entry
}

func NewStore(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	s := &Store{
		db:    db,
		rooms: make(map[int64]map[string]Entry),
	}
	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func SubjectKey(userID, nickname string) string {
	userID = strings.TrimSpace(userID)
	if userID != "" {
		return "uid:" + strings.ToLower(userID)
	}
	nickname = normalizeNickname(nickname)
	if nickname == "" {
		return ""
	}
	return "nick:" + nickname
}

func normalizeNickname(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func (s *Store) reload(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, room_id, subject_key, user_id, nickname, reason, blocked_at
		FROM core_room_user_blocks
		ORDER BY blocked_at DESC, id DESC
	`)
	if err != nil {
		return fmt.Errorf("load room user blocks: %w", err)
	}
	defer rows.Close()

	rooms := make(map[int64]map[string]Entry)
	for rows.Next() {
		var item Entry
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.RoomID,
			&item.SubjectKey,
			&item.UserID,
			&item.Nickname,
			&item.Reason,
			&item.BlockedAt,
		); err != nil {
			return fmt.Errorf("scan room user block: %w", err)
		}
		if rooms[item.RoomID] == nil {
			rooms[item.RoomID] = make(map[string]Entry)
		}
		rooms[item.RoomID][item.SubjectKey] = item
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate room user blocks: %w", err)
	}

	s.mu.Lock()
	s.rooms = rooms
	s.mu.Unlock()
	return nil
}

func (s *Store) IsBlocked(roomID int64, userID, nickname string) bool {
	key := SubjectKey(userID, nickname)
	if roomID <= 0 || key == "" {
		return false
	}
	s.mu.RLock()
	_, ok := s.rooms[roomID][key]
	s.mu.RUnlock()
	return ok
}

func (s *Store) List(roomID int64) []Entry {
	if roomID <= 0 {
		return nil
	}
	s.mu.RLock()
	roomItems := s.rooms[roomID]
	out := make([]Entry, 0, len(roomItems))
	for _, item := range roomItems {
		out = append(out, item)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].BlockedAt.Equal(out[j].BlockedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].BlockedAt.After(out[j].BlockedAt)
	})
	return out
}

func (s *Store) Block(
	ctx context.Context,
	tenantID, roomID int64,
	userID, nickname, reason string,
) (Entry, error) {
	if tenantID <= 0 || roomID <= 0 {
		return Entry{}, fmt.Errorf("invalid tenant or room")
	}
	userID = strings.TrimSpace(userID)
	nickname = strings.TrimSpace(nickname)
	reason = strings.TrimSpace(reason)
	key := SubjectKey(userID, nickname)
	if key == "" {
		return Entry{}, fmt.Errorf("user_id or nickname is required")
	}
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO core_room_user_blocks (
			tenant_id, room_id, subject_key, user_id, nickname, reason, blocked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			tenant_id = VALUES(tenant_id),
			user_id = VALUES(user_id),
			nickname = VALUES(nickname),
			reason = VALUES(reason),
			blocked_at = VALUES(blocked_at)
	`, tenantID, roomID, key, userID, nickname, reason, now)
	if err != nil {
		return Entry{}, fmt.Errorf("save room user block: %w", err)
	}

	var item Entry
	err = s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, room_id, subject_key, user_id, nickname, reason, blocked_at
		FROM core_room_user_blocks
		WHERE room_id = ? AND subject_key = ?
		LIMIT 1
	`, roomID, key).Scan(
		&item.ID,
		&item.TenantID,
		&item.RoomID,
		&item.SubjectKey,
		&item.UserID,
		&item.Nickname,
		&item.Reason,
		&item.BlockedAt,
	)
	if err != nil {
		return Entry{}, fmt.Errorf("read room user block: %w", err)
	}

	s.mu.Lock()
	if s.rooms[roomID] == nil {
		s.rooms[roomID] = make(map[string]Entry)
	}
	s.rooms[roomID][key] = item
	s.mu.Unlock()
	return item, nil
}

func (s *Store) Restore(ctx context.Context, roomID int64, userID, nickname string) (bool, error) {
	key := SubjectKey(userID, nickname)
	if roomID <= 0 || key == "" {
		return false, fmt.Errorf("invalid room or subject")
	}
	result, err := s.db.ExecContext(
		ctx,
		"DELETE FROM core_room_user_blocks WHERE room_id = ? AND subject_key = ?",
		roomID,
		key,
	)
	if err != nil {
		return false, fmt.Errorf("restore room user block: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	if roomItems := s.rooms[roomID]; roomItems != nil {
		delete(roomItems, key)
		if len(roomItems) == 0 {
			delete(s.rooms, roomID)
		}
	}
	s.mu.Unlock()
	return affected > 0, nil
}
