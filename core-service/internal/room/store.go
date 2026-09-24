package room

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"

	"livecompanion/core/internal/model"
)

var ErrConflict = errors.New("room already exists")

type Key struct {
	TenantID int64 `json:"tenant_id"`
	RoomID   int64 `json:"room_id"`
}

type Store struct {
	db *sql.DB
}

func NewStore(database *sql.DB) *Store {
	return &Store{db: database}
}

func (s *Store) List(ctx context.Context, tenantID *int64) ([]model.Room, error) {
	query := `
		SELECT id, tenant_id, platform, external_room_id, source_url, name, status,
		       collector_mode, monitor_enabled, device_online, online_count, last_event_at, created_at, updated_at
		FROM core_rooms
	`
	args := make([]any, 0, 1)

	if tenantID != nil {
		query += " WHERE tenant_id = ?"
		args = append(args, *tenantID)
	}
	query += " ORDER BY id DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Room, 0)
	for rows.Next() {
		item, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Store) ListByKeys(ctx context.Context, keys []Key) ([]model.Room, error) {
	if len(keys) == 0 {
		return []model.Room{}, nil
	}
	placeholders := make([]string, 0, len(keys))
	args := make([]any, 0, len(keys)*2)
	for _, key := range keys {
		if key.TenantID <= 0 || key.RoomID <= 0 {
			continue
		}
		placeholders = append(placeholders, "(?, ?)")
		args = append(args, key.TenantID, key.RoomID)
	}
	if len(placeholders) == 0 {
		return []model.Room{}, nil
	}
	query := `
		SELECT id, tenant_id, platform, external_room_id, source_url, name, status,
		       collector_mode, monitor_enabled, device_online, online_count, last_event_at, created_at, updated_at
		FROM core_rooms
		WHERE (tenant_id, id) IN (` + strings.Join(placeholders, ",") + `)
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Room, 0, len(keys))
	for rows.Next() {
		item, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Get(ctx context.Context, tenantID *int64, roomID int64) (model.Room, error) {
	query := `
		SELECT id, tenant_id, platform, external_room_id, source_url, name, status,
		       collector_mode, monitor_enabled, device_online, online_count, last_event_at, created_at, updated_at
		FROM core_rooms
		WHERE id = ?
	`
	args := []any{roomID}

	if tenantID != nil {
		query += " AND tenant_id = ?"
		args = append(args, *tenantID)
	}

	return scanRoom(s.db.QueryRowContext(ctx, query, args...))
}

func (s *Store) Create(ctx context.Context, input model.CreateRoomInput) (model.Room, error) {
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		platform = "douyin"
	}

	collectorMode := strings.TrimSpace(input.CollectorMode)
	if collectorMode == "" {
		collectorMode = "auto"
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO core_rooms (
			tenant_id, platform, external_room_id, source_url, name, status, collector_mode
		) VALUES (?, ?, ?, ?, ?, 'pending', ?)
	`,
		input.TenantID,
		platform,
		strings.TrimSpace(input.ExternalRoomID),
		strings.TrimSpace(input.SourceURL),
		strings.TrimSpace(input.Name),
		collectorMode,
	)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return model.Room{}, ErrConflict
		}
		return model.Room{}, err
	}

	roomID, err := result.LastInsertId()
	if err != nil {
		return model.Room{}, err
	}

	return s.Get(ctx, nil, roomID)
}

func (s *Store) Delete(ctx context.Context, tenantID *int64, roomID int64) error {
	query := "DELETE FROM core_rooms WHERE id = ?"
	args := []any{roomID}
	if tenantID != nil {
		query += " AND tenant_id = ?"
		args = append(args, *tenantID)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) MarkLive(ctx context.Context, tenantID int64, roomID int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE core_rooms
		SET status = 'live', last_event_at = CURRENT_TIMESTAMP(3)
		WHERE id = ? AND tenant_id = ? AND monitor_enabled = 1
	`, roomID, tenantID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRoom(scanner rowScanner) (model.Room, error) {
	var item model.Room
	var lastEventAt sql.NullTime

	err := scanner.Scan(
		&item.ID,
		&item.TenantID,
		&item.Platform,
		&item.ExternalRoomID,
		&item.SourceURL,
		&item.Name,
		&item.Status,
		&item.CollectorMode,
		&item.MonitorEnabled,
		&item.DeviceOnline,
		&item.OnlineCount,
		&lastEventAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.Room{}, err
	}

	if lastEventAt.Valid {
		value := lastEventAt.Time
		item.LastEventAt = &value
	}

	return item, nil
}

func (s *Store) SetStatus(
	ctx context.Context,
	tenantID int64,
	roomID int64,
	status string,
) error {
	if status == "live" {
		_, err := s.db.ExecContext(ctx, `
			UPDATE core_rooms
			SET status = ?
			WHERE id = ? AND tenant_id = ?
		`, status, roomID, tenantID)
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE core_rooms
		SET status = ?, online_count = 0
		WHERE id = ? AND tenant_id = ?
	`, status, roomID, tenantID)
	return err
}

func (s *Store) SetOnlineCount(
	ctx context.Context,
	tenantID int64,
	roomID int64,
	onlineCount uint64,
) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE core_rooms
		SET online_count = ?, last_event_at = CURRENT_TIMESTAMP(3)
		WHERE id = ? AND tenant_id = ? AND status = 'live'
	`, onlineCount, roomID, tenantID)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) UpdateRuntime(
	ctx context.Context,
	tenantID *int64,
	roomID int64,
	input model.UpdateRoomRuntimeInput,
) (model.Room, error) {
	sets := make([]string, 0, 2)
	args := make([]any, 0, 4)

	if input.MonitorEnabled != nil {
		sets = append(sets, "monitor_enabled = ?")
		args = append(args, *input.MonitorEnabled)
	}
	if input.DeviceOnline != nil {
		sets = append(sets, "device_online = ?")
		args = append(args, *input.DeviceOnline)
	}
	if len(sets) == 0 {
		return s.Get(ctx, tenantID, roomID)
	}

	query := "UPDATE core_rooms SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	args = append(args, roomID)

	if tenantID != nil {
		query += " AND tenant_id = ?"
		args = append(args, *tenantID)
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return model.Room{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.Room{}, err
	}
	if affected == 0 {
		return model.Room{}, sql.ErrNoRows
	}

	return s.Get(ctx, tenantID, roomID)
}
