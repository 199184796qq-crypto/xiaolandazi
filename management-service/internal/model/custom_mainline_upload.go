package model

import (
	"encoding/json"
	"time"
)

// Raw upload keys are temporary. Formal audio assets retain their existing lifecycle.
type CustomMainlineUploadFile struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Key         string `json:"key"`
	SnapshotKey string `json:"snapshot_key"`
}

type CustomMainlineUpload struct {
	ID         string                     `json:"id"`
	TenantID   int64                      `json:"-"`
	PlanID     int64                      `json:"-"`
	UserID     int64                      `json:"-"`
	Files      []CustomMainlineUploadFile `json:"-"`
	DurationMS int64                      `json:"-"`
	Status     string                     `json:"status"`
	Error      string                     `json:"error,omitempty"`
	Result     json.RawMessage            `json:"result,omitempty"`
	CreatedAt  time.Time                  `json:"created_at"`
	UpdatedAt  time.Time                  `json:"updated_at"`
	Cleaned    bool                       `json:"-"`
}
