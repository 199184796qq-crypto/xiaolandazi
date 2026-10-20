package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type nicknameFixtureStore struct{ store }

func (*nicknameFixtureStore) GetRoomAddressingPreferences(context.Context, int64, int64) (model.RoomAddressingPreferences, error) {
	return model.RoomAddressingPreferences{PreferredTerms: []string{"老乡"}}, nil
}

type nicknameFixtureModel struct {
	calls    int
	identity string
	bad      bool
}

func (g *nicknameFixtureModel) ModelRoleIdentity(context.Context, string) (string, error) {
	return g.identity, nil
}
func (g *nicknameFixtureModel) Complete(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
	g.calls++
	if g.bad {
		return agentgateway.Response{Text: "无法确认\n请重新输入"}, nil
	}
	if strings.Contains(r.Messages[1].Content, "\"index\"") {
		panic("program index exposed")
	}
	return agentgateway.Response{Text: "这位老乡"}, nil
}
func TestNicknamePlanningCachesByRoleAndLeavesOriginalNamesIntact(t *testing.T) {
	modelAPI := &nicknameFixtureModel{identity: "max-one"}
	w := &Worker{store: &nicknameFixtureStore{}, agent: modelAPI}
	session := model.LiveRuntimeSession{TenantID: 14, RoomID: 26}
	item := decisionItem{Nicknames: []string{"原昵称123456789"}}
	w.planNicknameAddresses(context.Background(), session, &item)
	if item.Nicknames[0] != "原昵称123456789" || item.PromptNicknames[0] != "这位老乡" {
		t.Fatal(item)
	}
	for i := 0; i < 2; i++ {
		again := decisionItem{Nicknames: item.Nicknames}
		w.planNicknameAddresses(context.Background(), session, &again)
	}
	if modelAPI.calls != 1 {
		t.Fatal("same nickname planned repeatedly", modelAPI.calls)
	}
	modelAPI.identity = "max-two"
	next := decisionItem{Nicknames: item.Nicknames}
	w.planNicknameAddresses(context.Background(), session, &next)
	if modelAPI.calls != 2 {
		t.Fatal("model switch kept stale nickname plan")
	}
}
func TestNicknamePlanFailureUsesNeutralAddressesWithoutBlocking(t *testing.T) {
	w := &Worker{store: &nicknameFixtureStore{}, agent: &nicknameFixtureModel{bad: true}}
	item := decisionItem{Nicknames: []string{"某个名字", "另一个名字"}}
	w.planNicknameAddresses(context.Background(), model.LiveRuntimeSession{RoomID: 26}, &item)
	if len(item.PromptNicknames) != 2 || item.PromptNicknames[0] != "这位朋友" || item.contentError != nil {
		t.Fatal(item)
	}
}

