package answerflow

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

func vectorFact(id, link int64, attr, content string) danmakutest.Record {
	claim := model.PlanMaterialClaim{ID: id, Topic: attr, Content: content, ScopeKind: "product",
		ProductLinkID: link, EmbeddingModel: "vectors", SourceQuote: "审核原文：" + content}
	if link == 0 {
		claim.ScopeKind = "plan"
	}
	return danmakutest.Record{Claim: claim, Vector: []float32{1, 0}}
}

func vectorFixture() Library {
	return Library{
		Links: []danmakutest.Link{{ID: 3, Name: "3号链接"}, {ID: 5, Name: "5号链接"}, {ID: 6, Name: "6号链接"}},
		Records: []danmakutest.Record{
			vectorFact(31, 3, "价格", "9.9元"),
			vectorFact(32, 3, "口感", "清甜"),
			vectorFact(33, 3, "配送", "仅限到店"),
			vectorFact(51, 5, "价格", "19.8元"),
			vectorFact(52, 5, "口感", "酸甜"),
			vectorFact(61, 6, "价格", "29.9元"),
			vectorFact(1, 0, "退款", "未核销可退"),
		},
	}
}

func retrievedTestService(t *testing.T, lib Library, question, intent string, packet *map[string]json.RawMessage, calls *[]Stage) Service {
	p0, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	return Service{
		Load: func(context.Context, Scope) (Library, error) { return lib, nil },
		Embed: func(_ context.Context, inputs []string) (Embeddings, error) {
			vectors := make([][]float32, len(inputs))
			for i, input := range inputs {
				if strings.Contains(input, "不相关") {
					vectors[i] = []float32{0, 1}
				} else {
					vectors[i] = []float32{1, 0}
				}
			}
			return Embeddings{Model: "vectors", Vectors: vectors}, nil
		},
		ReadPrompt: func(Stage) ([]byte, error) { return []byte("prompt"), nil },
		DecodeJSON: func(value string, out any) error { return json.Unmarshal([]byte(value), out) },
		Complete: func(_ context.Context, stage Stage, msg []agentgateway.Message) (agentgateway.Response, error) {
			*calls = append(*calls, stage)
			switch stage {
			case IntentStage:
				if question != "" && !strings.Contains(msg[1].Content, question) {
					t.Fatalf("unexpected intent payload %s", msg[1].Content)
				}
				return agentgateway.Response{Text: intent}, nil
			case RetrievalRewriteStage:
				return agentgateway.Response{Text: `{"query":"仍然不相关"}`}, nil
			case AnswerStage:
				if err := json.Unmarshal([]byte(msg[1].Content), packet); err != nil {
					t.Fatal(err)
				}
				return agentgateway.Response{Text: "意图：咨询商品\n类型：提问\n歧义：否\n澄清句：\n话术：这边帮您看一下。", FinishReason: "stop"}, nil
			}
			t.Fatal(stage)
			return agentgateway.Response{}, nil
		},
		ParseAnswer: func(_ context.Context, in Input, response agentgateway.Response) (Reply, error) {
			res, err := interactionplan.ParseReply(interactionplan.Snapshot{P0Markdown: string(p0)}, interactionplan.Input{Kind: "semantic", Text: in.Question, AllowClarify: true}, response)
			return Reply{Action: "answer", Answer: res.Body}, err
		},
	}
}

func TestRetrievedComparisonKeepsBothSidesAndDeduplicatesSharedFacts(t *testing.T) {
	for _, tc := range []struct {
		name, question, attrs, scope string
		want                         []int64
	}{
		{"泛对比", "3号和5号有啥区别", `[]`, "全部", []int64{1, 31, 32, 33, 51, 52}},
		{"价格对比", "3号和5号价格区别", `["价格"]`, "明确", []int64{31, 51}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intention := `{"meaning":"比较3号和5号","category":"问题","speech_act":"提问","asks":[
				{"id":"a1","text":"` + tc.question + `","query":"3号链接对比","subjects":["3号链接"],"attributes":` + tc.attrs + `,"attribute_scope":"` + tc.scope + `","compare_group":"g1"},
				{"id":"a2","text":"` + tc.question + `","query":"5号链接对比","subjects":["5"],"attributes":` + tc.attrs + `,"attribute_scope":"` + tc.scope + `","compare_group":"g1"}]}`
			var packet map[string]json.RawMessage
			var calls []Stage
			ranks := 0
			service := retrievedTestService(t, vectorFixture(), tc.question, intention, &packet, &calls)
			config := RetrievalConfig{Rerank: func(_ context.Context, _ string, documents []string) ([]float64, error) {
				ranks++
				scores := make([]float64, len(documents))
				for i := range scores {
					scores[i] = float64(i)
				}
				return scores, nil
			}}
			result, err := service.AnalyzeRetrieved(t.Context(), Request{Scope: Scope{Tenant: 14, Plan: 9}, Input: Input{Question: tc.question}}, config)
			if err != nil {
				t.Fatal(err)
			}
			if result.Answer != "这边帮您看一下。" || !reflect.DeepEqual(calls, []Stage{IntentStage, AnswerStage}) {
				t.Fatal(result.Answer, calls)
			}
			if tc.scope == "全部" && ranks != 2 {
				t.Fatalf("rerank calls = %d", ranks)
			}
			if len(result.Retrieval.Asks) != 2 || result.Retrieval.Asks[1].Subjects[0] != "5号链接" {
				t.Fatal(result.Retrieval.Asks)
			}
			var groups []retrievedFactGroup
			if err := json.Unmarshal(packet["facts"], &groups); err != nil {
				t.Fatal(err)
			}
			ids := []int64{}
			for _, group := range groups {
				for _, fact := range group.Facts {
					ids = append(ids, fact.IDs...)
					if strings.Contains(fact.Text, "审核原文") {
						t.Fatal(fact.Text)
					}
				}
			}
			slices.Sort(ids)
			if !slices.Equal(ids, tc.want) || result.Retrieval.FactCount != len(tc.want) {
				t.Fatal(ids, tc.want, result.Retrieval)
			}
		})
	}
}

