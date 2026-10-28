package decisionexecutor

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
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
	var input struct {
		Names []struct {
			Index int    `json:"index"`
			Name  string `json:"name"`
		} `json:"names"`
	}
	if err := json.Unmarshal([]byte(r.Messages[1].Content), &input); err != nil {
		return agentgateway.Response{}, err
	}
	results := make([]map[string]any, 0, len(input.Names))
	for _, name := range input.Names {
		runes := []rune(name.Name)
		base := name.Name
		if len(runes) > 4 {
			base = string(runes[:4])
		}
		if len([]rune(base)) > 2 {
			base = string([]rune(base)[:2])
		}
		short := base
		if len([]rune(short)) > 2 {
			short = string([]rune(short)[:2])
		}
		results = append(results, map[string]any{"index": name.Index, "ok": true, "base": base, "short": short})
	}
	raw, _ := json.Marshal(map[string]any{"results": results})
	return agentgateway.Response{Text: string(raw)}, nil
}

func TestNicknameProgramPathNeedsNoModel(t *testing.T) {
	modelAPI := &nicknameFixtureModel{identity: "role"}
	w := &Worker{store: &nicknameFixtureStore{}, agent: modelAPI}
	item := decisionItem{Nicknames: []string{"小明", "老王"}}
	w.planNicknameAddresses(context.Background(), model.LiveRuntimeSession{TenantID: 14, RoomID: 26}, &item)
	if modelAPI.calls != 0 {
		t.Fatalf("program-safe names called the model %d times", modelAPI.calls)
	}
	if item.PromptNicknames[0] != "小明" || item.PromptShortNames[1] != "老王" {
		t.Fatalf("names=%v shorts=%v", item.PromptNicknames, item.PromptShortNames)
	}
}

func TestNicknameGreyUsesOneBatchedCall(t *testing.T) {
	modelAPI := &nicknameFixtureModel{identity: "role"}
	w := &Worker{store: &nicknameFixtureStore{}, agent: modelAPI}
	item := decisionItem{Nicknames: []string{"挖坑不会埋", "芝士味的小宝贝"}}
	w.planNicknameAddresses(context.Background(), model.LiveRuntimeSession{TenantID: 14, RoomID: 26}, &item)
	if modelAPI.calls != 1 {
		t.Fatalf("grey batch calls=%d", modelAPI.calls)
	}
	if item.PromptNicknames[0] == "这位朋友" {
		t.Fatalf("grey name not addressed: %v", item.PromptNicknames)
	}
}

func TestNicknameGreyInvalidFallsBackNeutral(t *testing.T) {
	w := &Worker{store: &nicknameFixtureStore{}, agent: &nicknameFixtureModel{bad: true}}
	item := decisionItem{Nicknames: []string{"挖坑不会埋", "小明"}}
	w.planNicknameAddresses(context.Background(), model.LiveRuntimeSession{TenantID: 14, RoomID: 26}, &item)
	if item.PromptNicknames[0] != "这位朋友" {
		t.Fatalf("invalid grey accepted: %v", item.PromptNicknames)
	}
	if item.PromptNicknames[1] != "小明" {
		t.Fatalf("program-safe name changed: %v", item.PromptNicknames)
	}
}

func TestNicknameCacheSkipsSecondGreyCall(t *testing.T) {
	modelAPI := &nicknameFixtureModel{identity: "role"}
	w := &Worker{store: &nicknameFixtureStore{}, agent: modelAPI}
	session := model.LiveRuntimeSession{TenantID: 14, RoomID: 26}
	for i := 0; i < 3; i++ {
		item := decisionItem{Nicknames: []string{"挖坑不会埋"}}
		w.planNicknameAddresses(context.Background(), session, &item)
	}
	if modelAPI.calls != 1 {
		t.Fatalf("cache miss repeated: %d", modelAPI.calls)
	}
	modelAPI.identity = "other-role"
	item := decisionItem{Nicknames: []string{"挖坑不会埋"}}
	w.planNicknameAddresses(context.Background(), session, &item)
	if modelAPI.calls != 2 {
		t.Fatalf("role change kept stale address: %d", modelAPI.calls)
	}
}

func TestNicknameCacheExpires(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	modelAPI := &nicknameFixtureModel{identity: "role"}
	w := &Worker{store: &nicknameFixtureStore{}, agent: modelAPI, now: func() time.Time { return now }}
	session := model.LiveRuntimeSession{TenantID: 14, RoomID: 26}
	plan := func() {
		item := decisionItem{Nicknames: []string{"挖坑不会埋"}}
		w.planNicknameAddresses(context.Background(), session, &item)
	}
	plan()
	now = now.Add(29 * time.Minute)
	plan()
	if modelAPI.calls != 1 {
		t.Fatalf("cache expired early: %d", modelAPI.calls)
	}
	now = now.Add(2 * time.Minute)
	plan()
	if modelAPI.calls != 2 {
		t.Fatalf("expired address reused: %d", modelAPI.calls)
	}
}
