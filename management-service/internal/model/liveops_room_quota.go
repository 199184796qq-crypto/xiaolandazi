package model

import "time"

type LiveOpsRoomQuotaSummary struct {
	TenantID            int64      `json:"tenant_id"`
	UserID              int64      `json:"user_id"`
	Username            string     `json:"username"`
	DisplayName         string     `json:"display_name"`
	Phone               string     `json:"phone"`
	Status              string     `json:"status"`
	ParentOrgName       string     `json:"parent_org_name"`
	MembershipPlanID    int64      `json:"membership_plan_id"`
	MembershipName      string     `json:"membership_name"`
	MembershipRoomLimit int64      `json:"membership_room_limit"`
	CurrentRoomCount    int64      `json:"current_room_count"`
	RoomLimit           int64      `json:"room_limit"`
	RemainingSlots      int64      `json:"remaining_slots"`
	LastReason          string     `json:"last_reason"`
	LastOperatorName    string     `json:"last_operator_name"`
	LastAdjustedAt      *time.Time `json:"last_adjusted_at,omitempty"`
}

type LiveOpsRoomQuotaAdjustInput struct {
	RoomLimit int64  `json:"room_limit"`
	Reason    string `json:"reason"`
}
