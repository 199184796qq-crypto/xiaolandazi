package contentblock

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 是房间级「屏蔽内容」记录。
//
// 运营在直播大屏右键一条公屏内容并选择「屏蔽相同内容」后，
// 该房间内内容与它完全一致的事件不再下发到前端。
// 只按整句比对，不做关键词包含匹配，避免误伤正常提问。
type Entry struct {
	ID         int64     `json:"id"`
	TenantID   int64     `json:"tenant_id"`
	RoomID     int64     `json:"room_id"`
	ContentKey string    `json:"content_key"`
	Content    string    `json:"content"`
	Reason     string    `json:"reason,omitempty"`
	BlockedAt  time.Time `json:"blocked_at"`
}

// Normalize 把内容归一化成比对口径：去掉全部空白（含全角空格、换行）、忽略大小写。
//
// 去空白而不是压缩成一个空格，是为了让「加 V 看主页」和「加V看主页」算同一条内容；
// 否则刷屏者只要多敲一个空格就能绕过屏蔽。本项目公屏以中文为主，
// 这个口径不会把正常提问合并成一条。
func Normalize(content string) string {
	return strings.ToLower(strings.Join(strings.Fields(content), ""))
}

// ContentKey 返回归一化后正文的摘要；空内容返回空串，表示不可屏蔽。
func ContentKey(content string) string {
	normalized := Normalize(content)
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
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

func (s *Store) reload(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, room_id, content_key, content, reason, blocked_at
		FROM core_room_content_blocks
		ORDER BY blocked_at DESC, id DESC
	`)
	if err != nil {
		return fmt.Errorf("load room content blocks: %w", err)
	}
	defer rows.Close()

	rooms := make(map[int64]map[string]Entry)
	for rows.Next() {
		var item Entry
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.RoomID,
			&item.ContentKey,
			&item.Content,
			&item.Reason,
			&item.BlockedAt,
		); err != nil {
			return fmt.Errorf("scan room content block: %w", err)
		}
		if rooms[item.RoomID] == nil {
			rooms[item.RoomID] = make(map[string]Entry)
		}
		rooms[item.RoomID][item.ContentKey] = item
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate room content blocks: %w", err)
	}

	s.mu.Lock()
	s.rooms = rooms
	s.mu.Unlock()
	return nil
}

// IsBlocked 判断这条内容在本房间是否已被屏蔽。
func (s *Store) IsBlocked(roomID int64, content string) bool {
	key := ContentKey(content)
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
	content, reason string,
) (Entry, error) {
	if tenantID <= 0 || roomID <= 0 {
		return Entry{}, fmt.Errorf("invalid tenant or room")
	}
	content = strings.TrimSpace(content)
	reason = strings.TrimSpace(reason)
	key := ContentKey(content)
	if key == "" {
		return Entry{}, fmt.Errorf("content is required")
	}
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO core_room_content_blocks (
			tenant_id, room_id, content_key, content, reason, blocked_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			tenant_id = VALUES(tenant_id),
			content = VALUES(content),
			reason = VALUES(reason),
			blocked_at = VALUES(blocked_at)
	`, tenantID, roomID, key, content, reason, now)
	if err != nil {
		return Entry{}, fmt.Errorf("save room content block: %w", err)
	}

	var item Entry
	err = s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, room_id, content_key, content, reason, blocked_at
		FROM core_room_content_blocks
		WHERE room_id = ? AND content_key = ?
		LIMIT 1
	`, roomID, key).Scan(
		&item.ID,
		&item.TenantID,
		&item.RoomID,
		&item.ContentKey,
		&item.Content,
		&item.Reason,
		&item.BlockedAt,
	)
	if err != nil {
		return Entry{}, fmt.Errorf("read room content block: %w", err)
	}

	s.mu.Lock()
	if s.rooms[roomID] == nil {
		s.rooms[roomID] = make(map[string]Entry)
	}
	s.rooms[roomID][key] = item
	s.mu.Unlock()
	return item, nil
}

// Restore 按 content_key 或原始正文撤销一条屏蔽记录。
func (s *Store) Restore(ctx context.Context, roomID int64, contentKey, content string) (bool, error) {
	key := strings.TrimSpace(contentKey)
	if key == "" {
		key = ContentKey(content)
	}
	if roomID <= 0 || key == "" {
		return false, fmt.Errorf("invalid room or content")
	}
	result, err := s.db.ExecContext(
		ctx,
		"DELETE FROM core_room_content_blocks WHERE room_id = ? AND content_key = ?",
		roomID,
		key,
	)
	if err != nil {
		return false, fmt.Errorf("restore room content block: %w", err)
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

func (s *Store) ClearRoom(roomID int64) {
	s.mu.Lock()
	delete(s.rooms, roomID)
	s.mu.Unlock()
}
