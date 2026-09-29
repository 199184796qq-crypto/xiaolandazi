package model

import "time"

type LiveStrategyRule struct {
	Category        string         `json:"category"`
	Key             string         `json:"key"`
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	Enabled         bool           `json:"enabled"`
	BaseProbability int            `json:"base_probability"`
	MinProbability  int            `json:"min_probability"`
	SystemDefault   bool           `json:"system_default,omitempty"`
	Config          map[string]any `json:"config,omitempty"`
}

type LiveAddressingOption struct {
	Key           string `json:"key"`
	Text          string `json:"text"`
	Enabled       bool   `json:"enabled"`
	Probability   int    `json:"probability"`
	SystemDefault bool   `json:"system_default"`
}

type LiveStrategyCenterConfig struct {
	TenantID       int64                  `json:"tenant_id"`
	Rules          []LiveStrategyRule     `json:"rules"`
	AddressingMode string                 `json:"addressing_mode"`
	Addressing     []LiveAddressingOption `json:"addressing"`
	UpdatedBy      *int64                 `json:"updated_by_user_id,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at,omitempty"`
}

type LiveStrategyCenterInput struct {
	Rules          []LiveStrategyRule     `json:"rules"`
	AddressingMode string                 `json:"addressing_mode"`
	Addressing     []LiveAddressingOption `json:"addressing"`
}
