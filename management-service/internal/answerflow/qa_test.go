package answerflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

func qaFixtureRecord(id, question, answer string, vector []float32) qapair.Record {
	return qapair.Record{
		Entry: qapair.Entry{ID: id, Question: question, Answer: answer, Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}},
		Model: "qwen", Space: qapair.SpaceVersion, Vectors: [][]float32{vector},
	}
}

func TestQAOnlySingleAskAboveThresholdUsesHighestAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, asks, conditions, uncertainty string
		vector                              []float32
		direct                              bool
	}{
		{name: "single-direct", asks: `[{"text":"多少钱"}]`, vector: []float32{1, 0}, direct: true},
		{name: "multiple-even-if-only-one-matches", asks: `[{"text":"多少钱"},{"text":"什么时候发货"}]`, vector: []float32{1, 0}},
		{name: "multiple-identical-asks-still-generate", asks: `[{"text":"多少钱"},{"text":"多少钱"}]`, vector: []float32{1, 0}},
		{name: "between-thresholds", asks: `[{"text":"多少钱"}]`, vector: []float32{4, 3}},
		{name: "exactly-85-percent", asks: `[{"text":"多少钱"}]`, vector: []float32{17, 9, 5, 2, 1}},
		{name: "top-needs-condition", asks: `[{"text":"多少钱"}]`, conditions: "仅限会员", vector: []float32{1, 0}},
		{name: "uncertainty", asks: `[{"text":"多少钱"}]`, uncertainty: `,"uncertainties":["尚不清楚对象"]`, vector: []float32{1, 0}},
		{name: "no-ask", asks: `[]`, vector: []float32{1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixtureService()
			top := qaFixtureRecord("private-id", "标价", "标准价 80 元", tc.vector)
			top.Entry.Conditions = tc.conditions
			// Deliberately place a weaker valid match first in the catalog.
			lower := qaFixtureRecord("lower", "售价", "标准价 90 元", []float32{3, 4, 0, 0, 0})
			if tc.direct {
				lower.Vectors[0] = []float32{9, 4, 0, 0, 0}
			}
			for len(top.Vectors[0]) < 5 {
				top.Vectors[0] = append(top.Vectors[0], 0)
			}
			s.Load = func(context.Context, Scope) (Library, error) {
				return Library{QARecords: []qapair.Record{lower, top}}, nil
			}
			s.ValidateQA = func(string) error { return nil }
			var order []string
			s.Embed = func(_ context.Context, queries []string) (Embeddings, error) {
				order = append(order, "embed")
				vectors := [][]float32{{1, 0, 0, 0, 0}}
				if len(queries) == 2 {
					vectors = append(vectors, []float32{-1, 0, 0, 0, 0})
				}
				if queries[0] != "多少钱" {
					t.Fatal(queries)
				}
				return Embeddings{"qwen", vectors}, nil
			}
			s.Complete = func(_ context.Context, stage Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
				order = append(order, string(stage))
				if stage == IntentStage {
					if strings.Contains(messages[1].Content, "标准价") {
						t.Fatal(messages)
					}
					return agentgateway.Response{Text: fmt.Sprintf(`{"meaning":"问价格","asks":%s%s}`, tc.asks, tc.uncertainty)}, nil
				}
				if tc.asks != "[]" && (!strings.Contains(messages[1].Content, `"qa_pairs"`) || !strings.Contains(messages[1].Content, `"score"`)) {
					t.Fatal(messages)
				}
				if strings.Contains(messages[1].Content, "private-id") || strings.Contains(messages[1].Content, "标准价 90 元") {
					t.Fatal(messages)
				}
				return agentgateway.Response{Text: `{"action":"answer","answer":"模型组织的回答"}`}, nil
			}
			result, err := s.Analyze(t.Context(), Request{Scope: Scope{14, 4}, Input: Input{Question: "整条当前弹幕"}})
			wantOrder := []string{"intent", "embed", "answer"}
			want := "模型组织的回答"
			if tc.direct {
				wantOrder, want = wantOrder[:2], top.Entry.Answer
			} else if tc.asks == "[]" {
				wantOrder = []string{"intent", "answer"}
			}
			if err != nil || result.Answer != want || !slices.Equal(order, wantOrder) {
				t.Fatal(result, err, order)
			}
		})
	}
}

