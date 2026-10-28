package addressing

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRules(t *testing.T) *Rules {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "nickname-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			defaults := DefaultRules()
			return &defaults
		}
		t.Fatal(err)
	}
	return rules
}

func TestCheckHardRules(t *testing.T) {
	rules := testRules(t)
	cases := []struct {
		raw    string
		status Status
		text   string
	}{
		{"小明", StatusSafe, "小明"},
		{"爱吃苹果", StatusSafe, "爱吃苹果"},
		{"加V看福利", StatusDropped, ""},
		{"微信abc123", StatusDropped, ""},
		{"我是你爸爸", StatusDropped, ""},
		{"叫我老板", StatusDropped, ""},
		{"12345678", StatusDropped, ""},
		{"小林12345678", StatusRewritten, "小林"},
		{"小陈***隐藏部分", StatusRewritten, "小陈"},
		{"***隐藏部分", StatusDropped, ""},
		{"🎉🎉🎉", StatusDropped, ""},
		{"小陈🎉", StatusRewritten, "小陈"},
		{"超级爱吃苹果的人", StatusGrey, "超级爱吃苹果的人"},
		{"Ann Lee", StatusSafe, "Ann Lee"},
	}
	for _, tc := range cases {
		got := rules.Check(tc.raw)
		if got.Status != tc.status {
			t.Errorf("Check(%q)=%s want %s", tc.raw, got.Status, tc.status)
		}
		if tc.text != "" && got.Text != tc.text {
			t.Errorf("Check(%q).Text=%q want %q", tc.raw, got.Text, tc.text)
		}
	}
}

func TestPickRespectsDiceUpperBound(t *testing.T) {
	rules := testRules(t)
	rules.Dice.Max = 5
	viewers := make([]Viewer, 12)
	for i := range viewers {
		viewers[i] = Viewer{UserID: string(rune('a' + i)), RawName: "名字"}
	}
	for seed := int64(0); seed < 50; seed++ {
		got := Pick(viewers, rules, rand.New(rand.NewSource(seed)))
		if len(got) < 1 || len(got) > 5 {
			t.Fatalf("seed=%d picked=%d", seed, len(got))
		}
		// recent: the picked tail ends at the newest viewer
		if got[len(got)-1].UserID != viewers[len(viewers)-1].UserID {
			t.Fatalf("seed=%d did not pick from the newest tail", seed)
		}
	}
}

func TestBuildFoldsBadNamesIntoGroupAndKeepsIdentity(t *testing.T) {
	rules := testRules(t)
	viewers := []Viewer{
		{UserID: "1", RawName: "小陈"},
		{UserID: "2", RawName: "加V看福利"},
		{UserID: "3", RawName: "老王"},
		{UserID: "4", RawName: "阿芳"},
		{UserID: "5", RawName: "老周"},
	}
	plan := Build(viewers, rules, rand.New(rand.NewSource(7)))
	if plan.Mode != "mixed" && plan.Mode != "single" && plan.Mode != "group" {
		t.Fatalf("unexpected mode %q", plan.Mode)
	}
	for _, picked := range plan.Picked {
		if picked.UserID == "2" {
			t.Fatal("non-compliant viewer was named")
		}
		if picked.RawName == "" {
			t.Fatal("identity lost")
		}
	}
}

func TestBuildNeverNamesDroppedOnlyBatch(t *testing.T) {
	rules := testRules(t)
	viewers := []Viewer{
		{UserID: "1", RawName: "微信加我"},
		{UserID: "2", RawName: "123456"},
	}
	plan := Build(viewers, rules, rand.New(rand.NewSource(3)))
	if plan.Mode != "group" || len(plan.Picked) != 0 {
		t.Fatalf("plan=%+v", plan)
	}
	if plan.Dropped == 0 {
		t.Fatal("dropped viewers not counted")
	}
}

func TestShortenKeepsSuffixTitle(t *testing.T) {
	rules := testRules(t)
	if got := Shorten("爱跳舞的丽丽姐", *rules); !strings.HasSuffix(got, "姐") {
		t.Fatalf("Shorten=%q", got)
	}
	if got := Shorten("小明", *rules); got != "小明" {
		t.Fatalf("Shorten=%q", got)
	}
}
