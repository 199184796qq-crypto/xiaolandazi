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

type AgentPromptConfig struct {
	Key             string    `json:"key"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Scene           string    `json:"scene"`
	DefaultValue    string    `json:"default_value"`
	CurrentValue    string    `json:"current_value"`
	DraftValue      string    `json:"draft_value"`
	Enabled         bool      `json:"enabled"`
	DraftEnabled    bool      `json:"draft_enabled"`
	Version         uint64    `json:"version"`
	UpdatedByUserID int64     `json:"updated_by_user_id"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AgentPromptConfigUpdate struct {
	Key          string `json:"key"`
	CurrentValue string `json:"current_value"`
	Enabled      bool   `json:"enabled"`
}

type AgentPromptHistory struct {
	Key             string    `json:"key"`
	Version         uint64    `json:"version"`
	Value           string    `json:"value"`
	Enabled         bool      `json:"enabled"`
	Operation       string    `json:"operation"`
	UpdatedByUserID int64     `json:"updated_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type SystemSettingsDashboard struct {
	Settings             []SystemSetting                   `json:"settings"`
	Dictionaries         map[string][]SystemDictionaryItem `json:"dictionaries"`
	Warehouses           []Warehouse                       `json:"warehouses"`
	MembershipRoomLimits []MembershipRoomLimitSetting      `json:"membership_room_limits"`
	AgentPromptConfigs   []AgentPromptConfig               `json:"agent_prompt_configs"`
}

type PublicSystemConfig struct {
	SiteName                    string `json:"site_name"`
	AuthCustomerSideLabel       string `json:"auth_customer_side_label"`
	AuthCustomerTitleLine1      string `json:"auth_customer_title_line_1"`
	AuthCustomerTitleLine2      string `json:"auth_customer_title_line_2"`
	AuthCustomerDescription     string `json:"auth_customer_description"`
	AuthCustomerStatusLabel     string `json:"auth_customer_status_label"`
	AuthInternalSideLabel       string `json:"auth_internal_side_label"`
	AuthInternalTitleLine1      string `json:"auth_internal_title_line_1"`
	AuthInternalTitleLine2      string `json:"auth_internal_title_line_2"`
	AuthInternalDescription     string `json:"auth_internal_description"`
	AuthInternalStatusLabel     string `json:"auth_internal_status_label"`
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