func TestQASupplementDeduplicatesAnswersAndRetainsPerAskScores(t *testing.T) {
	s := fixtureService()
	s.ValidateQA = func(string) error { return nil }
	s.Load = func(context.Context, Scope) (Library, error) {
		return Library{
			QARecords: []qapair.Record{qaFixtureRecord("same", "能否发货", "次日发货", []float32{1, 0})},
			FactCard: map[string]any{"facts_by_object": []any{map[string]any{
				"object": "全局", "facts": []any{map[string]string{"text": "冷链运输"}},
			}}},
		}, nil
	}
	s.Embed = func(_ context.Context, queries []string) (Embeddings, error) {
		if !slices.Equal(queries, []string{"能发吗", "什么时候发"}) {
			t.Fatal(queries)
		}
		return Embeddings{"qwen", [][]float32{{1, 0}, {4, 3}}}, nil
	}
	s.Complete = func(_ context.Context, stage Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
		if stage == IntentStage {
			return agentgateway.Response{Text: `{"meaning":"咨询发货","asks":[{"text":"能发吗"},{"text":"什么时候发"}]}`}, nil
		}
		var request answerRequest
		if err := json.Unmarshal([]byte(messages[1].Content), &request); err != nil {
			t.Fatal(err)
		}
		if len(request.Facts) != 1 || len(request.QAPairs) != 1 ||
			!slices.Equal(request.QAPairs[0].Asks, []answerQAAsk{{"能发吗", 1}, {"什么时候发", 0.8}}) {
			t.Fatal(messages)
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"可发，一般次日发货"}`}, nil
	}
	result, err := s.Analyze(t.Context(), Request{Scope: Scope{14, 4}, Input: Input{Question: "能发吗，什么时候发"}})
	if err != nil || result.Answer != "可发，一般次日发货" || len(result.MatchedQA) != 2 {
		t.Fatal(result, err)
	}
}

func TestQAEmbeddingErrorStillUsesFacts(t *testing.T) {
	s := fixtureService()
	s.Embed = func(context.Context, []string) (Embeddings, error) { return Embeddings{}, errors.New("暂不可用") }
	s.Complete = func(_ context.Context, stage Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
		if stage == IntentStage {
			return agentgateway.Response{Text: `{"meaning":"问发货","asks":[{"text":"怎么寄"}]}`}, nil
		}
		if strings.Contains(messages[1].Content, "qa_pairs") {
			t.Fatal(messages)
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"需要核实发货细节"}`}, nil
	}
	out, err := s.Analyze(t.Context(), Request{Scope: Scope{14, 4}, Input: Input{Question: "怎么寄"}})
	if err != nil || out.Answer != "需要核实发货细节" || len(out.MatchedQA) != 0 {
		t.Fatal(out, err)
	}
}

func TestQASupplementSharesTenSlotsAcrossAsks(t *testing.T) {
	groups := []qapair.Group{{Ask: "第一个"}, {Ask: "第二个"}}
	for i := range 12 {
		groups[0].Matches = append(groups[0].Matches, qapair.Match{ID: fmt.Sprint(i), Answer: "回答", Score: 0.9})
	}
	groups[1].Matches = []qapair.Match{{ID: "other", Answer: "第二个回答", Score: 0.7}}
	_, refs, ids := answerQAReferences(groups, nil)
	if len(refs) != 10 || !slices.Contains(ids, "other") {
		t.Fatal(ids)
	}
	groups[0].Matches[0].Answer = strings.Repeat("字", answerQAChars+1)
	_, refs, ids = answerQAReferences(groups, nil)
	if len(refs) != 10 || slices.Contains(ids, "0") {
		t.Fatal(ids)
	}
}
