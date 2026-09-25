package model

import "time"

type LiveReviewEvent struct {
	ID          int64     `json:"id"`
	EventID     int64     `json:"event_id"`
	TenantID    int64     `json:"tenant_id"`
	RoomID      int64     `json:"room_id"`
	EventType   string    `json:"event_type"`
	UserID      string    `json:"user_id,omitempty"`
	Nickname    string    `json:"nickname,omitempty"`
	Content     string    `json:"content,omitempty"`
	PayloadJSON string    `json:"payload_json,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type LiveReviewQuestionGroup struct {
	Text      string    `json:"text"`
	Count     int64     `json:"count"`
	LastAt    time.Time `json:"last_at"`
	Nicknames []string  `json:"nicknames,omitempty"`
}

type LiveReviewSummary struct {
	RoomID             int64                     `json:"room_id"`
	StartedAt          time.Time                 `json:"started_at"`
	EndedAt            *time.Time                `json:"ended_at,omitempty"`
	ActiveSeconds      int64                     `json:"active_seconds"`
	InterruptedSeconds int64                     `json:"interrupted_seconds"`
	EventCount         int64                     `json:"event_count"`
	Entries            int64                     `json:"entries"`
	Chats              int64                     `json:"chats"`
	Likes              int64                     `json:"likes"`
	Follows            int64                     `json:"follows"`
	Gifts              int64                     `json:"gifts"`
	OrderSignals       int64                     `json:"order_signals"`
	QuestionGroups     []LiveReviewQuestionGroup `json:"question_groups"`
}
