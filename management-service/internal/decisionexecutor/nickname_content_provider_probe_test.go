package decisionexecutor

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSingleNicknameContentProviderProbe(t *testing.T) {
	if os.Getenv("SINGLE_NICKNAME_CONTENT_PROBE") != "1" {
		t.Skip("explicit paid probe")
	}
	store, err := db.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	g := &nicknameProbeGateway{store: store, gateway: agentgateway.NewFromEnv().WithSpeechModels(store)}
	w := &Worker{store: store, agent: g}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rows := []map[string]any{}
	for _, name := range []string{"芬芬", "小陈***隐藏部分", "123456789", "我爱你全家努力打拼"} {
		item := decisionItem{Nicknames: []string{name}}
		w.planNicknameAddresses(ctx, model.LiveRuntimeSession{TenantID: 14, RoomID: 26}, &item)
		alias := item.PromptNicknames[0]
		rows = append(rows, map[string]any{"original": name, "address": alias})
		if strings.Contains(alias, "隐藏") || strings.ContainsAny(alias, "*＊") || nicknameInput(0, alias).Digits > 4 || len([]rune(alias)) > 10 {
			t.Errorf("unsafe address")
		}
	}
	for _, call := range g.calls {
		request := call["request"].(agentgateway.Request)
		last := request.Messages[len(request.Messages)-1].Content
		if strings.Contains(last, "\"index\"") || strings.Contains(last, "隐藏部分") {
			t.Errorf("internal index or hidden nickname sent")
		}
	}
	if len(g.calls) != 4 {
		t.Errorf("planner was not exercised")
	}
	raw, _ := json.MarshalIndent(map[string]any{"passed": !t.Failed(), "rows": rows, "calls": g.calls, "tts": false, "played": false}, "", "  ")
	if os.WriteFile(os.Getenv("SINGLE_NICKNAME_CONTENT_OUTPUT"), raw, 0600) != nil {
		t.Fatal("archive failed")
	}
}
