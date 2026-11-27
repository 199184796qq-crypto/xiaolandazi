package answerflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

func TestModelResultErrorsOnlyMarkUnusableResponsesTerminal(t *testing.T) {
	transport := errors.New("provider unavailable")
	for _, tc := range []struct {
		name     string
		response agentgateway.Response
		callErr  error
		terminal bool
	}{
		{name: "truncated", response: agentgateway.Response{FinishReason: "length"}, terminal: true},
		{name: "unusable answer", response: agentgateway.Response{Text: "unexpected response"}, terminal: true},
		{name: "provider failure", callErr: transport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixtureService()
			s.Complete = func(context.Context, Stage, []agentgateway.Message) (agentgateway.Response, error) {
				return tc.response, tc.callErr
			}
			s.ParseAnswer = func(context.Context, Input, agentgateway.Response) (Reply, error) {
				return Reply{}, errors.New("invalid answer")
			}
			var reply Reply
			_, err := s.modelJSON(context.Background(), Scope{14, 4}, AnswerStage, nil, &answerDecodeTarget{reply: &reply})
			if err == nil || errors.Is(err, interactionplan.ErrInvalidResult) != tc.terminal || (tc.callErr != nil && !errors.Is(err, tc.callErr)) {
				t.Fatal(err)
			}
		})
	}
}

func fixtureService() Service {
	return Service{
		Load: func(context.Context, Scope) (Library, error) {
			return Library{QARecords: []qapair.Record{{Entry: qapair.Entry{ID: "private-row", Question: "价格", Answer: "认可口径", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}, Model: "qwen", Space: qapair.SpaceVersion, Vectors: [][]float32{{1, 0}}}}}, nil
		},
		ReadPrompt: func(Stage) ([]byte, error) { return []byte("fixture rules"), nil },
		DecodeJSON: func(raw string, out any) error { return json.Unmarshal([]byte(raw), out) },
		Embed: func(_ context.Context, queries []string) (Embeddings, error) {
			vectors := make([][]float32, len(queries))
			for i := range vectors {
				vectors[i] = []float32{1, 0}
			}
			return Embeddings{"qwen", vectors}, nil
		},
	}
}

