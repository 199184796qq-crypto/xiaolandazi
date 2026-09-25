package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) LoadLatestLiveReview(
	ctx context.Context,
	tenantID, roomID int64,
	limit int,
) (model.LiveReviewSummary, []model.LiveReviewEvent, error) {
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	var startedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT occurred_at
		FROM live_room_event_archive
		WHERE tenant_id=? AND room_id=? AND event_type='session_start'
		ORDER BY occurred_at DESC, id DESC
		LIMIT 1
	`, tenantID, roomID).Scan(&startedAt)
	if err != nil {
		return model.LiveReviewSummary{}, nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_id, tenant_id, room_id, event_type,
		       user_id, nickname, content, COALESCE(payload_json, ''), occurred_at
		FROM live_room_event_archive
		WHERE tenant_id=? AND room_id=? AND occurred_at>=?
		ORDER BY occurred_at ASC, id ASC
		LIMIT ?
	`, tenantID, roomID, startedAt, limit)
	if err != nil {
		return model.LiveReviewSummary{}, nil, err
	}
	defer rows.Close()

	events := make([]model.LiveReviewEvent, 0)
	for rows.Next() {
		var item model.LiveReviewEvent
		if err := rows.Scan(
			&item.ID,
			&item.EventID,
			&item.TenantID,
			&item.RoomID,
			&item.EventType,
			&item.UserID,
			&item.Nickname,
			&item.Content,
			&item.PayloadJSON,
			&item.OccurredAt,
		); err != nil {
			return model.LiveReviewSummary{}, nil, err
		}
		events = append(events, item)
	}
	if err := rows.Err(); err != nil {
		return model.LiveReviewSummary{}, nil, err
	}

	summary := buildLiveReviewSummary(roomID, startedAt, events)
	return summary, events, nil
}

func buildLiveReviewSummary(roomID int64, startedAt time.Time, events []model.LiveReviewEvent) model.LiveReviewSummary {
	summary := model.LiveReviewSummary{RoomID: roomID, StartedAt: startedAt}
	var pausedAt *time.Time
	questions := map[string]*model.LiveReviewQuestionGroup{}

	for _, event := range events {
		eventType := strings.ToLower(strings.TrimSpace(event.EventType))
		switch eventType {
		case "session_start":
			continue
		case "session_end":
			value := event.OccurredAt.UTC()
			summary.EndedAt = &value
			if pausedAt == nil {
				pausedAt = &value
			}
			continue
		case "session_reopen":
			continue
		case "session_resume":
			if pausedAt != nil && event.OccurredAt.After(*pausedAt) {
				summary.InterruptedSeconds += int64(event.OccurredAt.Sub(*pausedAt).Seconds())
			}
			pausedAt = nil
			summary.EndedAt = nil
			continue
		}

		summary.EventCount++
		switch eventType {
		case "member", "enter":
			summary.Entries++
		case "chat", "comment":
			summary.Chats++
			content := normalizeReviewQuestion(event.Content)
			if content != "" && looksLikeReviewQuestion(content) {
				group := questions[content]
				if group == nil {
					group = &model.LiveReviewQuestionGroup{Text: strings.TrimSpace(event.Content)}
					questions[content] = group
				}
				group.Count++
				if event.OccurredAt.After(group.LastAt) {
					group.LastAt = event.OccurredAt
				}
				appendReviewNickname(group, event.Nickname)
			}
		case "like":
			summary.Likes += reviewEventCount(event.PayloadJSON)
		case "follow":
			summary.Follows++
		case "gift":
			summary.Gifts++
		case "order_signal":
			summary.OrderSignals++
		}
	}

	end := time.Now().UTC()
	if summary.EndedAt != nil {
		end = summary.EndedAt.UTC()
	}
	if end.After(startedAt) {
		summary.ActiveSeconds = int64(end.Sub(startedAt).Seconds()) - summary.InterruptedSeconds
		if summary.ActiveSeconds < 0 {
			summary.ActiveSeconds = 0
		}
	}

	summary.QuestionGroups = make([]model.LiveReviewQuestionGroup, 0, len(questions))
	for _, group := range questions {
		summary.QuestionGroups = append(summary.QuestionGroups, *group)
	}
	sort.Slice(summary.QuestionGroups, func(i, j int) bool {
		if summary.QuestionGroups[i].Count != summary.QuestionGroups[j].Count {
			return summary.QuestionGroups[i].Count > summary.QuestionGroups[j].Count
		}
		return summary.QuestionGroups[i].LastAt.After(summary.QuestionGroups[j].LastAt)
	})
	if len(summary.QuestionGroups) > 50 {
		summary.QuestionGroups = summary.QuestionGroups[:50]
	}
	return summary
}

func normalizeReviewQuestion(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(
		"？", "", "?", "", "。", "", ".", "", "！", "", "!", "",
		"，", "", ",", "", " ", "", "\t", "", "\n", "",
	).Replace(value)
	return value
}

func looksLikeReviewQuestion(value string) bool {
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, "吗么呢咋怎哪几多少谁何") {
		return true
	}
	for _, keyword := range []string{"价格", "发货", "快递", "保质", "怎么吃", "如何", "有没有", "能不能", "是不是"} {
		if strings.Contains(value, keyword) {
			return true
		}
	}
	return false
}

func appendReviewNickname(group *model.LiveReviewQuestionGroup, nickname string) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return
	}
	for _, existing := range group.Nicknames {
		if existing == nickname {
			return
		}
	}
	if len(group.Nicknames) < 5 {
		group.Nicknames = append(group.Nicknames, nickname)
	}
}

func reviewEventCount(payload string) int64 {
	if strings.TrimSpace(payload) == "" {
		return 1
	}
	var data map[string]any
	if json.Unmarshal([]byte(payload), &data) != nil {
		return 1
	}
	for _, key := range []string{"count", "like_count", "delta"} {
		switch value := data[key].(type) {
		case float64:
			if value > 0 {
				return int64(value)
			}
		case int64:
			if value > 0 {
				return value
			}
		}
	}
	return 1
}

func IsLiveReviewNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