func TestRetrievedUsesCorrectedSearchButAuditsOriginal(t *testing.T) {
	var packet map[string]json.RawMessage
	var calls []Stage
	intent := `{"meaning":"问能否邮寄","category":"问题","asks":[{"id":"a1","text":"3号能邮寄吗","query":"3号邮寄","subjects":["3号"],"attributes":["邮寄"]}]}`
	lib := vectorFixture()
	// Exact attributes and catalog aliases suppress unrelated concept expansion.
	service := retrievedTestService(t, lib, "3号能邮寄吗", intent, &packet, &calls)
	config := RetrievalConfig{AttributeAliases: map[string]string{"邮寄": "配送"}}
	result, err := service.AnalyzeRetrieved(t.Context(), Request{Scope: Scope{Tenant: 14, Plan: 9}, Input: Input{Question: "三号能邮记吗"}}, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Retrieval.CorrectedQuestion != "3号能邮寄吗" || len(result.Retrieval.Edits) != 2 ||
		!strings.Contains(string(packet["customer_question"]), `"original":"三号能邮记吗"`) ||
		!slices.Equal(result.Retrieval.Asks[0].FactIDs, []int64{33}) {
		t.Fatal(result.Retrieval, string(packet["customer_question"]))
	}
}

func TestRetrievedFiltersRegionAndKeepsEmptyEvidence(t *testing.T) {
	lib := vectorFixture()
	regional := vectorFact(7, 0, "包装方式", "不抽真空")
	regional.Claim.Conditions = json.RawMessage(`[{"field":"region","op":"in","values":["川渝地区"]}]`)
	other := vectorFact(8, 0, "包装方式", "真空包装")
	other.Claim.Conditions = json.RawMessage(`[{"field":"region","op":"in","values":["其他地区"]}]`)
	lib.Records = append(lib.Records, regional, other)
	for _, tc := range []struct {
		region string
		want   int64
	}{{"上海", 8}, {"重庆", 7}} {
		t.Run(tc.region, func(t *testing.T) {
			question := tc.region + "抽不抽真空"
			intent := `{"meaning":"问包装方式","category":"问题","conditions":[{"field":"地区","value":"` + tc.region + `","scope":"all"}],"asks":[{"id":"a1","text":"` + question + `","query":"包装方式","attributes":["包装方式"]}]}`
			var packet map[string]json.RawMessage
			var calls []Stage
			service := retrievedTestService(t, lib, question, intent, &packet, &calls)
			result, err := service.AnalyzeRetrieved(t.Context(), Request{Scope: Scope{Tenant: 14, Plan: 9}, Input: Input{Question: question}}, RetrievalConfig{AttributeAliases: map[string]string{"真空": "包装方式"}})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(result.Retrieval.Asks[0].FactIDs, []int64{tc.want}) {
				t.Fatal(result.Retrieval, string(packet["facts"]))
			}
		})
	}

	var packet map[string]json.RawMessage
	var calls []Stage
	service := retrievedTestService(t, lib, "", `{"meaning":"无关问题","category":"问题","asks":[{"id":"a1","text":"这个不相关","query":"不相关"}]}`, &packet, &calls)
	result, err := service.AnalyzeRetrieved(t.Context(), Request{Scope: Scope{Tenant: 14, Plan: 9}, Input: Input{Question: "这个不相关"}}, RetrievalConfig{})
	if err != nil || result.Retrieval.FactCount != 0 || string(packet["facts"]) != "[]" ||
		!slices.Equal(calls, []Stage{IntentStage, RetrievalRewriteStage, AnswerStage}) {
		t.Fatal(result, err, calls, string(packet["facts"]))
	}
}

func TestRetrievedRestoresAnExplicitComparisonSideWithoutAddingAmounts(t *testing.T) {
	var packet map[string]json.RawMessage
	var calls []Stage
	const question = "3号和5号，6斤的价格有什么区别"
	const intent = `{"meaning":"比较两个链接的价格","category":"问题","asks":[{"id":"a1","text":"3号和5号，6斤的价格有什么区别","query":"3号价格","subjects":["3"],"attributes":["价格"],"compare_group":"comparison"}]}`
	service := retrievedTestService(t, vectorFixture(), strings.ReplaceAll(question, "，", ","), intent, &packet, &calls)
	result, err := service.AnalyzeRetrieved(t.Context(), Request{Scope: Scope{Tenant: 14, Plan: 9}, Input: Input{Question: question}}, RetrievalConfig{})
	if err != nil || len(result.Retrieval.Asks) != 2 {
		t.Fatal(result.Retrieval, err)
	}
	if !slices.Equal(result.Retrieval.Asks[0].FactIDs, []int64{31}) ||
		!slices.Equal(result.Retrieval.Asks[1].FactIDs, []int64{51}) {
		t.Fatal(result.Retrieval)
	}
}

func TestRetrievedGlobalNameUsesThePlanDirectory(t *testing.T) {
	for _, name := range []string{"全门店", "全店"} {
		t.Run(name, func(t *testing.T) {
			var packet map[string]json.RawMessage
			var calls []Stage
			question := "整个店怎么退款"
			intent := `{"meaning":"询问退款","category":"问题","asks":[{"id":"a1","text":"整个店怎么退款","query":"退款","subjects":["` + name + `"],"attributes":["退款"]}]}`
			service := retrievedTestService(t, vectorFixture(), question, intent, &packet, &calls)
			result, err := service.AnalyzeRetrieved(t.Context(), Request{Scope: Scope{Tenant: 14, Plan: 9}, Input: Input{Question: question}}, RetrievalConfig{GlobalName: name})
			if err != nil || !slices.Equal(result.Retrieval.Asks[0].FactIDs, []int64{1}) {
				t.Fatal(result.Retrieval, err)
			}
		})
	}
}

func TestRetrievedBudgetReservesBothComparisonSides(t *testing.T) {
	makeFact := func(id int64, scope string, size int) rankedFact {
		return rankedFact{hit: danmakutest.Hit{ID: id, Scope: scope}, fact: retrievalFact{text: strings.Repeat("据", size)}}
	}
	global := makeFact(1, "全局", 100)
	left := []rankedFact{global, makeFact(2, "3号", 900), makeFact(3, "3号", 3500)}
	right := []rankedFact{global, makeFact(4, "5号", 1300), makeFact(5, "5号", 800)}
	selected, used, trimmed := budgetFacts([][]rankedFact{left, right})
	if !selected[1] || !selected[2] || !selected[4] || selected[3] || used > factEvidenceBudget || !trimmed {
		t.Fatal(selected, used, trimmed)
	}
}

func TestRetrievedBatchOnlyAnalyzesSelectedViewer(t *testing.T) {
	const question = "3号和5号价格有啥区别"
	const intent = `{"meaning":"比较价格","category":"问题","speech_act":"提问","asks":[{"id":"a1","text":"3号和5号价格有啥区别","query":"3号链接价格","subjects":["3号链接"],"attributes":["价格"],"attribute_scope":"明确","compare_group":"g1"},{"id":"a2","text":"3号和5号价格有啥区别","query":"5号链接价格","subjects":["5号链接"],"attributes":["价格"],"attribute_scope":"明确","compare_group":"g1"}]}`
	for _, size := range []int{1, 2} {
		candidates := batchCandidates()[:size]
		selected := size - 1
		candidates[selected].Input.Question = question
		ownerHistory := candidates[selected].Input.Recent[0].Question
		otherHistory := batchCandidates()[1-selected].Input.Recent[0].Question
		var packet map[string]json.RawMessage
		var calls []Stage
		service := retrievedTestService(t, vectorFixture(), question, intent, &packet, &calls)
		complete := service.Complete
		service.Complete = func(ctx context.Context, stage Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
			if stage == BatchIntentStage {
				calls = append(calls, stage)
				return agentgateway.Response{Text: classifiedJSON()}, nil
			}
			if !strings.Contains(messages[1].Content, ownerHistory) || strings.Contains(messages[1].Content, otherHistory) {
				t.Fatal("wrong viewer history", stage, messages[1].Content)
			}
			return complete(ctx, stage, messages)
		}
		result, err := service.AnswerRetrievedBatch(t.Context(), Scope{Tenant: 14, Plan: 9}, candidates, RetrievalConfig{})
		want := []Stage{IntentStage, AnswerStage}
		if size > 1 {
			want = append([]Stage{BatchIntentStage}, want...)
		}
		if err != nil || result.Selected != selected || result.Items[selected].Reply == nil || !slices.Equal(calls, want) {
			t.Fatal(err, result, calls)
		}
		if size > 1 && result.Items[0].Reply != nil {
			t.Fatal("unselected viewer received a reply")
		}
		facts := string(packet["facts"])
		if !strings.Contains(facts, "9.9元") || !strings.Contains(facts, "19.8元") || strings.Contains(facts, "29.9元") {
			t.Fatal("unexpected V3 evidence", facts)
		}
	}
}
