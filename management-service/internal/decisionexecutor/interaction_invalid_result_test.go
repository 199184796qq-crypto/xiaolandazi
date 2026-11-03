package decisionexecutor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionplan"
)

type recoveryAgent struct {
	*fakeAgent
	reason string
}

func (a recoveryAgent) Complete(ctx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
	r, err := a.fakeAgent.Complete(ctx, req)
	r.FinishReason = a.reason
	return r, err
}

type recoveryCore struct {
	*fakeCore
	pending bool
	id      string
	removed int
}

func (c *recoveryCore) DoRoom(ctx context.Context, tenant, room int64, method, path string, query url.Values, body any) (*http.Response, error) {
	raw := ""
	if method == http.MethodGet && strings.HasSuffix(path, "/agent-decisions") {
		raw = fmt.Sprintf(`{"queue":[{"id":%q}]}`, c.id)
	} else if method == http.MethodDelete && strings.HasSuffix(path, "/agent-decisions/"+c.id) {
		c.pending = false
		c.removed++
		raw = `{"ok":true}`
	} else if !c.pending && strings.HasSuffix(path, "/agent-decisions/claim") {
		raw = `{"claimed":false}`
	}
	if raw != "" {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}}, nil
	}
	return c.fakeCore.DoRoom(ctx, tenant, room, method, path, query, body)
}

func manualRecoveryClaim(id string) string {
	return fmt.Sprintf(`{"claimed":true,"item":{"id":%q,"topic":"manual","mission_kind":"reply_question","manual_action":"quick","sample_questions":["哪天发货"]}}`, id)
}

func TestManualUnrecoverableResultIsRemovedWithoutBlockingNextTask(t *testing.T) {
	w, _, base, agent, tts, session, _ := v2Fixture(t, 10101)
	core := &recoveryCore{fakeCore: base, pending: true, id: "bad-item"}
	w.core = core
	w.agent = recoveryAgent{agent, "stop"}
	base.claimRaw = manualRecoveryClaim(core.id)
	agent.responses = []string{
		"编号：U01\n意图：问发货\n类型：提问\n歧义：否\n话术：",         // cannot invent the missing body
		"编号：U01\n意图：问发货\n类型：提问\n歧义：否\n话术：第二天上午发货。", // valid next task, missing only footer
	}
	err := w.processRoom(context.Background(), session)
	var terminal v2TaskFailure
	if !errors.As(err, &terminal) || !errors.Is(terminal.error, interactionplan.ErrInvalidResult) || core.removed != 1 || core.pending || core.releases != 0 || tts.calls != 0 {
		t.Fatal("bad content must leave the execution queue without TTS/release/backoff", err, core.removed, core.releases, tts.calls)
	}
	if err := w.processRoom(context.Background(), session); err != nil || agent.calls != 1 {
		t.Fatal("terminal task purchased generation again", err, agent.calls)
	}
	core.pending, core.id = true, "good-item"
	base.claimRaw = manualRecoveryClaim(core.id)
	if err := w.processRoom(context.Background(), session); err != nil || agent.calls != 2 || tts.calls != 1 || core.dispatches != 1 || core.removed != 1 {
		t.Fatal("bad predecessor blocked a valid recovered reply", err, agent.calls, tts.calls, core.dispatches)
	}
	if !strings.Contains(tts.last.Text, "第二天上午发货。") || strings.Contains(tts.last.Text, "编号") || strings.Contains(tts.last.Text, "意图") {
		t.Fatal("protocol metadata entered voice", tts.last.Text)
	}
}

func TestManualTruncatedReplyDoesNotGenerateTTS(t *testing.T) {
	w, _, base, agent, tts, session, _ := v2Fixture(t, 10102)
	core := &recoveryCore{fakeCore: base, pending: true, id: "truncated"}
	w.core, w.agent = core, recoveryAgent{agent, "length"}
	base.claimRaw = manualRecoveryClaim(core.id)
	agent.responses = []string{"编号：U01\n意图：问发货\n类型：提问\n歧义：否\n话术：第二天上午发货。"}
	err := w.processRoom(context.Background(), session)
	var terminal v2TaskFailure
	if !errors.As(err, &terminal) || !errors.Is(terminal.error, interactionplan.ErrInvalidResult) || tts.calls != 0 || core.removed != 1 || core.releases != 0 {
		t.Fatal(err, tts.calls, core.removed, core.releases)
	}
}

func TestManualProviderFailureKeepsExistingRetryPolicy(t *testing.T) {
	w, _, base, agent, tts, session, _ := v2Fixture(t, 10103)
	core := &recoveryCore{fakeCore: base, pending: true, id: "transport-error"}
	w.core = core
	base.claimRaw = manualRecoveryClaim(core.id)
	agent.err = errors.New("provider temporarily unavailable")
	err := w.processRoom(context.Background(), session)
	if err == nil || errors.Is(err, interactionplan.ErrInvalidResult) || core.removed != 0 || core.releases != 1 || tts.calls != 0 {
		t.Fatal("a transport failure must not be treated as malformed content", err, core.removed, core.releases, tts.calls)
	}
}