func TestNicknameCacheExpiresAndFreshRulesInvalidateIt(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"interaction-nickname-plan.md", "interaction-nickname-addressing.md"} {
		raw, err := interactionpolicy.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	p0, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(dir, "compliance", "L1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "compliance", "L1", "p0-hard-block.md"), p0, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", dir)
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	modelAPI := &nicknameFixtureModel{identity: "max"}
	w := &Worker{store: &nicknameFixtureStore{}, agent: modelAPI, now: func() time.Time { return now }}
	session := model.LiveRuntimeSession{TenantID: 14, RoomID: 26}
	plan := func() {
		item := decisionItem{Nicknames: []string{"原昵称123456789"}}
		w.planNicknameAddresses(context.Background(), session, &item)
	}
	plan()
	now = now.Add(29 * time.Minute)
	plan()
	if modelAPI.calls != 1 {
		t.Fatal("cache expired before its lifetime")
	}
	now = now.Add(time.Minute)
	plan()
	if modelAPI.calls != 2 {
		t.Fatal("expired nickname kept its old address")
	}
	path := filepath.Join(dir, "interaction-nickname-addressing.md")
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	plan()
	if modelAPI.calls != 3 {
		t.Fatal("new rules reused an old address")
	}
	session.RoomID = 27
	plan()
	if modelAPI.calls != 4 {
		t.Fatal("another room reused the first room's address")
	}
}
func TestNicknameStructureAndNumericMetadata(t *testing.T) {
	for _, raw := range []string{`{"aliases":[]}`, `{"aliases":[{"index":9,"address":"朋友"}]}`, `{"aliases":[{"index":0,"address":"朋友","extra":true}]}`} {
		if _, e := decodeNicknamePlan(raw, 1); e == nil {
			t.Fatal("invalid structure accepted")
		}
	}
	in := nicknameInput(0, "小林12345678")
	if in.Chars != 10 || in.Digits != 8 || in.RawName != "小林12345678" {
		t.Fatal(in)
	}
}
func TestNicknameCooldownUsesOriginalIdentityInsteadOfSharedNeutralAlias(t *testing.T) {
	w := &Worker{recentNamed: map[int64]map[string]time.Time{26: {}}}
	w.recordUsedAddressingNames(26, []string{"这位朋友"}, "欢迎这位朋友", []string{"账号甲"})
	got, _ := w.playableAddressingNames(26, []string{"这位朋友"}, []string{"账号乙"})
	if len(got) != 1 {
		t.Fatal("neutral address suppressed unrelated person")
	}
	got, _ = w.playableAddressingNames(26, []string{"这位朋友"}, []string{"账号甲"})
	if len(got) != 0 {
		t.Fatal("original person cooldown lost")
	}
	if strings.Contains(addressingPlanGuidance(w.buildAddressingPlan(26, decisionItem{Nicknames: []string{"账号乙"}, PromptNicknames: []string{"这位朋友"}}, "natural", "", model.RoomAddressingPreferences{}, nil)), "账号乙") {
		t.Fatal("raw nickname sent to speech")
	}
}

func TestMaskedNicknameInputsOnlyExposeReadablePrefix(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"小陈***隐藏部分", "小陈"},
		{"芬芬＊隐藏部分", "芬芬"},
		{" 小 陈❤️123*隐藏部分", "小 陈"},
		{"***小陈", "这位朋友"},
		{"123456*隐藏部分", "这位朋友"},
		{"❤️*隐藏部分", "这位朋友"},
		{"Ann*隐藏部分", "Ann"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			in := nicknameInput(0, tc.raw)
			if in.RawName != tc.want || !in.Masked || in.Chars != len([]rune(tc.want)) || in.Digits != 0 {
				t.Fatalf("unsafe planner input: %#v", in)
			}
		})
	}
	plain := nicknameInput(0, "小林12345678")
	if plain.RawName != "小林12345678" || plain.Masked || plain.Digits != 8 {
		t.Fatalf("ordinary nickname changed: %#v", plain)
	}
}

func TestNicknameAddressSuffixAndShorteningHint(t *testing.T) {
	suffixCases := []struct {
		name   string
		suffix string
	}{
		{"小明", ""},
		{"老王", ""},
		{"爱吃苹果", ""},
		{"爱吃辣的小陈哥", "哥"},
		{"卖水果的刘老板", "老板"},
		{"做菜很好吃的王师傅", "师傅"},
		{"爱跳舞的丽丽姐", "姐"},
		{"湖南香辣妹", "妹"},
		{"我是你永远的家", ""}, // "家"是主题词，不是称谓，不应误判
	}
	for _, c := range suffixCases {
		if got := nicknameAddressSuffix(c.name); got != c.suffix {
			t.Errorf("nicknameAddressSuffix(%q)=%q want %q", c.name, got, c.suffix)
		}
	}

	hintCases := []struct {
		name   string
		expect string
	}{
		{"小明", "仅 2 字，直接使用全称，不要简称"},
		{"爱吃苹果", "仅 4 字，直接使用全称，不要简称"},
		{"超级爱吃苹果的人", "共 8 字，取最有辨识度的核心，最多 4 字（不机械截前 4 字）"},
		{"爱吃辣的小陈哥", "末尾带称谓「哥」，简称取核心 1-2 字再带上该称谓（如「小陈哥」「刘老板」）"},
	}
	for _, c := range hintCases {
		if got := nicknameShorteningHint(c.name); got != c.expect {
			t.Errorf("nicknameShorteningHint(%q)=%q want %q", c.name, got, c.expect)
		}
	}
}
