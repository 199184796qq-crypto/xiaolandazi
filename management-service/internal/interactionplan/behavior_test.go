package interactionplan

import (
	"context"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func TestBehaviorTemplatesNeverCallModel(t *testing.T) {
	for _, x := range []struct {
		kind, id string
		count    int
	}{{"like", "like_v1", 1}, {"follow", "follow_v1", 10}, {"order", "order_single_v1", 1}, {"order", "order_count_v1", 4}, {"order", "order_count_v1", 1}} {
		t.Run(x.id, func(t *testing.T) {
			calls := 0
			r, err := GenerateBatch(context.Background(), fixture(t), []BatchItem{{ID: "U01", Input: Input{Kind: x.kind, Template: x.id, EventCount: x.count}}}, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
				calls++
				t.Error("called model")
				return agentgateway.Response{}, nil
			})
			if err != nil || calls != 0 || len(r.Items) != 1 || r.Items[0].Body == "" {
				t.Fatal(r, err, calls)
			}
		})
	}
}
func TestBehaviorTemplateContractRejectsForgeryAndMissingP0(t *testing.T) {
	for _, x := range []struct {
		kind, id string
		count    int
	}{{"semantic", "like_v1", 1}, {"like", "arbitrary", 1}, {"order", "order_single_v1", 4}, {"order", "order_count_v1", 0}, {"order", "order_count_v1", 8193}} {
		if _, err := BehaviorBody(x.kind, x.id, x.count); err == nil {
			t.Fatal("accepted invalid template", x)
		}
	}
	s := fixture(t)
	s.P0Markdown = ""
	if _, err := GenerateBatch(context.Background(), s, []BatchItem{{ID: "U01", Input: Input{Kind: "like", Template: "like_v1", EventCount: 1}}}, nil); err == nil {
		t.Fatal("missing P0 allowed")
	}
}
