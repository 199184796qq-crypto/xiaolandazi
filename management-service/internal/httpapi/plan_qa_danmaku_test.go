package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/answerflow"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
	"strings"
	"testing"
)

func TestQADanmakuOnlyLibraryUsesStandardAnswerAfterOneTextCall(t *testing.T) {
	calls, embeds := 0, 0
	lib := danmakuLibrary{QARecords: []qapair.Record{{Entry: qapair.Entry{ID: "internal-qa-id", Revision: 2, Question: "有点贵", Answer: "认可的价值解释", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}, Model: "qwen", Space: qapair.SpaceVersion, Vectors: [][]float32{{1, 0}}}}}
	complete := func(_ context.Context, stage string, m []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		if stage != "danmaku_test_intent" || strings.Contains(m[1].Content, "internal-qa-id") || strings.Contains(m[1].Content, "认可的价值解释") {
			t.Fatal(stage, m)
		}
		return agentgateway.Response{Text: `{"meaning":"质疑价格","asks":[{"text":"有点贵"}]}`, FinishReason: "stop"}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "有点贵"}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
	if err != nil || calls != 1 || embeds != 1 || out.Answer != "认可的价值解释" || len(out.MatchedQA) != 1 || len(out.MatchedQA[0].Matches) != 1 {
		t.Fatal(out, err, calls, embeds)
	}
}

func TestQAChangedDuringIntentPreventsReturningStaleAnswer(t *testing.T) {
	calls, embeds := 0, 0
	changed := errors.New("问答已变化")
	lib := danmakuLibrary{
		Validate:  func(context.Context) error { return changed },
		QARecords: []qapair.Record{{Entry: qapair.Entry{ID: "saved", Question: "多久发货", Answer: "次日上午发货。", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}, Model: "qwen", Space: qapair.SpaceVersion, Vectors: [][]float32{{1, 0}}}},
	}
	complete := func(_ context.Context, _ string, _ []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{Text: `{"meaning":"询问何时发货","asks":[{"text":"多久发货"}]}`}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "多久发货"}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
	if !errors.Is(err, changed) || out.Answer != "" || calls != 1 || embeds != 1 {
		t.Fatal(out, err, calls, embeds)
	}
}

func TestQAManualRecallSharesExistingQueryEmbedding(t *testing.T) {
	calls := 0
	lib := danmakuLibrary{QARecords: []qapair.Record{{Entry: qapair.Entry{ID: "approved", Question: "通用问题", Answer: "认可答案", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}, Model: "qwen", Space: qapair.SpaceVersion, Vectors: [][]float32{{1, 0}}}}}
	result, err := newDanmakuAnswerService(lib, danmakuFakeEmbed{calls: &calls}, nil).Recall(context.Background(), answerflow.Request{Scope: answerflow.Scope{Tenant: 14, Plan: 4}, Input: answerflow.Input{Queries: []string{"通用问题"}}})
	matches := result.MatchedQA
	if err != nil || calls != 1 || len(matches) != 1 || len(matches[0].Matches) != 1 {
		t.Fatal(matches, err, calls)
	}
}

func TestQAUnconfirmedConditionGoesToAnswerWithItsCondition(t *testing.T) {
	calls, embeds := 0, 0
	lib := danmakuLibrary{QARecords: []qapair.Record{{Entry: qapair.Entry{ID: "conditional", Question: "能包邮吗", Answer: "需要条件的肯定答案", Conditions: "仅限某地区", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}, Model: "qwen", Space: qapair.SpaceVersion, Vectors: [][]float32{{1, 0}}}}}
	complete := func(_ context.Context, _ string, m []agentgateway.Message) (agentgateway.Response, error) {
		calls++
		text := `{"meaning":"询问运费","asks":[{"text":"能包邮吗"}]}`
		if calls == 2 {
			if !strings.Contains(m[1].Content, `"conditions":"仅限某地区"`) ||
				!strings.Contains(m[1].Content, `"score":1`) || !strings.Contains(m[0].Content, "必要条件未确认") {
				t.Fatal(m)
			}
			text = `意图：理解当前诉求
类型：提问
歧义：是
话术：
澄清句：准备寄到哪里？`
		}
		return agentgateway.Response{Text: text}, nil
	}
	out, err := runDanmakuTest(context.Background(), danmakuInput{Question: "能包邮吗"}, lib, danmakuFakeEmbed{calls: &embeds}, complete)
	if err != nil || calls != 2 || len(out.MatchedQA) != 1 || out.MatchedQA[0].Matches[0].Status != "待核对" || out.Action != "clarify" {
		t.Fatal(out, err, calls)
	}
}
