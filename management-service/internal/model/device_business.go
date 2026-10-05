package model

import (
	"encoding/json"
	"time"
)

// Billing is deliberately disabled for phase one. No wallet is read or mutated.
const DeviceBillingMode = "disabled"

type DeviceBusinessIdentity struct {
	DeviceID    int64  `json:"device_id"`
	TenantID    int64  `json:"tenant_id"`
	RoomID      *int64 `json:"room_id,omitempty"`
	BindingRole string `json:"binding_role,omitempty"`
}

type DeviceBusinessEvent struct {
	ID int64 `json:"event_id"`
	DeviceBusinessIdentity
	RequestID    string          `json:"request_id"`
	BusinessType string          `json:"business_type"`
	Status       string          `json:"status"`
	Action       string          `json:"action,omitempty"`
	Operation    string          `json:"operation,omitempty"`
	Text         string          `json:"text,omitempty"`
	Question     string          `json:"question,omitempty"`
	Value        json.RawMessage `json:"value,omitempty"`
	Message      string          `json:"message,omitempty"`
	ResultText   string          `json:"result_text,omitempty"`
	AssetID      *int64          `json:"asset_id,omitempty"`
	BillingMode  string          `json:"billing_mode"`
	ChargedBeans int64           `json:"charged_beans"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	PayloadHash  string          `json:"-"`
	ExpiresAt    time.Time       `json:"-"`
}

type DeviceCommandEventInput struct {
	HardwareMAC string          `json:"hardware_mac"`
	RequestID   string          `json:"request_id"`
	Action      string          `json:"action"`
	Operation   string          `json:"operation"`
	Status      string          `json:"status"`
	Value       json.RawMessage `json:"value"`
	Message     string          `json:"message"`
}

type DeviceCommandDispatchInput struct {
	HardwareMAC string `json:"hardware_mac"`
	RequestID   string `json:"request_id"`
	Action      string `json:"action"`
	Operation   string `json:"operation"`
}
