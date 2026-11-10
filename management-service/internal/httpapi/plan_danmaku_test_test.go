package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/model"
	"net/http/httptest"
	"strings"
	"testing"
)

type danmakuFakeEmbed struct {
	calls *int
	fail  bool
}

func (danmakuFakeEmbed) Enabled() bool { return true }
func (danmakuFakeEmbed) Model() string { return "qwen" }
func (e danmakuFakeEmbed) EmbedTexts(_ context.Context, texts []string) ([][]float32, error) {
	*e.calls++
	if e.fail {
		return nil, errors.New("fixture vector failure")
	}
	out := [][]float32{}
	for range texts {
		out = append(out, []float32{1, 0})
	}
	return out, nil
}
func TestDanmakuRoutesRequireActor(t *testing.T) {
	for _, path := range []string{"analyze", "recall"} {
		s := &Server{}
		w := httptest.NewRecorder()
		s.planDanmakuTest(w, httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/danmaku-test/"+path, nil))
		if w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
}
func TestDanmakuPipelineTwoModelsContextAndReadOnlyEvidence(t *testing.T) {
	calls := []string{}
	embeds := 0
	lib := danmakuLibrary{Links: []danmakutest.Link{{ID: 11, Name: "1号链接"}}, Words: []contextword.Entry{{Triggers: []string{"切"}, Meaning: "切块", When: "讨论商品处理", Enabled: true}}, Records: []danmakutest.Record{{Claim: model.PlanMaterialClaim{ID: 99, Topic: "切块", Content: "支持切块", EmbeddingModel: "qwen", MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "plan"}}}}, Vector: []float32{1, 0}}}}
	complete := func(_ context.Context, stage string, m []agentgateway.Message) (agentgateway.Response, error) {
		calls = append(calls, stage)
		if !strings.Contains(m[1].Content, "切块") || !strings.Contains(m[1].Content, "讨论商品处理") {
			t.Fatal("context lost", m)
		}
		if strings.Contains(m[1].Content, `"id":99`) {
			t.Fatal("model required internal identity")
		}
		text := `{"meaning":"询问1号能否切块","speech_act":"提问","operation":"answer","asks":[{"text":"1号能切吗","subjects":["1号链接"],"attribute":"切块"}],"uncertainties":[]}`
		if len(calls) == 2 {
			if !strings.Contains(m[1].Content, "支持切块") {
				t.Fatal("missing evidence")
			}
			text = `{"action":"answer","answer":"1号可以切块。"}`
		}
		return agentgateway.Response{Text: text, FinishReason: "stop"}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "1号能切吗"}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
	if err != nil || len(calls) != 2 || embeds != 1 || len(out.Recall.Hits) != 1 || out.Answer != "1号可以切块。" {
		t.Fatal(out, err, calls, embeds)
	}
}
func TestDanmakuFailureDoesNotPurchaseAnswerOrMisreportNoFacts(t *testing.T) {
	count, embeds := 0, 0
	complete := func(_ context.Context, _ string, _ []agentgateway.Message) (agentgateway.Response, error) {
		count++
		return agentgateway.Response{Text: `{"meaning":"问运费","asks":[{"text":"运费"}]}`}, nil
	}
	lib := danmakuLibrary{Records: []danmakutest.Record{{Claim: model.PlanMaterialClaim{Content: "运费", EmbeddingModel: "qwen"}, Vector: []float32{1, 0}}}}
	_, err := runDanmakuTest(context.Background(), danmakuInput{Question: "运费"}, lib, danmakuFakeEmbed{calls: &embeds, fail: true}, complete)
	if err == nil || !strings.Contains(err.Error(), "查询向量调用失败") || count != 1 || embeds != 1 {
		t.Fatal(err, count, embeds)
	}
}
func TestDanmakuEmptyLibraryGreetingAndInputLimits(t *testing.T) {
	count := 0
	complete := func(_ context.Context, _ string, _ []agentgateway.Message) (agentgateway.Response, error) {
		count++
		text := `{"meaning":"向主播打招呼","speech_act":"打招呼","asks":[]}`
		if count == 2 {
			text = `{"action":"answer","answer":"你好，欢迎来逛逛。"}`
		}
		return agentgateway.Response{Text: text}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "你好"}, danmakuLibrary{}, nil, complete)
	if err != nil || count != 2 || len(out.Recall.Notices) != 1 {
		t.Fatal(out, err)
	}
	if validateDanmakuInput(&danmakuInput{Queries: make([]string, 11)}, true) == nil || validateDanmakuInput(&danmakuInput{Question: strings.Repeat("字", 1001)}, false) == nil {
		t.Fatal("unbounded input")
	}
}

