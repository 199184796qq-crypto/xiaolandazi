package model

import "time"

const (
	CustomerCooperationStatusCooperating    = "cooperating"
	CustomerCooperationStatusNonCooperating = "non_cooperating"
)

type CustomerCooperationInfo struct {
	TenantID              int64      `json:"tenant_id"`
	Status                string     `json:"cooperation_status"`
	Note                  string     `json:"cooperation_note"`
	MarkedAt              *time.Time `json:"cooperation_marked_at,omitempty"`
	MarkedByUserID        int64      `json:"cooperation_marked_by_user_id,omitempty"`
	LastRechargeAt        *time.Time `json:"last_recharge_at,omitempty"`
	RechargeDormant90Days bool       `json:"recharge_dormant_90_days"`
}

func (c CustomerCooperationInfo) IsNonCooperating() bool {
	return c.Status == CustomerCooperationStatusNonCooperating
}
