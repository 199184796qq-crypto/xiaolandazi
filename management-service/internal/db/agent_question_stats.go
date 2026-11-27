package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"
)

type QuestionStatsRoom struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type QuestionStatsSession struct {
	ID        int64     `json:"id"`
	StartedAt time.Time `json:"started_at"`
}

type QuestionStatsComment struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

const questionStatsRoomScope = `
    FROM live_agent_plans p
    JOIN core_rooms r ON r.tenant_id=p.tenant_id
    WHERE p.tenant_id=? AND p.id=? AND p.status='active'
      AND (EXISTS (
        SELECT 1 FROM live_agent_plan_room_bindings b
        WHERE b.tenant_id=p.tenant_id AND b.plan_id=p.id AND b.room_id=r.id AND b.status='active'
      ) OR EXISTS (
        SELECT 1 FROM live_agent_room_plan_selections chosen
        WHERE chosen.tenant_id=p.tenant_id AND chosen.plan_id=p.id AND chosen.room_id=r.id
      ))
      AND NOT EXISTS (SELECT 1 FROM mgmt_room_deletions d WHERE d.tenant_id=r.tenant_id AND d.room_id=r.id)`

func (s *Store) ListQuestionStatsRooms(ctx context.Context, tenantID, planID, roomID int64) ([]QuestionStatsRoom, error) {
	query := "SELECT r.id, r.name" + questionStatsRoomScope
	args := []any{tenantID, planID}
	if roomID > 0 {
		query += " AND r.id=?"
		args = append(args, roomID)
	}
	rows, err := s.db.QueryContext(ctx, query+" ORDER BY r.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rooms := []QuestionStatsRoom{}
	for rows.Next() {
		var room QuestionStatsRoom
		if err := rows.Scan(&room.ID, &room.Name); err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	return rooms, rows.Err()
}

func (s *Store) ListQuestionStatsSessions(ctx context.Context, tenantID, roomID int64) ([]QuestionStatsSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, occurred_at
        FROM live_room_event_archive
        WHERE tenant_id=? AND room_id=? AND event_type='session_start'
        ORDER BY occurred_at DESC, id DESC LIMIT 10`, tenantID, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []QuestionStatsSession{}
	for rows.Next() {
		var session QuestionStatsSession
		if err := rows.Scan(&session.ID, &session.StartedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) StreamQuestionStatsComments(ctx context.Context, tenantID, planID, roomID, sessionID int64, write func(QuestionStatsComment) error) (QuestionStatsSession, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return QuestionStatsSession{}, 0, err
	}
	defer tx.Rollback()
	var session QuestionStatsSession
	var allowed bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1"+questionStatsRoomScope+" AND r.id=?)",
		tenantID, planID, roomID).Scan(&allowed); err != nil {
		return session, 0, err
	}
	if !allowed {
		return session, 0, sql.ErrNoRows
	}
	err = tx.QueryRowContext(ctx, `SELECT id, occurred_at FROM (
        SELECT id, occurred_at FROM live_room_event_archive
        WHERE tenant_id=? AND room_id=? AND event_type='session_start'
        ORDER BY occurred_at DESC, id DESC LIMIT 10
    ) recent WHERE id=?`, tenantID, roomID, sessionID).Scan(&session.ID, &session.StartedAt)
	if err != nil {
		return session, 0, err
	}
	var next QuestionStatsSession
	err = tx.QueryRowContext(ctx, `SELECT id, occurred_at FROM live_room_event_archive
        WHERE tenant_id=? AND room_id=? AND event_type='session_start'
        AND (occurred_at>? OR (occurred_at=? AND id>?))
        ORDER BY occurred_at, id LIMIT 1`,
		tenantID, roomID, session.StartedAt, session.StartedAt, session.ID).Scan(&next.ID, &next.StartedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return session, 0, err
	}
	where := ` FROM live_room_event_archive
        WHERE tenant_id=? AND room_id=?
          AND (occurred_at>? OR (occurred_at=? AND id>?))`
	args := []any{tenantID, roomID, session.StartedAt, session.StartedAt, session.ID}
	if next.ID > 0 {
		where += " AND (occurred_at<? OR (occurred_at=? AND id<?))"
		args = append(args, next.StartedAt, next.StartedAt, next.ID)
	}
	// A final session_end closes the window even when the room has not started another session.
	// A later session_resume keeps both portions of the same session in scope.
	var closed QuestionStatsSession
	var transition string
	err = tx.QueryRowContext(ctx, `SELECT id, occurred_at, event_type`+where+
		` AND event_type IN ('session_end','session_resume') ORDER BY occurred_at DESC, id DESC LIMIT 1`,
		args...).Scan(&closed.ID, &closed.StartedAt, &transition)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return session, 0, err
	}
	if transition == "session_end" {
		where += " AND (occurred_at<? OR (occurred_at=? AND id<?))"
		args = append(args, closed.StartedAt, closed.StartedAt, closed.ID)
	}
	where += " AND event_type IN ('chat','comment') AND TRIM(content)<>''"
	// Return every group from the authorized archive snapshot. The caller
	// streams groups to a file without holding the full session in memory.
	rows, err := tx.QueryContext(ctx, `SELECT CAST(TRIM(content) AS BINARY) body, COUNT(*)`+
		where+` GROUP BY SHA2(TRIM(content),256), body ORDER BY COUNT(*) DESC, MIN(id)`, args...)
	if err != nil {
		return session, 0, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var item QuestionStatsComment
		if err := rows.Scan(&item.Text, &item.Count); err != nil {
			return session, 0, err
		}
		item.Text = strings.TrimSpace(item.Text)
		if strings.IndexFunc(item.Text, func(char rune) bool {
			return !unicode.IsControl(char) && !unicode.IsSpace(char)
		}) < 0 {
			continue
		}
		if err := write(item); err != nil {
			return session, 0, err
		}
		total += item.Count
	}
	return session, total, rows.Err()
}
