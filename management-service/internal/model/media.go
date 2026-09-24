package model

import "time"

type MediaAsset struct {
	ID              int64          `json:"id"`
	TenantID        int64          `json:"tenant_id"`
	AgentID         *int64         `json:"agent_id,omitempty"`
	AssetType       string         `json:"asset_type"`
	OriginalName    string         `json:"original_name"`
	StorageDriver   string         `json:"storage_driver"`
	StorageBucket   string         `json:"storage_bucket,omitempty"`
	ObjectKey       string         `json:"object_key"`
	MIMEType        string         `json:"mime_type"`
	SizeBytes       uint64         `json:"size_bytes"`
	DurationMS      *uint64        `json:"duration_ms,omitempty"`
	ChecksumSHA256  string         `json:"checksum_sha256"`
	Status          string         `json:"status"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedByUserID *int64         `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type CreateMediaAssetInput struct {
	TenantID        int64
	AgentID         *int64
	AssetType       string
	OriginalName    string
	StorageDriver   string
	StorageBucket   string
	ObjectKey       string
	MIMEType        string
	SizeBytes       uint64
	DurationMS      *uint64
	ChecksumSHA256  string
	Metadata        map[string]any
	CreatedByUserID int64
}

type VoiceProfile struct {
	ID              int64          `json:"id"`
	TenantID        int64          `json:"tenant_id"`
	AgentID         *int64         `json:"agent_id,omitempty"`
	Name            string         `json:"name"`
	Provider        string         `json:"provider"`
	VoiceID         string         `json:"voice_id"`
	SampleAssetID   *int64         `json:"sample_asset_id,omitempty"`
	CloneStatus     string         `json:"clone_status"`
	Config          map[string]any `json:"config,omitempty"`
	IsDefault       bool           `json:"is_default"`
	CreatedByUserID *int64         `json:"created_by_user_id,omitempty"`
	UpdatedByUserID *int64         `json:"updated_by_user_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type VoiceProfileInput struct {
	AgentID       *int64         `json:"agent_id,omitempty"`
	Name          string         `json:"name"`
	Provider      string         `json:"provider"`
	VoiceID       string         `json:"voice_id"`
	SampleAssetID *int64         `json:"sample_asset_id,omitempty"`
	CloneStatus   string         `json:"clone_status"`
	Config        map[string]any `json:"config,omitempty"`
	IsDefault     bool           `json:"is_default"`
}

type AgentConfigVersion struct {
	ID              int64          `json:"id"`
	AgentID         int64          `json:"agent_id"`
	VersionNo       uint64         `json:"version_no"`
	Layer1          map[string]any `json:"layer1"`
	Layer2          map[string]any `json:"layer2"`
	Layer3          map[string]any `json:"layer3"`
	Persona         map[string]any `json:"persona"`
	ModelConfig     map[string]any `json:"model_config"`
	SpeechConfig    map[string]any `json:"speech_config"`
	StyleProfile    map[string]any `json:"style_profile"`
	SafetyConfig    map[string]any `json:"safety_config"`
	LifecycleStatus string         `json:"lifecycle_status"`
	CreatedAt       time.Time      `json:"created_at"`
	PublishedAt     *time.Time     `json:"published_at,omitempty"`
}

type AgentConfigInput struct {
	Layer1       map[string]any `json:"layer1,omitempty"`
	Layer2       map[string]any `json:"layer2,omitempty"`
	Layer3       map[string]any `json:"layer3,omitempty"`
	Persona      map[string]any `json:"persona,omitempty"`
	ModelConfig  map[string]any `json:"model_config,omitempty"`
	SpeechConfig map[string]any `json:"speech_config,omitempty"`
	StyleProfile map[string]any `json:"style_profile,omitempty"`
	SafetyConfig map[string]any `json:"safety_config,omitempty"`
}
