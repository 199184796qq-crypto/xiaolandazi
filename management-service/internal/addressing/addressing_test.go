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
		{"我是你爸爸", StatusGrey, "我是你爸爸"},
		{"叫我老板", StatusDropped, ""},
		{"12345678", StatusRewritten, "一二三四"},{"8412836552", StatusRewritten, "八四一二"},{"用户6008101088461梅梅", StatusRewritten, "梅梅"},
		{"小林12345678", StatusRewritten, "小林"},
		{"小陈***隐藏部分", StatusRewritten, "小陈"},
		{"***隐藏部分", StatusDropped, ""},
		{"🎉🎉🎉", StatusDropped, ""},
		{"小陈🎉", StatusRewritten, "小陈"},
		{"超级爱吃苹果的人", StatusGrey, "超级爱吃苹果的人"},
		{"Ann Lee", StatusSafe, "Ann Lee"},
		// Real production names that exposed rule gaps on 2026-10-28.
		{"妈妈", StatusGrey, "妈妈"},
		{"爸爸", StatusGrey, "爸爸"},
		{"老公", StatusGrey, "老公"},
		{"二楠妈妈", StatusSafe, "二楠妈妈"},
		{"用户4001612171583", StatusRewritten, "用户四零零"},{"用户7141435402865", StatusRewritten, "用户七一四"},
		{"卟过如此ヤ", StatusRewritten, "卟过如此"},
		{"雅迪电动车(丰收路专卖店)", StatusDropped, ""},
		{"拖拉机出租，需要起箱，打田打地联系我", StatusDropped, ""},
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
		{UserID: "2", RawName: "滚"},
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

func TestDuplicateAccountAddressesAreExtended(t *testing.T) {
	rules := testRules(t)
	plan := Plan{Picked: []Picked{
		{Viewer: Viewer{UserID: "1", RawName: "用户2616106374192"}, Decision: Decision{Reason: "generic_account_digits"}, Text: "用户二六一"},
		{Viewer: Viewer{UserID: "2", RawName: "用户2613503868174"}, Decision: Decision{Reason: "generic_account_digits"}, Text: "用户二六一"},
	}}
	resolveDuplicateAddresses(&plan, rules)
	if plan.Picked[0].Text == plan.Picked[1].Text {
		t.Fatalf("collision kept: %s", plan.Picked[0].Text)
	}
	if !strings.HasPrefix(plan.Picked[0].Text, "用户二六一") {
		t.Fatalf("address changed base: %s", plan.Picked[0].Text)
	}
}
