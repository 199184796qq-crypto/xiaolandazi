package httpapi

import (
	"context"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
)

func TestBehaviorHTTPPreparationKeepsCountScopeAndUsesNoModel(t *testing.T) {
	inputs := httpBatchFixture(1)
	inputs[0].Kind = "order"
	inputs[0].Template = "order_count_v1"
	inputs[0].EventCount = 4
	inputs[0].Text = ""
	items, err := batchInputsV2("behavior-batch", inputs, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	got, _, err := preparedBatchResultsV2(context.Background(), httpBatchSnapshot(t), inputs, items, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{}, nil
	})
	if err != nil || calls != 0 || len(got) != 1 || got[0].Error != "" {
		t.Fatal(got, err, calls)
	}
	e := got[0].Envelope
	if e.Template != "order_count_v1" || e.EventCount != 4 || e.Session != 567 || e.Plan != 7 || e.Task != inputs[0].Task || !e.ExpiresAt.Equal(inputs[0].ExpiresAt) {
		t.Fatal("lost template contract", e)
	}
}
