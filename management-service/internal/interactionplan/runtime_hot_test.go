package interactionplan

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

type runtimeHotSource struct {
	sectionPromptSource
	runtime planprompt.RuntimePrompts
	err     error
}

func (s *runtimeHotSource) GetLiveAgentPlanInteractionSections(context.Context, int64, int64) (planprompt.Sections, error) {
	return s.runtime.Sections, s.err
}

func TestFrozenInteractionIgnoresRetiredPromptChanges(t *testing.T) {
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
	if err != nil || after.Prompt != before.Prompt || strings.Contains(after.Prompt, "回答要求") || strings.Contains(after.Prompt, "次日上午发货") || after.Fingerprint != before.Fingerprint {
		t.Fatal("retired fields entered the interaction", before, after, err)
	}
	source.err = errors.New("transient prompt read failure")
	if frozen, err := Freeze(ctx, source, session); err != nil || frozen.Fingerprint != before.Fingerprint {
		t.Fatal("generation lost its frozen context", err)
	}
	prepared, err := FreezePrepared(ctx, source, session)
	if err != nil || prepared.Fingerprint != before.Fingerprint {
		t.Fatal("prepared content lost its original identity", err)
	}
}
