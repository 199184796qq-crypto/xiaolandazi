package httpapi

import (
	"context"
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
