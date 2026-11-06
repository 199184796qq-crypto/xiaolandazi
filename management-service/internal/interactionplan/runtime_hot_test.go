package interactionplan

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

type runtimeHotSource struct {
	sectionPromptSource
	runtime planprompt.RuntimePrompts
	err     error
}

func (s *runtimeHotSource) GetLiveAgentPlanRuntimePrompts(context.Context, int64, int64) (planprompt.RuntimePrompts, error) {
	return s.runtime, s.err
}

func TestHotRuntimePromptChangesNextGenerationAndPreservesPreparedIdentity(t *testing.T) {
	Frozen = Cache{}
	ctx := context.Background()
	s := planprompt.Sections{PersonaMD: "本店主播", FactsMD: "次日上午发货", RedLinesMD: "不编造", InteractionRequirementsMD: "旧回答要求"}
	source := &runtimeHotSource{sectionPromptSource: sectionPromptSource{prompt: s.Interaction()}, runtime: planprompt.RuntimePrompts{Sections: s, Revision: "before", Structured: true}}
	session := model.LiveRuntimeSession{TenantID: 14, RoomID: 33, ID: 20001}
	before, err := Freeze(ctx, source, session)
	if err != nil {
		t.Fatal(err)
	}
	source.runtime.Sections.InteractionRequirementsMD = "新回答要求：简短回应"
	source.runtime.Enabled = true
	source.runtime.Revision = "after"
	after, err := Freeze(ctx, source, session)
	if err != nil || !strings.Contains(after.Prompt, "新回答要求") || strings.Contains(after.Prompt, "旧回答要求") || after.Fingerprint != before.Fingerprint || after.Scope != before.Scope || after.P0 != before.P0 {
		t.Fatal("new requirements changed fixed identity or did not apply", before, after, err)
	}
	calls := 0
	_, err = GenerateBatch(ctx, after, batchFixture(1), func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		calls++
		var sent string
		for _, message := range req.Messages {
			sent += message.Content
		}
		if !strings.Contains(sent, "新回答要求") || strings.Contains(sent, "旧回答要求") {
			t.Fatal("actual model request used old rules", sent)
		}
		return agentgateway.Response{Text: blockText(batchFixture(1)[0].ID, "一般次日上午发货。")}, nil
	})
	if err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	source.err = errors.New("transient prompt read failure")
	if _, err = Freeze(ctx, source, session); err == nil {
		t.Fatal("new generation accepted an unconfirmed prompt")
	}
	prepared, err := FreezePrepared(ctx, source, session)
	if err != nil || prepared.Fingerprint != before.Fingerprint {
		t.Fatal("prepared content lost its original identity", err)
	}
	source.err = nil
	source.runtime.Sections.FactsMD = "其他方案的新事实"
	protected, err := Freeze(ctx, source, session)
	if err != nil || protected.Prompt != before.Prompt {
		t.Fatal("protected context leaked into a live snapshot", protected, err)
	}
}
