package httpapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/policy"
)

func httpBatchFixture(n int) []interactionV2Input {
	in := make([]interactionV2Input, n)
	for i := range in {
		in[i] = interactionV2Input{Tenant: 14, Room: 29, Plan: 7, Task: fmt.Sprintf("program-task-%d", i), Epoch: "boot", Revision: 1, Kind: "semantic", Text: "咨询发货", AllowClarify: true, ExpiresAt: time.Now().Add(time.Minute)}
	}
	return in
}
func httpBatchSnapshot(t *testing.T) interactionplan.Snapshot {
	t.Helper()
	p, err := policy.LoadGeneration()
	if err != nil {
		t.Fatal(err)
	}
	md, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	s, err := interactionplan.NewSnapshot(interactionplan.Scope{Tenant: 14, Room: 29, Session: 567, Plan: 7, Version: 289}, "本方案事实：下单后次日上午发货。", p.PromptText, string(md))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBatchHTTPPreparationOneThroughTenOneModelNoTTS(t *testing.T) {
	for n := 1; n <= 10; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			in := httpBatchFixture(n)
			items, err := batchInputsV2("batch", in, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			got, issues, err := preparedBatchResultsV2(context.Background(), httpBatchSnapshot(t), in, items, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
				calls++
				if strings.Contains(req.Messages[1].Content, "program-task") || strings.Contains(req.Messages[1].Content, "tenant_id") {
					t.Fatal("program identity exposed to model")
				}
				raw := ""
				for i := n - 1; i >= 0; i-- {
					raw += fmt.Sprintf("编号：U%02d\n意图：发货咨询\n类型：提问\n歧义：否\n回答：问题%c按当前方案说明。\n结束：U%02d\n", i+1, 'A'+i, i+1)
				}
				return agentgateway.Response{Text: raw}, nil
			})
			if err != nil || len(issues) != 0 || len(got) != n || calls != 1 {
				t.Fatal(got, issues, err, calls)
			}
			for i, result := range got {
				if result.Error != "" || result.Task != in[i].Task || result.Envelope.Task != in[i].Task || result.Envelope.Session != 567 || result.Envelope.Body != fmt.Sprintf("问题%c按当前方案说明。", 'A'+i) {
					t.Fatal("wrong transport binding", result)
				}
			}
		})
	}
}

func TestBatchHTTPBadScopeOrExpiredInputNeverAccessesStore(t *testing.T) {
	for _, name := range []string{"empty", "eleven", "room", "tenant", "plan", "epoch", "duplicate", "expired", "revision"} {
		t.Run(name, func(t *testing.T) {
			in := httpBatchFixture(2)
			switch name {
			case "empty":
				in = nil
			case "eleven":
				in = httpBatchFixture(11)
			case "room":
				in[1].Room++
			case "tenant":
				in[1].Tenant++
			case "plan":
				in[1].Plan++
			case "epoch":
				in[1].Epoch = "next"
			case "duplicate":
				in[1].Task = in[0].Task
			case "expired":
				in[1].ExpiresAt = time.Now().Add(-time.Second)
			case "revision":
				in[1].Revision = 0
			}
			if _, _, err := (&Server{}).prepareInteractionBatchV2(context.Background(), "batch", in); err == nil {
				t.Fatal("invalid input reached nil store")
			}
		})
	}
}

func TestBatchHTTPPartialResultOnlyFailsAffectedPerson(t *testing.T) {
	in := httpBatchFixture(2)
	items, _ := batchInputsV2("batch", in, time.Now())
	got, _, err := preparedBatchResultsV2(context.Background(), httpBatchSnapshot(t), in, items, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		return agentgateway.Response{Text: "编号：U02\n意图：发货咨询\n类型：提问\n歧义：否\n回答：次日上午发货。\n结束：U02\n"}, nil
	})
	if err != nil || got[0].Error == "" || got[1].Error != "" || got[1].Envelope.Task != in[1].Task {
		t.Fatal(got, err)
	}
}

func TestBatchModelOutputCapCannotSilentlyClipPeople(t *testing.T) {
	for _, tc := range []struct {
		n, cap, want int
		fail         bool
	}{{1, 600, 900, false}, {2, 1800, 1800, false}, {10, 9000, 9000, false}, {10, 16000, 9000, false}, {10, 2048, 9000, true}} {
		err := interactionBatchBudgetV2(tc.n, tc.cap, tc.want)
		if (err != nil) != tc.fail {
			t.Fatal(tc, err)
		}
	}
}
