package decisionexecutor

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/model"
)

// TestSingleInteractionStrategySkeleton 验证「单一互动」骨架阶段三兄弟的占位输出。
//
// 骨架阶段（singleInteractionStrategy）不调模型，按 mission_kind 给三种占位回应：
//   - complaint：安抚 + 交代（"别着急...处理"）
//   - reply_chat：有内容回显、无内容极简
//   - reply_question：回显确认（"帮您确认一下"）
func TestSingleInteractionStrategySkeleton(t *testing.T) {
	ctx := context.Background()
	session := model.LiveRuntimeSession{}
	s := singleInteractionStrategy{}

	t.Run("投诉带昵称安抚", func(t *testing.T) {
		item := decisionItem{
			MissionKind:     "complaint",
			resolvedAddress: "小陈",
		}
		got := s.Respond(ctx, session, item)
		if !strings.Contains(got, "小陈") || !strings.Contains(got, "处理") {
			t.Fatalf("投诉骨架应点昵称+安抚交代，实际：%q", got)
		}
	})

	t.Run("投诉无昵称用群称", func(t *testing.T) {
		got := s.Respond(ctx, session, decisionItem{MissionKind: "complaint"})
		if !strings.Contains(got, "处理") {
			t.Fatalf("投诉无昵称也应给交代，实际：%q", got)
		}
	})

	t.Run("弹幕无内容极简回应", func(t *testing.T) {
		got := s.Respond(ctx, session, decisionItem{MissionKind: "reply_chat"})
		if strings.TrimSpace(got) == "" {
			t.Fatalf("无意义弹幕应给极简回应，不能为空")
		}
		if strings.Contains(got, "帮您") || strings.Contains(got, "确认") {
			t.Fatalf("无内容弹幕不应走问答回显，实际：%q", got)
		}
	})

	t.Run("弹幕有内容回显", func(t *testing.T) {
		item := decisionItem{
			MissionKind:     "reply_chat",
			resolvedAddress: "阿芳",
			SampleQuestions: []string{"主播今天几点下播"},
		}
		got := s.Respond(ctx, session, item)
		if !strings.Contains(got, "阿芳") || !strings.Contains(got, "几点下播") {
			t.Fatalf("有内容弹幕应点昵称+回显原话，实际：%q", got)
		}
	})

	t.Run("提问带昵称回显确认", func(t *testing.T) {
		item := decisionItem{
			MissionKind:     "reply_question",
			resolvedAddress: "老周",
			SampleQuestions: []string{"这个油多少钱"},
		}
		got := s.Respond(ctx, session, item)
		if !strings.Contains(got, "老周") || !strings.Contains(got, "确认") || !strings.Contains(got, "多少钱") {
			t.Fatalf("提问骨架应点昵称+确认+回显，实际：%q", got)
		}
	})

	t.Run("提问无内容兜底", func(t *testing.T) {
		got := s.Respond(ctx, session, decisionItem{MissionKind: "reply_question"})
		if !strings.Contains(got, "确认") {
			t.Fatalf("提问无内容也应给确认兜底，实际：%q", got)
		}
	})
}

func TestStripAwkwardAnswerLeadKeepsAddressAndAnswer(t *testing.T) {
	got := stripAwkwardAnswerLead("小陈，这个问题问得特别好，我这就给你详细讲讲，3号和6号主要差在份量和配菜。")
	if got != "小陈，3号和6号主要差在份量和配菜。" {
		t.Fatalf("got %q", got)
	}
	if got := stripAwkwardAnswerLead("小陈，你问得正好，我这就帮你确认一下。"); got != "小陈。" {
		t.Fatalf("got %q", got)
	}
}

func TestDirectQuestionWithoutFactsUsesBoundaryNotGreeting(t *testing.T) {
	agent := &fakeAgent{}
	worker := &Worker{agent: agent}
	item := decisionItem{MissionKind: "reply_question", resolvedAddress: "小陈"}
	got := worker.generateSingleInteractionDirectAnswer(context.Background(), model.LiveRuntimeSession{}, item)
	if !strings.Contains(got, "小陈") || !strings.Contains(got, "还没") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "问得好") || strings.Contains(got, "我这就") {
		t.Fatalf("awkward greeting leaked: %q", got)
	}
}

func TestSingleAnswerFactsUsesSelectionOnly(t *testing.T) {
	worker := &Worker{}
	selected := decisionItem{
		SelectedContentPlan: &contentstrategy.Selection{Facts: []contentstrategy.Fact{{ID: "F1", Text: "选中事实"}}},
		FullFacts:           "全量事实",
	}
	if got := worker.singleAnswerFacts(selected); !strings.Contains(got, "F1 选中事实") || strings.Contains(got, "全量事实") {
		t.Fatalf("selected facts should win: %q", got)
	}
	// The full package is evidence, never a fallback: an empty selection must
	// reach the no-fact boundary instead of confirming something unselected.
	if got := worker.singleAnswerFacts(decisionItem{FullFacts: "全量事实"}); got != "" {
		t.Fatalf("empty selection must not fall back to the full package: %q", got)
	}
}

func TestDirectQuestionWithFactsUsesOneCallAndNoGreeting(t *testing.T) {
	agent := &fakeAgent{responses: []string{"小陈，3号和6号主要差在份量和配菜。"}}
	worker := &Worker{agent: agent}
	item := decisionItem{
		MissionKind:     "reply_question",
		SampleQuestions: []string{"3号和6号有什么区别"},
		resolvedAddress: "小陈",
		SelectedContentPlan: &contentstrategy.Selection{
			Facts: []contentstrategy.Fact{{ID: "F1", Text: "3号和6号主要差在份量和配菜"}},
		},
	}
	got := worker.generateSingleInteractionText(context.Background(), model.LiveRuntimeSession{}, item)
	if agent.calls != 1 {
		t.Fatalf("fact question should call model once, calls=%d", agent.calls)
	}
	if len(agent.requests) != 1 || !strings.Contains(agent.requests[0].Messages[1].Content, "观众问题：3号和6号有什么区别") {
		t.Fatalf("direct answer prompt missing question: %#v", agent.requests)
	}
	if !strings.HasPrefix(got, "小陈，") || !strings.Contains(got, "份量和配菜") {
		t.Fatalf("got %q", got)
	}
	for _, banned := range []string{"问得好", "我这就", "马上", "稍等"} {
		if strings.Contains(got, banned) {
			t.Fatalf("awkward phrase %q in %q", banned, got)
		}
	}
}

// TestDispatchSingleInteraction 验证「单一互动」分派：无 agent 走纯程序骨架。
func TestDispatchSingleInteraction(t *testing.T) {
	ctx := context.Background()
	session := model.LiveRuntimeSession{}

	// 无 agent 的 Worker，dispatchSingleInteraction 应回退到骨架实现。
	w := &Worker{}

	cases := []struct {
		name     string
		kind     string
		question string
		wantSub  string
	}{
		{"投诉走骨架", "complaint", "", "处理"},
		{"提问走骨架", "reply_question", "这个油多少钱", "确认"},
		{"弹幕走骨架", "reply_chat", "主播好", "主播好"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := decisionItem{MissionKind: tc.kind, SampleQuestions: []string{tc.question}}
			got := w.dispatchSingleInteraction(ctx, session, item)
			if strings.TrimSpace(got) == "" {
				t.Fatalf("分派返回空文字")
			}
			if tc.wantSub != "" && !strings.Contains(got, tc.wantSub) {
				t.Fatalf("期望输出包含 %q，实际：%q", tc.wantSub, got)
			}
		})
	}
}
