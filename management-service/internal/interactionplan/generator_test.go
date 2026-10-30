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
			return agentgateway.Response{Text: `{"relevant":true,"question":true,"meaning":"希望延迟发货","need_clarify":false}`}, nil
		}
		return agentgateway.Response{Text: "咱们一般下单后第二天上午发货；想晚几天发可以联系客服备注，具体以客服确认为准。"}, nil
	}
	i, b, err := Generate(context.Background(), s, Input{Kind: "semantic", Text: "可以过几天发货吗", AllowClarify: true}, complete)
	if err != nil || !i.Question || b == "" || len(requests) != 2 {
		t.Fatal(i, b, err, len(requests))
	}
	for _, r := range requests {
		if len(r.Messages) != 4 || !strings.Contains(r.Messages[0].Content, s.P0) || !strings.Contains(r.Messages[1].Content, s.Prompt) {
			t.Fatal("missing exact full scope/conditions/exceptions or plan")
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
				if calls == 1 {
					return agentgateway.Response{Text: `{"relevant":true,"question":true,"meaning":"歧义","need_clarify":true,"clarify":"您说的是新建区，还是新建订单？"}`}, nil
				}
				return agentgateway.Response{Text: "具体发货安排可以联系客服确认。"}, nil
			}
			i, b, err := Generate(context.Background(), s, Input{Text: "新建", AllowClarify: allow}, fn)
			if err != nil || b == "" || i.NeedClarify != allow {
				t.Fatal(i, b, err)
			}
			if allow && calls != 1 || !allow && calls != 2 {
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
				if !strings.Contains(r.Messages[0].Content, s.P0) {
					t.Fatal("join missing complete P0")
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
	if err != nil || got != body || calls != 1 {
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
		return agentgateway.Response{Text: `{"relevant":false}`}, nil
	})
	if err != nil || i.Relevant || b != "" || n != 1 {
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
