package model

import "time"

// Append-only confirmed plan versions. Run progress and hook-use memory are
// deliberately not part of this asset.
type LiveOpeningHookAsset struct {
	ID              int64     `json:"id"`
	TenantID        int64     `json:"tenant_id"`
	PlanID          int64     `json:"plan_id"`
	LibraryMD       string    `json:"library_md"`
	Source          string    `json:"source"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
}
