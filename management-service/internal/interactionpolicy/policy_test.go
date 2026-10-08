package interactionpolicy

import (
	"encoding/json"
	"testing"
)

func TestMotherHasChineseLabelsAndStrictSchema(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TemplateSHA256 == "" {
		t.Fatal("missing provenance")
	}
	cfg.Events["complaint"] = cfg.Events["complaint"]
	raw, _ := json.Marshal(cfg)
	var value map[string]any
	json.Unmarshal(raw, &value)
	value["unsupported_future_field"] = true
	raw, _ = json.Marshal(value)
	if _, err := Decode(raw); err == nil {
		t.Fatal("unknown schema silently discarded")
	}
	delete(cfg.FieldLabels, "weight")
	if err := Validate(cfg); err == nil {
		t.Fatal("missing Chinese alias accepted")
	}
}