func TestDanmakuPriceFocusActualOverAnswerRegression(t *testing.T) {
	calls, embeds := 0, 0
	lib := danmakuLibrary{Links: []danmakutest.Link{{ID: 11, Name: "1"}}}
	for i, entry := range [][2]string{{"1号链接价格", "87.6元"}, {"1号链接门店价", "95.6元"}, {"门店价", "加上运费后一百多"}, {"1号链接礼品包装费", "额外加5元"}, {"1号链接规格", "900g"}, {"1号链接适用口味", "麻辣和五香"}, {"默认规格", "默认整只"}, {"1号链接商品名", "杨鸭子"}} {
		lib.Records = append(lib.Records, danmakutest.Record{Claim: model.PlanMaterialClaim{ID: int64(i + 1), Topic: entry[0], Content: entry[1], SourceQuote: entry[1], EmbeddingModel: "qwen", MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 11}}}}, Vector: []float32{1, 0}})
	}
	complete := func(_ context.Context, _ string, m []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		var payload map[string]any
		if err := json.Unmarshal([]byte(m[1].Content), &payload); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			for _, a := range payload["attributes"].([]any) {
				if strings.HasPrefix(a.(string), "1号") {
					t.Fatal("catalog object prefix not resolved", a)
				}
			}
			return agentgateway.Response{Text: `{"meaning":"询问1号商品的价格","speech_act":"提问","operation":"answer","asks":[{"text":"1号多少钱","subjects":["1"],"attribute":"价格"}],"uncertainties":[]}`}, nil
		}
		groups := payload["asks"].([]any)
		g := groups[0].(map[string]any)

		main := g["main_facts"].([]any)
		peripheral := g["peripheral_facts"].([]any)
		if len(groups) != 1 || g["focus_resolved"] != true || len(main) != 1 || main[0].(map[string]any)["content"] != "87.6元" || len(peripheral) != 7 {
			t.Fatal("main/peripheral grouping incorrect", g)
		}
		for _, content := range []string{"95.6元", "额外加5元", "加上运费后一百多"} {
			found := false
			for _, fact := range peripheral {
				found = found || fact.(map[string]any)["content"] == content
			}
			if !found {
				t.Fatal("peripheral fact was hard filtered", content, g)
			}
		}
		if _, legacy := g["facts"]; legacy {
			t.Fatal("unlabelled duplicate evidence", g)
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"1号链接价格是87.6元。"}`}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "1号多少钱"}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
	if err != nil || calls != 2 || embeds != 1 || len(out.Recall.Hits) != 8 || out.Answer != "1号链接价格是87.6元。" {
		t.Fatal(out, err, calls, embeds)
	}
}

func TestDanmakuTieredEvidenceSeparateConditionsAndUnresolvedFocus(t *testing.T) {
	for _, tc := range []struct {
		name, question, attribute string
		wantMain                  int
	}{
		{"separate prerequisite and conflict", "1号多少钱", "价格", 1},
		{"combined cost without exact attribute", "1号到手总价多少", "到手总价", 0},
		{"intent label can be corrected", "1号几天到", "价格", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, embeds := 0, 0
			lib := danmakuLibrary{Links: []danmakutest.Link{{ID: 11, Name: "1"}}}
			for i, entry := range [][2]string{{"1号链接价格", "80元"}, {"购买条件", "80元每份仅限两份起购"}, {"售价说明", "线上售价85元"}, {"到货时效", "通常三天到，具体以物流为准"}} {
				lib.Records = append(lib.Records, danmakutest.Record{Claim: model.PlanMaterialClaim{ID: int64(i + 1), Topic: entry[0], Content: entry[1], SourceQuote: "原文：" + entry[1], EmbeddingModel: "qwen", MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 11}}}}, Vector: []float32{1, 0}})
			}
			lib.Records[1].Claim.Conditions = json.RawMessage(`[{"field":"quantity","op":"gte","values":[2],"raw":"两份起购"}]`)
			lib.Records[1].Claim.Exceptions = json.RawMessage(`[{"field":"channel","op":"eq","values":["线下"],"raw":"线下除外"}]`)
			complete := func(_ context.Context, _ string, messages []agentgateway.Message) (agentgateway.Response, error) {
				calls++
				if calls == 1 {
					raw, _ := json.Marshal(danmakutest.Intent{Meaning: "核对当前问题", Asks: []danmakutest.Ask{{Text: tc.question, Subjects: []string{"1"}, Attribute: tc.attribute}}})
					return agentgateway.Response{Text: string(raw)}, nil
				}
				var payload struct {
					Asks []struct {
						Main       []map[string]any `json:"main_facts"`
						Peripheral []map[string]any `json:"peripheral_facts"`
						Resolved   bool             `json:"focus_resolved"`
					} `json:"asks"`
				}
				if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.Asks) != 1 || len(payload.Asks[0].Main) != tc.wantMain || len(payload.Asks[0].Peripheral) != 4-tc.wantMain || payload.Asks[0].Resolved != (tc.wantMain > 0) {
					t.Fatal("tier counts or fallback wrong", payload)
				}
				facts := append(payload.Asks[0].Main, payload.Asks[0].Peripheral...)
				seen := map[string]bool{}
				for _, fact := range facts {
					content := fact["content"].(string)
					if seen[content] {
						t.Fatal("duplicated evidence", fact)
					}
					seen[content] = true
					for _, key := range []string{"id", "score", "rank"} {
						if _, exists := fact[key]; exists {
							t.Fatal("internal identity/rank sent to model", fact)
						}
					}
					if content == "80元每份仅限两份起购" && (fact["conditions"] != "两份起购" || fact["exceptions"] != "线下除外" || fact["condition_status"] != "待核对" || fact["original"] != "原文：80元每份仅限两份起购") {
						t.Fatal("separate prerequisite flattened", fact)
					}
				}
				for _, content := range []string{"80元", "80元每份仅限两份起购", "线上售价85元", "通常三天到，具体以物流为准"} {
					if !seen[content] {
						t.Fatal("candidate lost", content)
					}
				}
				// This fake completion verifies request evidence, not provider reasoning.
				return agentgateway.Response{Text: `{"action":"insufficient","answer":"需要核对当前问题的完整条件。"}`}, nil
			}
			_, err := runDanmakuTest(context.Background(), danmakuInput{Question: tc.question}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
			if err != nil || calls != 2 || embeds != 1 {
				t.Fatal(err, calls, embeds)
			}
		})
	}
}

func TestDanmakuShippingExclusionAndSpecificSurchargeReachAnswer(t *testing.T) {
	calls, embeds := 0, 0
	lib := danmakuLibrary{}
	for i, entry := range [][2]string{{"运费", "免邮费"}, {"运费", "全场包邮"}, {"偏远地区空运费", "指定地区只能发空运并需补收运费"}} {
		lib.Records = append(lib.Records, danmakutest.Record{Claim: model.PlanMaterialClaim{ID: int64(i + 1), Topic: entry[0], Content: entry[1], SourceQuote: entry[1], EmbeddingModel: "qwen", MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "plan"}}}}, Vector: []float32{1, 0}})
	}
	lib.Records[1].Claim.Exceptions = json.RawMessage(`[{"field":"region","op":"in","values":["海南","新疆"],"raw":"海南、新疆除外"}]`)
	lib.Records[2].Claim.Conditions = json.RawMessage(`[{"field":"region","op":"in","values":["海南","新疆"],"raw":"海南、新疆"}]`)
	complete := func(_ context.Context, _ string, messages []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		if calls == 1 {
			return agentgateway.Response{Text: `{"meaning":"询问新疆是否包邮","asks":[{"text":"新疆包邮吗","attribute":"运费","conditions":{"region":"新疆"}}]}`}, nil
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
			t.Fatal(err)
		}
		group := payload["asks"].([]any)[0].(map[string]any)
		main, peripheral := group["main_facts"].([]any), group["peripheral_facts"].([]any)
		if len(main) != 1 || main[0].(map[string]any)["content"] != "免邮费" || len(peripheral) != 2 {
			t.Fatal("shipping boundary evidence missing", group)
		}
		statuses := map[string]any{}
		for _, raw := range peripheral {
			f := raw.(map[string]any)
			statuses[f["content"].(string)] = f["condition_status"]
		}
		if statuses["全场包邮"] != "不适用" || statuses["指定地区只能发空运并需补收运费"] != "适用" {
			t.Fatal("specific exception/surcharge lost", group)
		}
		if !strings.Contains(messages[0].Content, "无条件短句覆盖明确例外") || !strings.Contains(messages[0].Content, "不适用") {
			t.Fatal("exclusion rule missing")
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"该地区需要补收运费。"}`}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "新疆包邮吗"}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
	if err != nil || calls != 2 || embeds != 1 || len(out.Recall.Hits) != 3 {
		t.Fatal(out, err, calls, embeds)
	}
}

