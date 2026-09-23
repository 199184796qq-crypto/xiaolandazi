package model

import "time"

type FeatureRecord struct {
	ID              int64     `json:"id"`
	FeatureKey      string    `json:"feature_key"`
	RecordKey       string    `json:"record_key"`
	Title           string    `json:"title"`
	Status          string    `json:"status"`
	SortOrder       int       `json:"sort_order"`
	PayloadJSON     string    `json:"payload_json"`
	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID *int64    `json:"updated_by_user_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type FeatureRecordInput struct {
	RecordKey   string `json:"record_key"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sort_order"`
	PayloadJSON string `json:"payload_json"`
}
