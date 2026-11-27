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
	"livecompanion/management/internal/model"
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

func TestMixedQuestionsKeepAnswerFlowAndOriginalTaskOrder(t *testing.T) {
	inputs := httpBatchFixture(4)
	inputs[0].Kind, inputs[2].Kind = "like", "guide"
	inputs[1].EngagementToken = "room activity token"
	inputs[3].Kind = "dialogue"
	items, err := batchInputsV2("mixed-batch", inputs, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	checkGroup := func(group []interactionV2Input, items []interactionplan.BatchItem, first, second int) {
		t.Helper()
		if len(group) != 2 || len(items) != 2 || group[0].Task != inputs[first].Task || group[1].Task != inputs[second].Task || items[0].ID != "U01" || items[1].ID != "U02" {
			t.Fatalf("wrong group or identifiers: %+v, %+v", group, items)
		}
	}
	results, _, err := prepareQuestionAndBehaviorResults(inputs, items,
		func(group []interactionV2Input, items []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error) {
			checkGroup(group, items, 1, 3)
			return []interactionBatchResultV2{{Task: group[0].Task}, {Task: group[1].Task}}, nil, nil
		},
		func(group []interactionV2Input, items []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error) {
			checkGroup(group, items, 0, 2)
			return nil, nil, fmt.Errorf("behavior generation failed")
		})
	if err != nil || len(results) != len(inputs) {
		t.Fatal(results, err)
	}
	for i, result := range results {
		wantError := i == 0 || i == 2
		if result.Task != inputs[i].Task || (result.Error != "") != wantError {
			t.Fatalf("result %d was routed incorrectly: %+v", i, result)
		}
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

func TestPreparedWelcomeUsesWriterAndKeepsMixedBatchOrder(t *testing.T) {
	in := httpBatchFixture(3)
	in[0].Kind, in[2].Kind = "welcome_named", "welcome_batch"
	items, err := batchInputsV2("batch", in, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := httpBatchSnapshot(t)
	written := []string{}
	otherCalls := 0
	got, _, err := preparedWelcomeResultsV2(context.Background(), snapshot, model.LiveRuntimeSession{TenantID: 14, RoomID: 29, ID: 567}, in, items,
		func(ctx context.Context, session model.LiveRuntimeSession, task, kind string, frozen interactionplan.Snapshot) (string, error) {
			if ctx.Value(welcomeTaskKey{}) != task || session.ID != 567 || frozen.Fingerprint != snapshot.Fingerprint {
				t.Fatal("welcome task lost its frozen identity")
			}
			written = append(written, task+":"+kind)
			return "今天介绍一款南充特产。", nil
		},
		func(other []interactionV2Input, batch []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error) {
			otherCalls++
			if len(other) != 1 || other[0].Task != in[1].Task || len(batch) != 1 || batch[0].Kind != "semantic" {
				t.Fatal("welcome entered generic question batch", other, batch)
			}
			return []interactionBatchResultV2{{Task: other[0].Task, Revision: other[0].Revision, Envelope: interactionplan.Envelope{Body: "次日上午发货。"}}}, nil, nil
		})
	if err != nil || otherCalls != 1 || len(written) != 2 || len(got) != 3 {
		t.Fatal(err, otherCalls, written, got)
	}
	for _, i := range []int{0, 2} {
		if got[i].Error != "" || got[i].Envelope.Body != "今天介绍一款南充特产。" ||
			got[i].Envelope.Intent.Type != "欢迎" || got[i].Envelope.Task != in[i].Task ||
			got[i].Envelope.Epoch != in[i].Epoch || got[i].Envelope.Fingerprint != snapshot.Fingerprint {
			t.Fatal("prepared welcome did not use writer envelope", got[i])
		}
	}
	if got[1].Envelope.Body != "次日上午发货。" {
		t.Fatal("non-welcome batch result moved", got[1])
	}
}

func TestPreparedWelcomeWriterFailureNeverFallsBackToGeneric(t *testing.T) {
	in := httpBatchFixture(1)
	in[0].Kind = "welcome_named"
	items, _ := batchInputsV2("batch", in, time.Now())
	got, _, err := preparedWelcomeResultsV2(context.Background(), httpBatchSnapshot(t), model.LiveRuntimeSession{TenantID: 14, RoomID: 29, ID: 567}, in, items,
		func(context.Context, model.LiveRuntimeSession, string, string, interactionplan.Snapshot) (string, error) {
			return "", fmt.Errorf("语义库没有可用的无条件素材")
		},
		func([]interactionV2Input, []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error) {
			t.Fatal("writer failure re-entered generic model")
			return nil, nil, nil
		})
	if err != nil || len(got) != 1 || got[0].Error == "" || got[0].Envelope.Body != "" {
		t.Fatal(got, err)
	}
}
