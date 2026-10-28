package addressing

import "testing"

func TestValidateBaseKeepsOrderAndLength(t *testing.T) {
	rules := testRules(t)
	for _, tc := range []struct {
		base, raw string
		ok        bool
	}{
		{"挖坑不会埋", "挖坑不会埋", true},
		{"绿茵足球馆", "晋城市绿茵动力体育文化有限公司足球馆瑞雪", true},
		{"小陈子", "小陈", false},
		{"小陈", "小陈1234", true},
		{"", "小陈", false},
	} {
		err := rules.ValidateBase(tc.base, tc.raw)
		if tc.ok && err != nil {
			t.Errorf("ValidateBase(%q,%q) rejected: %v", tc.base, tc.raw, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("ValidateBase(%q,%q) accepted", tc.base, tc.raw)
		}
	}
}

func TestParseGreyRejectsInventedBase(t *testing.T) {
	rules := testRules(t)
	items := []GreyCandidate{
		{Viewer: Viewer{UserID: "1", RawName: "挖坑不会埋"}, Candidate: "挖坑不会埋"},
		{Viewer: Viewer{UserID: "2", RawName: "妈妈"}, Candidate: "妈妈"},
	}
	raw := `{"results":[{"index":0,"ok":true,"base":"挖坑不会埋"},{"index":1,"ok":false}]}`
	got, err := ParseGrey(raw, items, rules)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].OK || got[0].Base != "挖坑不会埋" {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].OK {
		t.Fatalf("kinship term accepted: %+v", got[1])
	}
}

func TestSalvageBaseUsesOriginalSegment(t *testing.T) {
	rules := testRules(t)
	got, ok := rules.SalvageBase("创科电池张云锋", "创科电池:张云锋")
	if !ok || got != "张云锋" {
		t.Fatalf("salvage=%q ok=%v", got, ok)
	}
}
