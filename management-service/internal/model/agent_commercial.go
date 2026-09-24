package model

import "time"

type AgentLevel struct {
	ID                int64     `json:"id"`
	Code              string    `json:"code"`
	Name              string    `json:"name"`
	Status            string    `json:"status"`
	EntryFeeCents     uint64    `json:"entry_fee_cents"`
	IncludedDevices   uint64    `json:"included_devices"`
	DeviceDiscountBPS uint64    `json:"device_discount_bps"`
	ConsumerShareBPS  uint64    `json:"consumer_share_bps"`
	ReserveBPS        uint64    `json:"reserve_bps"`
	SettlementCycle   string    `json:"settlement_cycle"`
	HoldDays          uint64    `json:"hold_days"`
	OEMEnabled        bool      `json:"oem_enabled"`
	Note              string    `json:"note"`
	CreatedByUserID   *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID   *int64    `json:"updated_by_user_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type AgentLevelHistory struct {
	ID                int64      `json:"id"`
	AgentTenantID     int64      `json:"agent_tenant_id"`
	AgentName         string     `json:"agent_name"`
	LevelID           int64      `json:"level_id"`
	LevelCode         string     `json:"level_code"`
	LevelName         string     `json:"level_name"`
	PreviousLevelID   *int64     `json:"previous_level_id,omitempty"`
	PreviousLevelName string     `json:"previous_level_name,omitempty"`
	Status            string     `json:"status"`
	ApprovedByUserID  *int64     `json:"approved_by_user_id,omitempty"`
	ApprovedAt        time.Time  `json:"approved_at"`
	EffectiveAt       time.Time  `json:"effective_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	Reason            string     `json:"reason"`
	CreatedAt         time.Time  `json:"created_at"`
}

type AgentContractAttachment struct {
	ID              int64     `json:"id"`
	ContractID      int64     `json:"contract_id"`
	FileName        string    `json:"file_name"`
	FileURL         string    `json:"file_url"`
	ContentType     string    `json:"content_type"`
	SizeBytes       uint64    `json:"size_bytes"`
	PageOrder       int       `json:"page_order"`
	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}
type AgentContract struct {
	ID                  int64                     `json:"id"`
	ContractNo          string                    `json:"contract_no"`
	ExternalContractNo  string                    `json:"external_contract_no"`
	AgentTenantID       int64                     `json:"agent_tenant_id"`
	AgentName           string                    `json:"agent_name"`
	ParentContractID    *int64                    `json:"parent_contract_id,omitempty"`
	ContractType        string                    `json:"contract_type"`
	Status              string                    `json:"status"`
	LevelID             *int64                    `json:"level_id,omitempty"`
	LevelName           string                    `json:"level_name,omitempty"`
	LevelSnapshotJSON   string                    `json:"level_snapshot_json"`
	StartsOn            time.Time                 `json:"starts_on"`
	EndsOn              *time.Time                `json:"ends_on,omitempty"`
	ContractAmountCents uint64                    `json:"contract_amount_cents"`
	Note                string                    `json:"note"`
	SignedAt            *time.Time                `json:"signed_at,omitempty"`
	CreatedByUserID     *int64                    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID     *int64                    `json:"updated_by_user_id,omitempty"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
	Attachments         []AgentContractAttachment `json:"attachments"`
}
