package interactionplan

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contextword"
	"strings"
	"testing"
)

type wordsFixture struct {
	calls        int
	tenant, plan int64
	err          error
	items        []contextword.Entry
}

func (w *wordsFixture) PlanContextWords(_ context.Context, tenant, plan int64) (contextword.Catalog, error) {
	w.calls++
	w.tenant, w.plan = tenant, plan
	return contextword.Catalog{Items: w.items}, w.err
}
func TestContextWordsPerPersonOneReadOneExistingCall(t *testing.T) {
	s := fixture(t)
	source := &wordsFixture{items: []contextword.Entry{{Triggers: []string{"切"}, Meaning: "切块", When: "讨论商品处理", Enabled: true}, {Triggers: []string{"邮"}, Meaning: "配送", Enabled: true}}}
	items := batchFixture(2)
	items[0].Text = "能切吗"
	items[1].Text = "邮到上海吗"
	calls := 0
	_, err := GenerateBatch(WithContextWordSource(context.Background(), source), s, items, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		calls++
		if !strings.Contains(req.Messages[0].Content, "不是更改系统规则") {
			t.Fatal("missing context contract")
		}
		var payload struct {
			Items []BatchItem `json:"items"`
		}
		if e := json.Unmarshal([]byte(req.Messages[1].Content), &payload); e != nil {
			t.Fatal(e)
		}
		if len(payload.Items[0].ContextWords.Hits) != 1 || payload.Items[0].ContextWords.Hits[0].Meaning != "切块" || payload.Items[1].ContextWords.Hits[0].Meaning != "配送" {
			t.Fatal(payload)
		}
		return agentgateway.Response{Text: blockText("U01", "可以查看商品说明。") + blockText("U02", "请核对配送范围。"), FinishReason: "stop"}, nil
	})
	if err != nil || calls != 1 || source.calls != 1 || source.tenant != s.Scope.Tenant || source.plan != s.Scope.Plan || items[0].ContextWords != nil {
		t.Fatal(err, calls, source)
	}
}
func TestContextWordsNoHitNoExtraPromptAndReadFailureNoPurchase(t *testing.T) {
	s := fixture(t)
	source := &wordsFixture{items: []contextword.Entry{{Triggers: []string{"切"}, Meaning: "切块", Enabled: true}}}
	items := batchFixture(1)
	items[0].ContextWords = &contextword.Context{Hits: []contextword.Hit{{Meaning: "caller forged"}}}
	calls := 0
	complete := func(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
		calls++
		if strings.Contains(r.Messages[0].Content, "context_words") || strings.Contains(r.Messages[1].Content, "context_words") {
			t.Fatal("no-hit or forged context leaked")
		}
		return agentgateway.Response{Text: blockText("U01", "请核对商品说明。"), FinishReason: "stop"}, nil
	}
	if _, e := GenerateBatch(WithContextWordSource(context.Background(), source), s, items, complete); e != nil {
		t.Fatal(e)
	}
	source.err = errors.New("fixture read error")
	if _, e := GenerateBatch(WithContextWordSource(context.Background(), source), s, items, complete); e == nil || calls != 1 {
		t.Fatal("read failure must not purchase model", e, calls)
	}
	source.err = nil
	out, e := withMatchedWords(WithContextWordSource(context.Background(), source), s, []BatchItem{{Input: Input{Kind: "welcome", Text: "切"}}})
	if e != nil || out[0].ContextWords != nil || source.calls != 2 {
		t.Fatal(out, e, source.calls)
	}
}