func TestDanmakuTieredMultiAskDoesNotBroadenObjectScope(t *testing.T) {
	calls, embeds := 0, 0
	lib := danmakuLibrary{Links: []danmakutest.Link{{ID: 11, Name: "1"}, {ID: 22, Name: "2"}}}
	for i, entry := range []struct {
		link           int64
		topic, content string
	}{{11, "价格", "1号80元"}, {22, "价格", "2号90元"}, {11, "配送", "1号指定地区配送"}, {22, "到货时效", "2号三天到"}} {
		lib.Records = append(lib.Records, danmakutest.Record{Claim: model.PlanMaterialClaim{ID: int64(i + 1), Topic: entry.topic, Content: entry.content, EmbeddingModel: "qwen", MaterialClaimDetails: model.MaterialClaimDetails{ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: entry.link}}}}, Vector: []float32{1, 0}})
	}
	complete := func(_ context.Context, _ string, messages []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		if calls == 1 {
			return agentgateway.Response{Text: `{"meaning":"问1号价格和2号到货时间","asks":[{"text":"1号多少钱","subjects":["1"],"attribute":"价格"},{"text":"2号多久到","subjects":["2"],"attribute":"到货时效"}]}`}, nil
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
			t.Fatal(err)
		}
		groups := payload["asks"].([]any)
		if len(groups) != 2 {
			t.Fatal(payload)
		}
		first, _ := json.Marshal(groups[0])
		second, _ := json.Marshal(groups[1])
		if !strings.Contains(string(first), "1号80元") || !strings.Contains(string(first), "1号指定地区配送") || strings.Contains(string(first), "2号90元") || strings.Contains(string(first), "2号三天到") {
			t.Fatal("foreign first ask evidence", string(first))
		}
		if !strings.Contains(string(second), "2号90元") || !strings.Contains(string(second), "2号三天到") || strings.Contains(string(second), "1号80元") {
			t.Fatal("foreign second ask evidence", string(second))
		}
		return agentgateway.Response{Text: `{"action":"answer","answer":"1号80元。2号通常三天到。"}`}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "1号多少钱，2号多久到"}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
	if err != nil || calls != 2 || embeds != 1 || len(out.Recall.Hits) != 4 {
		t.Fatal(out, err, calls, embeds)
	}
}
