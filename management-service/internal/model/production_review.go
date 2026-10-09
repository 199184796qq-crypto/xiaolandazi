package model

import "time"

type ProductionReviewItem struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Stage     string    `json:"stage"`
	Decision  string    `json:"decision"`
	TaskKey   string    `json:"task_key"`
	Text      string    `json:"text"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
type ProductionReview struct {
	RuntimeSessionID int64                  `json:"runtime_session_id"`
	StartedAt        time.Time              `json:"started_at"`
	EndedAt          *time.Time             `json:"ended_at,omitempty"`
	Items            []ProductionReviewItem `json:"items"`
}
