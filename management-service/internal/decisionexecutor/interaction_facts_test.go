package decisionexecutor

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"testing"
)

type snapshotFactStore struct {
	*fakeStore
	asset  model.LiveDerivedSafetyAsset
	failed bool
}

func TestInteractionApprovedFactsRejectsInvalidSnapshotAndCrossPlan(t *testing.T) {
	for _, generation := range []map[string]any{
		{"derived_safety": nil},
		{"derived_safety": "invalid"},
		{"derived_safety": map[string]any{}},
		{"derived_safety": map[string]any{"source_sha256": "missing"}},
	} {
		s := &snapshotFactStore{fakeStore: &fakeStore{publishedVersion: &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: generation}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: "不能回退旧事实"}}
		w := New(s, nil, nil, nil)
		if _, _, err := w.interactionApprovedFacts(context.Background(), model.LiveRuntimeSession{TenantID: 7}, 2); err == nil {
			t.Fatal("invalid reference silently fell back", generation)
		}
	}
	s := &snapshotFactStore{fakeStore: &fakeStore{publishedVersion: &model.LiveAgentPlanVersion{PlanID: 3}}}
	if _, _, err := New(s, nil, nil, nil).interactionApprovedFacts(context.Background(), model.LiveRuntimeSession{TenantID: 7}, 2); err == nil {
		t.Fatal("another scheme was accepted")
	}
}

func (s *snapshotFactStore) GetLiveDerivedSafety(_ context.Context, tenant, plan int64, hash string) (model.LiveDerivedSafetyAsset, error) {
	if tenant != 7 || plan != 2 || hash != "approved" || s.failed {
		return model.LiveDerivedSafetyAsset{}, errors.New("missing")
	}
	return s.asset, nil
}
