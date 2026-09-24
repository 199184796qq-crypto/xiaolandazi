package model

import "time"

type SystemSetting struct {
	Key             string    `json:"key"`
	Group           string    `json:"group"`
	Label           string    `json:"label"`
	Value           string    `json:"value"`
	InputType       string    `json:"input_type"`
	SortOrder       int       `json:"sort_order"`
	UpdatedByUserID int64     `json:"updated_by_user_id"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SystemSettingUpdate struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type SystemDictionaryItem struct {
	ID           int64     `json:"id"`
	Category     string    `json:"category"`
	Code         string    `json:"code"`
	Label        string    `json:"label"`
	Description  string    `json:"description"`
	SortOrder    int       `json:"sort_order"`
	Enabled      bool      `json:"enabled"`
	SystemSeeded bool      `json:"system_seeded"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SystemDictionaryItemInput struct {
	Category    string `json:"category"`
	Code        string `json:"code"`
	Label       string `json:"label"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Enabled     bool   `json:"enabled"`
}

type MembershipRoomLimitSetting struct {
	PlanID     int64  `json:"plan_id"`
	PlanCode   string `json:"plan_code"`
	PlanName   string `json:"plan_name"`
	PlanStatus string `json:"plan_status"`
	RoomLimit  int64  `json:"room_limit"`
}

type MembershipRoomLimitUpdate struct {
	PlanID    int64 `json:"plan_id"`
	RoomLimit int64 `json:"room_limit"`
}

type SystemSettingsDashboard struct {
	Settings             []SystemSetting                   `json:"settings"`
	Dictionaries         map[string][]SystemDictionaryItem `json:"dictionaries"`
	Warehouses           []Warehouse                       `json:"warehouses"`
	MembershipRoomLimits []MembershipRoomLimitSetting      `json:"membership_room_limits"`
}

type PublicSystemConfig struct {
	SiteName                    string `json:"site_name"`
	InternalAgentName           string `json:"internal_agent_name"`
	ClientAgentName             string `json:"client_agent_name"`
	LivePolicyRuleTitleFontSize int    `json:"live_policy_rule_title_font_size"`
	LivePolicyRuleBodyFontSize  int    `json:"live_policy_rule_body_font_size"`
	LivePolicyRuleMetaFontSize  int    `json:"live_policy_rule_meta_font_size"`
	LivePolicyTestTitleFontSize int    `json:"live_policy_test_title_font_size"`
	LivePolicyTestBodyFontSize  int    `json:"live_policy_test_body_font_size"`
	LivePolicyTestMetaFontSize  int    `json:"live_policy_test_meta_font_size"`
	FooterEnabled               bool   `json:"footer_enabled"`
	FooterCopyright             string `json:"footer_copyright"`
	FooterICPText               string `json:"footer_icp_text"`
	FooterICPURL                string `json:"footer_icp_url"`
	FooterPoliceText            string `json:"footer_police_text"`
	FooterPoliceURL             string `json:"footer_police_url"`
	FooterReportText            string `json:"footer_report_text"`
	FooterReportURL             string `json:"footer_report_url"`
	FooterExtraText             string `json:"footer_extra_text"`
}
