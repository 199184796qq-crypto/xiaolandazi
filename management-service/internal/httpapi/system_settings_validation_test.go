package httpapi

import "testing"

func TestValidateLivePolicyFontSetting(t *testing.T) {
	tests := []struct {
		key   string
		value string
		valid bool
	}{
		{"live_policy_rule_title_font_size", "26", true},
		{"live_policy_rule_title_font_size", "15", false},
		{"live_policy_rule_title_font_size", "41", false},
		{"live_policy_rule_body_font_size", "24", true},
		{"live_policy_rule_body_font_size", "13", false},
		{"live_policy_rule_body_font_size", "37", false},
		{"live_policy_rule_meta_font_size", "20", true},
		{"live_policy_rule_meta_font_size", "11", false},
		{"live_policy_rule_meta_font_size", "29", false},
		{"live_policy_test_title_font_size", "22", true},
		{"live_policy_test_title_font_size", "17", false},
		{"live_policy_test_title_font_size", "33", false},
		{"live_policy_test_body_font_size", "18", true},
		{"live_policy_test_body_font_size", "15", false},
		{"live_policy_test_body_font_size", "29", false},
		{"live_policy_test_meta_font_size", "16", true},
		{"live_policy_test_meta_font_size", "13", false},
		{"live_policy_test_meta_font_size", "25", false},
		{"site_name", "anything", true},
	}
	for _, test := range tests {
		err := validateLivePolicyFontSetting(test.key, test.value)
		if test.valid && err != nil {
			t.Fatalf("%s=%s should be valid: %v", test.key, test.value, err)
		}
		if !test.valid && err == nil {
			t.Fatalf("%s=%s should be invalid", test.key, test.value)
		}
	}
}

func TestValidateAuthHomepageSetting(t *testing.T) {
	tests := []struct {
		key   string
		value string
		valid bool
	}{
		{"auth_customer_side_label", "BANBO AI LIVE", true},
		{"auth_customer_title_line_1", "AI直播搭子，", true},
		{"auth_customer_description", "实时感知公屏互动。", true},
		{"auth_customer_description", " ", false},
		{"auth_internal_title_line_2", "", false},
		{"auth_customer_title_line_1", string(make([]rune, 49)), false},
		{"site_name", "", true},
	}
	for _, test := range tests {
		err := validateAuthHomepageSetting(test.key, test.value)
		if test.valid && err != nil {
			t.Fatalf("%s should be valid: %v", test.key, err)
		}
		if !test.valid && err == nil {
			t.Fatalf("%s should be invalid", test.key)
		}
	}
}
