package httpapi

import (
	"strings"
	"testing"
)

// 主播卖自家东西却写「这家」，等于把自己当外人。默认必须按自家人播约束。
// 代播场景（称呼不是咱们/我们/本店）反过来：不能冒充店家。
func TestRoomSelfReferenceRule(t *testing.T) {
	owner := roomSelfReferenceRule("")
	if !strings.Contains(owner, "咱们家、咱们店") || !strings.Contains(owner, "「这家」") {
		t.Fatalf("default must be owner tone and forbid 这家, got: %s", owner)
	}
	custom := roomSelfReferenceRule("我们家、本店")
	if !strings.Contains(custom, "我们家、本店") || !strings.Contains(custom, "自家门店自播") {
		t.Fatalf("custom owner tone must be kept: %s", custom)
	}
	agency := roomSelfReferenceRule("他们家")
	if !strings.Contains(agency, "代播口吻") || strings.Contains(agency, "自家门店自播") {
		t.Fatalf("agency tone must not claim to be the owner: %s", agency)
	}
	if !strings.Contains(agency, "他们家") || !strings.Contains(agency, "不要混用「咱们家」") {
		t.Fatalf("agency tone must forbid owner wording: %s", agency)
	}
	// 超长称呼在选项校验处就被拒绝，这里只保证不 panic。
	if got := roomSelfReferenceRule(strings.Repeat("咱", 30)); got == "" {
		t.Fatal("long input must still render")
	}
}
