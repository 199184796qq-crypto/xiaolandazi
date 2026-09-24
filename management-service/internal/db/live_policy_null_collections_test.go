package db

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/model"
)

func TestNormalizeLivePolicyVersionCollectionsAfterJSONNull(t *testing.T) {
	item := model.LivePolicyVersion{
		Rules:     []model.LivePolicyRule{},
		Overrides: []model.LivePolicyOverride{},
		Conflicts: []model.LivePolicyConflict{},
	}

	if err := json.Unmarshal([]byte("null"), &item.Rules); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte("null"), &item.Overrides); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte("null"), &item.Conflicts); err != nil {
		t.Fatal(err)
	}

	normalizeLivePolicyVersionCollections(&item)

	if item.Rules == nil || item.Overrides == nil || item.Conflicts == nil {
		t.Fatalf("expected non-nil collections: rules=%v overrides=%v conflicts=%v", item.Rules, item.Overrides, item.Conflicts)
	}
	if len(item.Rules) != 0 || len(item.Overrides) != 0 || len(item.Conflicts) != 0 {
		t.Fatalf("expected empty collections after normalization: %+v", item)
	}
}
