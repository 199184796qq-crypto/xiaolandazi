package httpapi

import (
	"strings"
	"testing"
)

func TestValidateBrowserTitleSetting(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"小蓝搭子 · 智能直播系统", true}, {" 自定义系统名称 ", true}, {strings.Repeat("字", 100), true},
		{"", false}, {"  ", false}, {strings.Repeat("字", 101), false}, {"标题\n换行", false}, {"标题\x00", false},
	} {
		if err := validateBrowserTitleSetting("browser_title", tc.value); (err == nil) != tc.valid {
			t.Fatalf("%q: %v", tc.value, err)
		}
	}
	if err := validateBrowserTitleSetting("footer_extra_text", ""); err != nil {
		t.Fatal("unrelated optional field changed", err)
	}
}
