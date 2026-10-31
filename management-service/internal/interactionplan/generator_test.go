package interactionplan

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/policy"
	"strings"
	"testing"
	"time"
)

func TestSpokenBodyExtractsOnlyContentAndRejectsAnalysisOnly(t *testing.T) {
	for raw, want := range map[string]string{"意图：延期发货｜置信度：85\n歧义：否\n候选：\n澄清句：\n回答：联系客服备注，以客服确认为准。": "联系客服备注，以客服确认为准。", "回答：正常发货。": "正常发货。", "这个问题正常说明。": "这个问题正常说明。"} {
		got, err := spokenBody(raw)
		if err != nil || got != want {
			t.Fatal(raw, got, err)
		}
	}
	for _, raw := range []string{`{"intent":"延期"}`, "意图：延期发货\n置信度：85", `{"body":{"text":"不是纯文本"}}`} {
		if _, err := spokenBody(raw); err == nil {
			t.Fatal("analysis reached speech", raw)
		}
	}
}

func TestVocativeNormalizationPreservesBusinessClauses(t *testing.T) {
	cases := map[string]string{"老乡，你问的是哪里？": "你问的是哪里？", "正常次日上午发货，朋友们，特殊时间以客服确认为准。": "正常次日上午发货，特殊时间以客服确认为准。", "请身边的朋友帮忙看一下。": "请身边的朋友帮忙看一下。"}
	for input, want := range cases {
		if got := withoutAddress(input); got != want {
			t.Fatalf("%q => %q, want %q", input, got, want)
		}
	}
}

func TestOldSessionCannotEvictNewFrozenSnapshot(t *testing.T) {
	var c Cache
	newer := Snapshot{Scope: Scope{14, 29, 102, 7, 3}, Prompt: "new frozen"}
	c.Put(newer)
	c.Put(Snapshot{Scope: Scope{14, 29, 101, 7, 2}, Prompt: "late old"})
	got, ok := c.ForSession(14, 29, 102)
	if !ok || got.Prompt != newer.Prompt {
		t.Fatal("late old session evicted new live snapshot", got)
	}
}

