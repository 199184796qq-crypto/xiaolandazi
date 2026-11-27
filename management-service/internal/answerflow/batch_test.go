package answerflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
)

func batchCandidates() []Candidate {
	return []Candidate{
		{ID: "U01", Input: Input{Question: "那1号呢", Recent: []Turn{{Question: "甲自己的历史", Answer: "甲旧答"}}, Context: []Message{{Role: "user", Text: "甲对话"}}}, ExpiresAt: time.Now().Add(time.Minute)},
		{ID: "U02", Input: Input{Question: "那2号呢", Recent: []Turn{{Question: "乙自己的历史", Answer: "乙旧答"}}, Context: []Message{{Role: "user", Text: "乙对话"}}}, ExpiresAt: time.Now().Add(time.Minute)},
	}
}
func classifiedJSON() string {
	return `{"items":[{"item_id":"U02","relevant":true,"priority":"high","reason":"需要及时回应","intent":{"meaning":"乙询问价格","asks":[{"text":"价格"}]}},{"item_id":"U01","relevant":true,"priority":"normal","reason":"一般咨询","intent":{"meaning":"甲询问价格","asks":[{"text":"价格"}]}}]}`
}

func TestBatchClassifiesOnceAnswersOneWithOnlyOwnersHistory(t *testing.T) {
	s := fixtureService()
	calls, embeds := 0, 0
	orig := s.Embed
	s.Embed = func(ctx context.Context, q []string) (Embeddings, error) {
		if calls != 1 || len(q) != 1 || q[0] != "价格" {
			t.Fatal("expected selected viewer's ask after intent", q, calls)
		}
		embeds++
		return orig(ctx, q)
	}
	s.Complete = func(_ context.Context, stage Stage, m []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		if stage == BatchIntentStage {
			var payload struct {
				Items []struct {
					ID    string          `json:"item_id"`
					Input json.RawMessage `json:"input"`
				}
			}
			if err := json.Unmarshal([]byte(m[1].Content), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Items) != 2 || strings.Contains(string(payload.Items[0].Input), "乙") || strings.Contains(string(payload.Items[1].Input), "甲") ||
				!strings.Contains(string(payload.Items[0].Input), "甲对话") || !strings.Contains(string(payload.Items[1].Input), "乙对话") || strings.Contains(m[1].Content, "认可口径") {
				t.Fatal("cross-person history", m[1].Content)
			}
			return agentgateway.Response{Text: classifiedJSON()}, nil
		}
		if stage != AnswerStage || strings.Contains(m[1].Content, "甲") || !strings.Contains(m[1].Content, "乙自己的历史") {
			t.Fatal("repeated intent or wrong owner's answer", stage, m)
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"乙的标准回答"}`}, nil
	}
	out, err := s.AnswerBatch(context.Background(), Scope{14, 4}, batchCandidates())
	if err != nil || calls != 2 || embeds != 1 || out.Selected != 1 || out.Items[0].Reply != nil || out.Items[1].Reply.Answer != "乙的标准回答" {
		t.Fatal(out, err, calls, embeds)
	}
}

func TestBatchBadIdentityOrPriorityNeverPurchasesAnswer(t *testing.T) {
	for _, raw := range []string{`{"items":[]}`, strings.Replace(classifiedJSON(), "U02", "U01", 1), strings.Replace(classifiedJSON(), "U02", "U09", 1), strings.Replace(classifiedJSON(), `"high"`, `"highest"`, 1), strings.Replace(classifiedJSON(), `"relevant":true,`, "", 1)} {
		t.Run(fmt.Sprint(len(raw))+raw[:12], func(t *testing.T) {
			s := fixtureService()
			calls := 0
			s.Complete = func(_ context.Context, stage Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
				calls++
				if stage != BatchIntentStage {
					t.Fatal("purchased answer after bad batch")
				}
				return agentgateway.Response{Text: raw}, nil
			}
			if _, err := s.AnswerBatch(context.Background(), Scope{14, 4}, batchCandidates()); err == nil || calls != 1 {
				t.Fatal(err, calls)
			}
		})
	}
}

func TestExpiredHigherPriorityIsNotSelectedAndNoReselectionAfterPayment(t *testing.T) {
	s := fixtureService()
	c := batchCandidates()
	c[1].ExpiresAt = time.Now().Add(-time.Second)
	calls := 0
	s.Complete = func(_ context.Context, stage Stage, m []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		if stage == BatchIntentStage {
			return agentgateway.Response{Text: classifiedJSON()}, nil
		}
		if strings.Contains(m[1].Content, "乙") {
			t.Fatal("expired viewer answered")
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"甲的回答"}`}, nil
	}
	out, err := s.AnswerBatch(context.Background(), Scope{14, 4}, c)
	if err != nil || out.Selected != 0 || calls != 2 {
		t.Fatal(out, err, calls)
	}
	// A failed paid answer does not select another viewer or retry.
	calls = 0
	s.Complete = func(_ context.Context, stage Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		if stage == BatchIntentStage {
			return agentgateway.Response{Text: classifiedJSON()}, nil
		}
		return agentgateway.Response{}, fmt.Errorf("unknown provider outcome")
	}
	if _, err = s.AnswerBatch(context.Background(), Scope{14, 4}, batchCandidates()); err == nil || calls != 2 {
		t.Fatal(err, calls)
	}
}

func TestNoRelevantCandidateSkipsEmbeddingAndAnswer(t *testing.T) {
	s := fixtureService()
	s.Complete = func(_ context.Context, _ Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
		return agentgateway.Response{Text: strings.ReplaceAll(classifiedJSON(), `"relevant":true`, `"relevant":false`)}, nil
	}
	s.Embed = func(context.Context, []string) (Embeddings, error) {
		t.Fatal("no selected candidate purchased embedding")
		return Embeddings{}, nil
	}
	out, err := s.AnswerBatch(context.Background(), Scope{14, 4}, batchCandidates())
	if err != nil || out.Selected != -1 {
		t.Fatal(out, err)
	}
}
