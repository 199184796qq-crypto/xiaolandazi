package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"os"
	"sync"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

func TestDirectionNoveltyOwnsOnlyOneContentDecision(t *testing.T) {
	f, err := parseDirectionNovelty("重复\n只是把已有的存放时长选择换了称呼")
	if err != nil || f.Novel || f.Reason == "" {
		t.Fatal(f, err)
	}
	if _, err := parseDirectionNovelty(`{"id":"made-up","novel":true}`); err == nil {
		t.Fatal("model relation table accepted")
	}
}

func TestSupplementFilteringKeepsRawHistoryAndOldSupplyOnFeedbackFailure(t *testing.T) {
	server := &Server{}
	state := &anchorcycle.State{Status: "ready"}
	previous := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{{ID: "old", Action: "按存放时长选包装"}}}
	plan := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{{ID: "same", Action: "按存放时长选包装"}, {ID: "unconfirmed", Action: "选择加热还是直接吃"}}}
	// No policy/store/provider: the distinct candidate must fail before any
	// unconstrained call, while the exact duplicate needs no model request.
	server.filterAngleSupplement(context.Background(), state, agentgateway.SpeechModel{}, plan, previous)
	if len(plan.Cards) != 0 || len(plan.GeneratedCards) != 2 || len(plan.DirectionSelections) != 2 || len(previous.Cards) != 1 || state.Status != "ready" {
		t.Fatal("history lost or existing supply stopped", plan, state)
	}
}

func TestAngleDirectionNoveltyProviderProbe(t *testing.T) {
	if os.Getenv("ANGLE_NOVELTY_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid novelty comparison required")
	}
	var input []struct {
		Name  string            `json:"name"`
		State anchorcycle.State `json:"state"`
	}
	var supplement struct {
		Rows []struct {
			Name   string   `json:"name"`
			Old    []string `json:"initial_direction_texts"`
			Rounds []struct {
				Cards []anchorcycle.AngleHook `json:"new_cards"`
			} `json:"rounds"`
		} `json:"rows"`
	}
	raw, err := os.ReadFile(os.Getenv("ANGLE_EXPANSION_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("input invalid")
	}
	raw, err = os.ReadFile(os.Getenv("ANGLE_SUPPLEMENT_OUTPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &supplement) != nil {
		t.Fatal("supplement invalid")
	}
	policy, source, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id, err := store.ResolveTextRole(context.Background(), "mainline_angle_plan")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.ResolveSpeechModel(context.Background(), id)
	if err != nil || profile == nil {
		t.Fatal("planner missing")
	}
	server := &Server{store: store}
	rows := []map[string]any{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, 4)
	for _, topic := range supplement.Rows {
		var state anchorcycle.State
		for _, c := range input {
			if c.Name == topic.Name {
				state = c.State
			}
		}
		state.PlanID = 0
		state.RunID = "offline-angle-expansion-novelty-" + anchorcycle.ID()
		state.P0PolicyMD = ""
		if state.InstallP0(policy, source) != nil {
			t.Fatal("P0 unavailable")
		}
		for ri, r := range topic.Rounds {
			for ci, c := range r.Cards {
				wg.Add(1)
				go func(name string, round, ordinal int, state anchorcycle.State, candidate anchorcycle.AngleHook, old []string) {
					defer wg.Done()
					limit <- struct{}{}
					defer func() { <-limit }()
					feedback, e := server.directionNovelty(context.Background(), &state, *profile, candidate.Action, old)
					row := map[string]any{"name": name, "round": round, "ordinal": ordinal, "run_id": state.RunID, "candidate": candidate, "feedback": feedback, "request_success": e == nil}
					if e != nil {
						row["error"] = e.Error()
						t.Errorf("novelty feedback failed")
					}
					mu.Lock()
					rows = append(rows, row)
					mu.Unlock()
				}(topic.Name, ri+1, ci+1, state, c, topic.Old)
			}
		}
	}
	wg.Wait()
	raw, _ = json.MarshalIndent(map[string]any{"request_test_passed": !t.Failed(), "model": profile.Model, "text_only": true, "plan_assets_unchanged": true, "p0_policy_sha": derivedHash(policy), "rows": rows}, "", "  ")
	if err = os.WriteFile(os.Getenv("ANGLE_NOVELTY_OUTPUT"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