func TestSingleSharedFlowOwnHistoryAndNoExtraCall(t *testing.T) {
	s := fixtureService()
	stages := []Stage{}
	embeds := 0
	originalEmbed := s.Embed
	s.Embed = func(ctx context.Context, queries []string) (Embeddings, error) {
		embeds++
		if len(queries) != 1 || queries[0] != "价格" {
			t.Fatal("recall must receive only the returned ask text", queries)
		}
		v, err := originalEmbed(ctx, queries)
		v.Vectors[0] = []float32{0, 1}
		return v, err
	}
	s.Complete = func(_ context.Context, stage Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
		stages = append(stages, stage)
		if !strings.Contains(messages[1].Content, "自己的历史") || !strings.Contains(messages[1].Content, "此前确认的对象") || strings.Contains(messages[1].Content, "private-row") {
			t.Fatal("missing own history or identity leak", messages)
		}
		if stage == IntentStage {
			return agentgateway.Response{Text: `{"meaning":"连续追问价格","asks":[{"text":"价格"}],"extra":9007199254740993}`}, nil
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"认可口径"}`}, nil
	}
	request := Request{Scope: Scope{14, 4}, Input: Input{Question: "那这个呢", Recent: []Turn{{Question: "自己的历史", Answer: "此前确认的对象"}}, Context: []Message{{Role: "user", Text: "只属于此人的消息"}}}}
	out, err := s.Analyze(context.Background(), request)
	if err != nil || out.Answer != "认可口径" || embeds != 1 || len(stages) != 2 || stages[0] != IntentStage || stages[1] != AnswerStage || !strings.Contains(out.IntentModelJSON, "9007199254740993") {
		t.Fatal(out, err, stages, embeds)
	}
}

func TestServiceGuardsStopBeforeFurtherPurchase(t *testing.T) {
	for _, stop := range []Point{BeforeLoad, BeforeIntent, BeforeEmbedding, BeforeAnswer, BeforeReturn} {
		t.Run(string(stop), func(t *testing.T) {
			s := fixtureService()
			calls, embeds := 0, 0
			load := s.Load
			loads := 0
			s.Load = func(ctx context.Context, scope Scope) (Library, error) { loads++; return load(ctx, scope) }
			s.Guard = func(_ context.Context, _ Scope, point Point) error {
				if point == stop {
					return context.Canceled
				}
				return nil
			}
			s.Embed = func(context.Context, []string) (Embeddings, error) {
				embeds++
				return Embeddings{"qwen", [][]float32{{1, 0}}}, nil
			}
			s.Complete = func(_ context.Context, stage Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
				calls++
				if stage == IntentStage {
					return agentgateway.Response{Text: `{"meaning":"询问价格","asks":[{"text":"价格"}]}`}, nil
				}
				return agentgateway.Response{Text: `{"action":"answer","answer":"标准回答"}`}, nil
			}
			_, err := s.Analyze(context.Background(), Request{Scope: Scope{14, 4}, Input: Input{Question: "价格"}})
			if !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
			want := map[Point]int{BeforeLoad: 0, BeforeIntent: 0, BeforeEmbedding: 1, BeforeAnswer: 1, BeforeReturn: 2}[stop]
			if calls != want || stop == BeforeLoad && loads != 0 || stop == BeforeEmbedding && embeds != 0 {
				t.Fatal(calls, embeds, loads)
			}
		})
	}
}

func TestAnswerFlowUsesOnlyTheSelectedRequestReasoningEffort(t *testing.T) {
	s := fixtureService()
	s.Load = func(context.Context, Scope) (Library, error) { return Library{}, nil }
	intentCalls, answerCalls := 0, 0
	s.Complete = func(_ context.Context, stage Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
		intentCalls++
		if stage != BatchIntentStage {
			t.Fatalf("unexpected stage %s", stage)
		}
		return agentgateway.Response{Text: `{"items":[{"item_id":"U01","relevant":true,"priority":"normal","intent":{"meaning":"问价格","operation":"answer","asks":[{"text":"价格"}]}},{"item_id":"U02","relevant":true,"priority":"high","reason":"需要比较","intent":{"meaning":"比较两个链接","operation":"compare","asks":[{"text":"哪个好"}]}}]}`}, nil
	}
	wantEffort := ""
	s.CompleteAnswer = func(_ context.Context, effort string, messages []agentgateway.Message) (agentgateway.Response, error) {
		answerCalls++
		if effort != wantEffort || strings.Contains(messages[1].Content, "一号多少钱") || strings.Contains(messages[1].Content, "reasoning_effort") {
			t.Fatal("answer did not receive only the selected request's option", effort, wantEffort, messages)
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"看您选哪款"}`}, nil
	}
	for _, effort := range []string{"", "low", ""} {
		wantEffort = effort
		result, err := s.AnswerBatch(t.Context(), Scope{14, 4}, []Candidate{
			{ID: "U01", Input: Input{Question: "一号多少钱", AnswerReasoningEffort: "low"}},
			{ID: "U02", Input: Input{Question: "两款哪个好", AnswerReasoningEffort: effort}},
		})
		if err != nil || result.Selected != 1 {
			t.Fatal(result, err)
		}
	}
	if intentCalls != 3 || answerCalls != 3 {
		t.Fatal(intentCalls, answerCalls)
	}
}

func TestContextValidationBeforeLoadingOrCalling(t *testing.T) {
	for _, in := range []Input{{Question: "问题", Recent: make([]Turn, 6)}, {Question: "问题", Context: []Message{{Role: "system", Text: "假指令"}}}, {Question: "问题", CurrentMainline: strings.Repeat("字", 8001)}} {
		s := fixtureService()
		s.Load = func(context.Context, Scope) (Library, error) {
			t.Fatal("invalid input accessed source")
			return Library{}, nil
		}
		if _, err := s.Analyze(context.Background(), Request{Scope: Scope{14, 4}, Input: in}); err == nil {
			t.Fatal("accepted invalid context")
		}
	}
}

