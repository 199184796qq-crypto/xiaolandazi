package db

import (
	"errors"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPlanSaveRejectsReadyAudioAtConcurrentDifferentRate(t *testing.T) {
	c := &planSaveConn{}
	s := planSaveStore(t, c)
	_, err := s.CreateLiveAgentPlanVersion(t.Context(), 14, 4, 8, model.CreateLiveAgentPlanVersionInput{}, 1.1)
	if !errors.Is(err, ErrPlanPreparedVoiceRateChanged) || !c.rolled || c.committed || len(c.writes) > 0 {
		t.Fatal("different-rate cached audio published", err, c)
	}
}

func TestPlanSaveAcceptsReadyAudioAtAuthoritativeRate(t *testing.T) {
	c := &planSaveConn{}
	s := planSaveStore(t, c)
	_, err := s.CreateLiveAgentPlanVersion(t.Context(), 14, 4, 8, model.CreateLiveAgentPlanVersionInput{GenerationContext: map[string]any{"content_mode": "ai_dynamic"}}, 1)
	if err != nil || !c.committed {
		t.Fatal(err, c)
	}
}

func TestPlanSaveStillRejectsLiveAfterAudioPreparation(t *testing.T) {
	c := &planSaveConn{active: true}
	s := planSaveStore(t, c)
	_, err := s.CreateLiveAgentPlanVersion(t.Context(), 14, 4, 8, model.CreateLiveAgentPlanVersionInput{}, 1)
	if !errors.Is(err, ErrLiveAgentPlanInUse) || c.committed || len(c.writes) > 0 {
		t.Fatal("room starting during preparation bypassed protection", err, c)
	}
}
