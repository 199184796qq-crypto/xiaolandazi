package httpapi

import "testing"

func TestStripPlanPromptHints(t *testing.T) {
	in := "【事实依据】\n〔提示〕一行一条，格式“分类：内容”。\n原料：八个月土麻鸭\n\n【用词消歧】\n〔提示〕一行一条。\n- 砍：默认是切块"
	got := stripPlanPromptHints(in)
	if containsHint(got) {
		t.Fatalf("提示行未剥离: %s", got)
	}
	if !containsSub(got, "原料：八个月土麻鸭") || !containsSub(got, "- 砍：默认是切块") {
		t.Fatalf("正文被误删: %s", got)
	}
	if containsSub(got, "\n\n\n") {
		t.Fatalf("连续空行未压缩: %s", got)
	}
}

func containsHint(text string) bool { return containsSub(text, "〔提示〕") }

func containsSub(text, sub string) bool {
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
