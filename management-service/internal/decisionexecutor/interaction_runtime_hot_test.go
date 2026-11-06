package decisionexecutor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/planprompt"
)

type hotRuntimeWorkerStore struct {
	*v2Store
	runtime planprompt.RuntimePrompts
	err     error
}

func (s *hotRuntimeWorkerStore) GetLiveAgentPlanRuntimePrompts(context.Context, int64, int64) (planprompt.RuntimePrompts, error) {
	return s.runtime, s.err
}

func TestPreparedV2WorkerStillDispatchesOnceAfterHotPromptReadFailure(t *testing.T) {
	w, original, core, agent, tts, session, envelope := v2Fixture(t, 18300)
	sections := planprompt.Sections{FactsMD: "第二天上午发货", InteractionRequirementsMD: "简短回答"}
	original.prompt = sections.Interaction()
	store := &hotRuntimeWorkerStore{v2Store: original, runtime: planprompt.RuntimePrompts{Sections: sections, Structured: true}}
	w.store = store
	interactionplan.Frozen = interactionplan.Cache{}
	frozen, err := interactionplan.Freeze(context.Background(), store, session)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Fingerprint = frozen.Fingerprint
	claimV2(core, envelope)
	store.err = errors.New("prompt read unavailable after generation")
	if err = w.processRoom(context.Background(), session); err != nil {
		t.Fatal("already generated content did not continue", err)
	}
	if agent.calls != 0 || tts.calls != 1 || core.dispatches < 1 || !strings.Contains(tts.last.Text, envelope.Body) {
		t.Fatal("prepared body was regenerated, duplicated or dropped", agent.calls, tts.calls, core.dispatches, tts.last.Text)
	}
}
