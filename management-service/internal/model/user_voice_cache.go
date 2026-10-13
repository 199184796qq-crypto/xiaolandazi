package model

import (
	"context"
	"time"
)

// Tenant ownership is taken from the running task, never from model output.
type TenantGenerationCachePolicyReader interface {
	GenerationCacheReuseEnabledForTenant(context.Context, int64, string) (bool, error)
	AnchorVoiceCacheRetentionHoursForTenant(context.Context, int64) (int, error)
}

type UserVoiceCacheSettings struct {
	MainlineReuse    bool `json:"mainline_cache_reuse_enabled"`
	InteractionReuse bool `json:"interaction_cache_reuse_enabled"`
	RetentionHours   int  `json:"retention_hours"`
}

type UserVoiceCachePolicy struct {
	UserVoiceCacheSettings
	TenantID     int64      `json:"tenant_id"`
	UserID       int64      `json:"user_id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"display_name"`
	Status       string     `json:"status"`
	UsingDefault bool       `json:"using_default"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}
