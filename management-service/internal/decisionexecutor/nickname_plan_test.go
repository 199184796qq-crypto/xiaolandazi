package decisionexecutor

import (
	"context"
	"encoding/json"
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
		return agentgateway.Response{Text: "not json"}, nil
	}
	var in struct {
		Names []nicknamePlanInput `json:"names"`
	}
	json.Unmarshal([]byte(r.Messages[1].Content), &in)
	rows := []map[string]any{}
	for _, n := range in.Names {
		rows = append(rows, map[string]any{"index": n.Index, "address": "这位老乡"})
	}
	raw, _ := json.Marshal(map[string]any{"aliases": rows})
	return agentgateway.Response{Text: string(raw)}, nil
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
	if strings.Contains(addressingPlanGuidance(w.buildAddressingPlan(26, decisionItem{Nicknames: []string{"账号乙"}, PromptNicknames: []string{"这位朋友"}}, "natural", "", model.RoomAddressingPreferences{})), "账号乙") {
		t.Fatal("raw nickname sent to speech")
	}
}