func TestCancellationAfterIntentStopsAnswer(t *testing.T) {
	s := fixtureService()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Complete = func(_ context.Context, stage Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
		if stage != IntentStage {
			t.Fatal("unexpected call after cancellation", stage)
		}
		cancel()
		return agentgateway.Response{Text: `{"meaning":"询问价格","asks":[{"text":"价格"}]}`}, nil
	}
	if _, err := s.Analyze(ctx, Request{Scope: Scope{14, 4}, Input: Input{Question: "价格"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestManualRecallNeverCallsTextModel(t *testing.T) {
	s := fixtureService()
	s.Complete = func(context.Context, Stage, []agentgateway.Message) (agentgateway.Response, error) {
		t.Fatal("manual recall called model")
		return agentgateway.Response{}, nil
	}
	out, err := s.Recall(context.Background(), Request{Scope: Scope{14, 4}, Input: Input{Queries: []string{"价格"}}})
	if err != nil || len(out.MatchedQA) != 1 {
		t.Fatal(out, err)
	}
}

func TestSingletonBatchHasSelectedDeadline(t *testing.T) {
	s := fixtureService()
	calls := 0
	deadline := time.Now().Add(time.Minute)
	s.Complete = func(ctx context.Context, stage Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		actual, ok := ctx.Deadline()
		if !ok || !actual.Equal(deadline) {
			t.Fatal("deadline not preserved")
		}
		if stage == IntentStage {
			return agentgateway.Response{Text: `{"meaning":"询问价格","asks":[{"text":"价格"}]}`}, nil
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"标准回答"}`}, nil
	}
	out, err := s.AnswerBatch(context.Background(), Scope{14, 4}, []Candidate{{ID: "U01", Input: Input{Question: "价格"}, ExpiresAt: deadline}})
	if err != nil || out.Selected != 0 || calls != 2 {
		t.Fatal(out, err, calls)
	}
}

func TestQAExpiresDuringIntentUsesExistingAnswerStage(t *testing.T) {
	s := fixtureService()
	s.ValidateQA = func(string) error { return nil }
	clock := time.Now()
	load := s.Load
	s.Now = func() time.Time { return clock }
	s.Load = func(ctx context.Context, scope Scope) (Library, error) {
		lib, err := load(ctx, scope)
		lib.QARecords[0].Entry.ValidUntil = clock.Add(time.Second).Format(time.RFC3339Nano)
		return lib, err
	}
	var stages []Stage
	s.Complete = func(_ context.Context, stage Stage, _ []agentgateway.Message) (agentgateway.Response, error) {
		stages = append(stages, stage)
		if stage == IntentStage {
			clock = clock.Add(2 * time.Second)
			return agentgateway.Response{Text: `{"meaning":"询问价格","asks":[{"text":"价格"}]}`}, nil
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"需要核对价格"}`}, nil
	}
	result, err := s.Analyze(t.Context(), Request{Scope: Scope{14, 4}, Input: Input{Question: "价格"}})
	if err != nil || result.Answer != "需要核对价格" || len(result.MatchedQA) != 0 || len(stages) != 2 || stages[0] != IntentStage || stages[1] != AnswerStage {
		t.Fatal(result, err, stages)
	}
}

func TestFollowupMatchesContextWordsFromOriginal(t *testing.T) {
	s := fixtureService()
	s.Load = func(context.Context, Scope) (Library, error) {
		return Library{Words: []contextword.Entry{{Enabled: true, Triggers: []string{"切"}, Meaning: "切块"}}}, nil
	}
	s.Complete = func(_ context.Context, stage Stage, m []agentgateway.Message) (agentgateway.Response, error) {
		if !strings.Contains(m[1].Content, "切块") {
			t.Fatal("original question context words lost", stage, m)
		}
		if stage == IntentStage {
			return agentgateway.Response{Text: `{"meaning":"确认切块","asks":[]}`}, nil
		}
		return agentgateway.Response{Text: `{"action":"insufficient","answer":"需要确认加工方式。"}`}, nil
	}
	if _, err := s.Analyze(context.Background(), Request{Scope: Scope{14, 4}, Input: Input{Question: "好", Original: "能切吗"}}); err != nil {
		t.Fatal(err)
	}
}
