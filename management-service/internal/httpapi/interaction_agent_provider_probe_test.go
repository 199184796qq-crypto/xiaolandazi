package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/decisionexecutor"
)

// Ordinary paid text API only. Reads an explicit plan, preserves model exchange
// logs, and never starts a session, synthesizes audio, adopts or publishes assets.
func TestInteractionAgentProviderProbe(t *testing.T) {
	if os.Getenv("INTERACTION_AGENT_PROBE") != "1" {
		t.Skip("explicit operational probe required")
	}
	tenant, _ := strconv.ParseInt(os.Getenv("INTERACTION_AGENT_TENANT"), 10, 64)
	room, _ := strconv.ParseInt(os.Getenv("INTERACTION_AGENT_ROOM"), 10, 64)
	planID, _ := strconv.ParseInt(os.Getenv("INTERACTION_AGENT_PLAN"), 10, 64)
	if tenant <= 0 || room <= 0 || planID <= 0 {
		t.Fatal("explicit tenant/room/plan required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration database unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	plan, err := store.GetLiveAgentPlan(ctx, tenant, planID)
	if err != nil || plan.Status != "active" {
		t.Fatal("selected plan unavailable")
	}
	server := &Server{store: store}
	worker := decisionexecutor.New(&planSpeechTestStore{Store: store, tenantID: tenant, roomID: room, plan: plan}, nil, server.speechGateway(), nil)
	recent := []decisionexecutor.InteractionRecentAnswer{}
	for _, question := range []string{"放冰箱不", "放冰箱不", "发什么快递？"} {
		result, err := worker.SimulateInteraction(ctx, tenant, room, question, "", recent)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result)
		t.Log(string(encoded))
		if strings.TrimSpace(result.Reply) == "" {
			t.Error("reply missing")
		}
		if utf8.RuneCountInString(result.Reply) > 120 {
			t.Error("simple answer too long")
		}
		if strings.Contains(result.Reply, "空气炸锅") || strings.Contains(result.Reply, "零食") || strings.Contains(result.Reply, "现卤现炸") {
			t.Error("unrelated content expanded")
		}
		for _, wording := range []string{"宝宝", "哥哥姐", "放心拍", "品质有保障", "1号链接", "2号链接"} {
			if strings.Contains(result.Reply, wording) {
				t.Error("simulation inherited unrelated presentation", wording)
			}
		}
		if question == "发什么快递？" && (utf8.RuneCountInString(result.Reply) > 30 || strings.Contains(result.Reply, "到货")) {
			t.Error("company-only question expanded")
		}
		recent = append(recent, decisionexecutor.InteractionRecentAnswer{Question: question, Reply: result.Reply})
	}
}
