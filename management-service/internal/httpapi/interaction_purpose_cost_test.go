package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
)

func TestInteractionPurposeCostBindsActualSessionOnceForBothModels(t *testing.T) {
	calls := 0
	ledger := &adapterCostStore{}
	server := adapterTestServer(t, ledger)
	session := model.LiveRuntimeSession{ID: 481, ExternalID: "run-481", TenantID: 14, RoomID: 29, Status: "running"}
	bind := interactionPurposeCostBinder(func(_ context.Context, tenant, room int64) (model.LiveRuntimeSession, error) {
		calls++
		if tenant != 14 || room != 29 {
			t.Fatal("looked up another room")
		}
		return session, nil
	}, 14, 29, 4)
	if calls != 0 {
		t.Fatal("cache-only purpose resolution fetched runtime")
	}
	for _, stage := range []string{"interaction_purpose_judge", "interaction_purpose_resolution"} {
		ctx, err := bind(context.Background())
		if err != nil {
			t.Fatal(stage, err)
		}
		cost := liveCostScope(ctx)
		exchange := ctx.Value(exchangeScopeKey{}).(appdb.ModelExchangeScope)
		if cost.TenantID != 14 || cost.RoomID != 29 || cost.PlanID != 4 || cost.SessionID != 481 || cost.RunID != "run-481" ||
			cost.Priority != livecost.PriorityNecessary || cost.Attribution == nil || cost.Attribution.Source != "interaction" || cost.Key != "" ||
			exchange.RunID != "run-481" || exchange.PlanID != 4 {
			t.Fatalf("%s lost session attribution or conflated two paid request keys: %+v %+v", stage, cost, exchange)
		}
		// Exercise the actual request/receipt adapter using local usage fixtures.
		id := int64(len(ledger.requests) + 1)
		request := agentgateway.Request{Stage: stage, Provider: "qwen", Model: "qwen3.7-flash", MaxTokens: 128,
			Messages: []agentgateway.Message{{Role: "user", Content: "purpose fixture"}}}
		if err := server.beginLiveModelCost(ctx, id, stage, "", request); err != nil {
			t.Fatal(err)
		}
		server.finishLiveModelCost(ctx, id, agentgateway.Response{Provider: request.Provider, Model: request.Model, InputTokens: 100, OutputTokens: 10}, 100, 10, nil)
		// A later room session cannot steal the second model's cost attribution.
		session.ID, session.ExternalID = 482, "run-482"
	}
	if calls != 1 {
		t.Fatal("judge and resolution used different runtime snapshots", calls)
	}
	if len(ledger.requests) != 2 || len(ledger.receipts) != 2 || ledger.requests[0].Key == ledger.requests[1].Key {
		t.Fatal("two independent purpose calls were merged or not recorded")
	}
	for _, request := range ledger.requests {
		if request.SessionID != 481 || request.RunID != "run-481" || request.Attribution == nil || request.Attribution.Source != "interaction" ||
			ledger.entries[request.Key].Status != livecost.StatusSettled || ledger.entries[request.Key].ChargedMicros <= 0 {
			t.Fatal("actual purpose ledger request lost its session or charge", request, ledger.entries[request.Key])
		}
	}
}

func TestInteractionPurposeCostRejectsUnknownOrEndedScopeBeforePaidCall(t *testing.T) {
	ended := time.Now()
	for _, tc := range []struct {
		name    string
		session model.LiveRuntimeSession
		err     error
	}{
		{"lookup_failure", model.LiveRuntimeSession{}, errors.New("database unavailable")},
		{"missing", model.LiveRuntimeSession{}, nil},
		{"stopped", model.LiveRuntimeSession{ID: 481, TenantID: 14, RoomID: 29, Status: "stopped"}, nil},
		{"ended", model.LiveRuntimeSession{ID: 481, TenantID: 14, RoomID: 29, Status: "running", EndedAt: &ended}, nil},
		{"another_room", model.LiveRuntimeSession{ID: 481, TenantID: 14, RoomID: 30, Status: "running"}, nil},
		{"another_tenant", model.LiveRuntimeSession{ID: 481, TenantID: 15, RoomID: 29, Status: "running"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			bind := interactionPurposeCostBinder(func(context.Context, int64, int64) (model.LiveRuntimeSession, error) {
				calls++
				return tc.session, tc.err
			}, 14, 29, 4)
			for n := 0; n < 2; n++ {
				ctx, err := bind(context.Background())
				if err == nil || ctx != nil {
					t.Fatal("model could run with an invented or ended billing scope", ctx, err)
				}
			}
			if calls != 1 {
				t.Fatal("failed lookup was silently retried against a future session")
			}
		})
	}
}