func fixture(t *testing.T) Snapshot {
	t.Helper()
	p, err := policy.LoadGeneration()
	if err != nil {
		t.Fatal(err)
	}
	md, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSnapshot(Scope{14, 29, 566, 7, 295}, "方案事实：下单后第二天上午发货；晚发需客服备注并确认。风格自然。", p.PromptText, string(md))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestIntentAndBodyUseOnlyPlanAndCompleteP0(t *testing.T) {
	s := fixture(t)
	var requests []agentgateway.Request
	complete := func(ctx context.Context, r agentgateway.Request) (agentgateway.Response, error) {
		requests = append(requests, r)
		if r.Stage == "interaction_purpose_resolution" {
			return agentgateway.Response{Text: "意图：希望晚几天发货\n歧义：否\n回答：咱们一般下单后第二天上午发货；想晚几天发可以联系客服备注，具体以客服确认为准。"}, nil
		}
		return agentgateway.Response{Text: "意图：补充\n歧义：否\n回答：具体以客服确认为准。"}, nil
	}
	i, b, err := Generate(context.Background(), s, Input{Kind: "semantic", Text: "可以过几天发货吗", AllowClarify: true}, complete)
	if err != nil || !i.Question || b == "" || len(requests) != 1 {
		t.Fatal(i, b, err, len(requests))
	}
	for _, r := range requests {
		if len(r.Messages) < 2 || len(r.Messages) > 3 {
			t.Fatal("interaction request must be plan prompt (+ optional task) + payload")
		}
		if !strings.Contains(r.Messages[0].Content, s.P0) {
			t.Fatal("missing complete frozen P0 in interaction prompt")
		}
		if !strings.Contains(r.Messages[0].Content, "本场冻结的方案提示词") {
			t.Fatal("missing frozen plan prompt")
		}
		payload := r.Messages[len(r.Messages)-1].Content
		for _, forbidden := range []string{"tenant_id", "candidate_id", "fact_ids", "nickname", "小王"} {
			if strings.Contains(payload, forbidden) {
				t.Fatal("model received program identity", forbidden)
			}
		}
	}
}
func TestClarificationIsOneShotAndNotMissingFactsFallback(t *testing.T) {
	s := fixture(t)
	for _, allow := range []bool{true, false} {
		t.Run(map[bool]string{true: "first", false: "already_asked"}[allow], func(t *testing.T) {
			calls := 0
			fn := func(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
				calls++
				if allow {
					return agentgateway.Response{Text: "意图：地址还是订单不清楚\n歧义：是\n澄清句：您说的是新建区，还是新建订单？"}, nil
				}
				return agentgateway.Response{Text: "意图：问发货安排\n歧义：否\n回答：具体发货安排可以联系客服确认。"}, nil
			}
			i, b, err := Generate(context.Background(), s, Input{Text: "新建", AllowClarify: allow}, fn)
			if err != nil || b == "" || i.NeedClarify != allow {
				t.Fatal(i, b, err)
			}
			if calls != 1 {
				t.Fatal(calls)
			}
		})
	}
}
func TestJoinNeverRewritesFrozenBodyOrInventsAddress(t *testing.T) {
	s := fixture(t)
	body := "第二天上午发货，特殊时间以客服确认为准。"
	for _, output := range []string{"小王，" + body, "小王！" + body, "小李，" + body, "小王，保证今天发货。", "小王，谢谢你，" + body} {
		t.Run(output, func(t *testing.T) {
			got, err := Join(context.Background(), s, "小王", body, func(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
				for _, m := range r.Messages {
					if m.Role == "system" && strings.Contains(m.Content, "本场冻结的方案提示词") && !strings.Contains(m.Content, s.P0) {
						t.Fatal("missing complete frozen P0 in joining prompt")
					}
				}
				if !strings.Contains(r.Messages[0].Content, "本场冻结的方案提示词") {
					t.Fatal("join missing frozen plan prompt")
				}
				return agentgateway.Response{Text: output}, nil
			})
			if err != nil || !strings.HasSuffix(got, body) || !(strings.HasPrefix(got, "小王，") || got == "小王！"+body) {
				t.Fatal(got, err)
			}
		})
	}
}
func TestJoinTimeoutFailureAndNoAddress(t *testing.T) {
	s := fixture(t)
	body := "可以联系客服确认。"
	calls := 0
	fn := func(_ context.Context, _ agentgateway.Request) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{}, errors.New("timeout")
	}
	got, err := Join(context.Background(), s, "小王", body, fn)
	if err != nil || got != "小王，"+body {
		t.Fatal(got, err)
	}
	got, err = Join(context.Background(), s, "", body, fn)
	if err != nil || got != body || calls != 0 {
		t.Fatal(got, err, calls)
	}
}
func TestMalformedIntentAndEmptyBodyAreFailures(t *testing.T) {
	s := fixture(t)
	for _, raw := range []string{"not-json", `{"relevant":true}`, `{"relevant":true,"need_clarify":true}`} {
		_, _, err := Generate(context.Background(), s, Input{Text: "问题", AllowClarify: true}, func(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
			if r.Stage == "interaction_purpose_resolution" {
				return agentgateway.Response{Text: raw}, nil
			}
			return agentgateway.Response{}, nil
		})
		if err == nil {
			t.Fatal(raw)
		}
	}
}
func TestNoResponseDoesNotPaySecondBodyModel(t *testing.T) {
	s := fixture(t)
	n := 0
	i, b, err := Generate(context.Background(), s, Input{Text: "……"}, func(_ context.Context, _ agentgateway.Request) (agentgateway.Response, error) {
		n++
		return agentgateway.Response{Text: "意图：无意义刷屏\n歧义：否\n回答："}, nil
	})
	if err == nil || b != "" || n != 1 {
		t.Fatal(i, b, err, n)
	}
}
func TestFrozenPlanCannotBeChangedByLaterEdits(t *testing.T) {
	a := fixture(t)
	c := Cache{}
	first := c.Put(a)
	a.Prompt = "已改为今天发货"
	a.Fingerprint = "other"
	second := c.Put(a)
	if second.Prompt != first.Prompt || second.Fingerprint != first.Fingerprint {
		t.Fatal("immutable snapshot overwritten")
	}
	next := a
	next.Scope.Session++
	next.FrozenAt = time.Now()
	c.Put(next)
	if _, ok := c.ForSession(14, 29, 566); ok {
		t.Fatal("new session retained old runtime cache")
	}
	if _, ok := c.ForSession(14, 33, next.Scope.Session); ok {
		t.Fatal("cross room cache")
	}
}
func TestEnvelopeIsProgramCreatedAndIdentityBound(t *testing.T) {
	e := Envelope{Protocol: Protocol, Task: "program-owned", Epoch: "boot", Revision: 3, Tenant: 14, Room: 29, Session: 566, Plan: 7, Body: "正文。"}
	raw, _ := json.Marshal(e)
	got, ok := Decode(raw)
	if !ok || got.Task != e.Task || got.Revision != 3 {
		t.Fatal(got, ok)
	}
	if _, ok = Decode([]byte(`{"body":"模型不能创建任务关系"}`)); ok {
		t.Fatal("accepted model-shaped content as transport")
	}
}
