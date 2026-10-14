package voicecatalog

import "testing"

func TestTencentPremiumCatalogueKeepsLegacyVoices(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range TencentPremium() {
		if seen[v.ID] || v.Provider != "tencent_tts" || v.Model != "tencent-premium" {
			t.Fatalf("bad entry: %+v", v)
		}
		seen[v.ID] = true
		if found, ok := Find(v.ID); !ok || found.Model != v.Model {
			t.Fatal("unselectable premium voice")
		}
	}
	if len(seen) != 14 || seen["402000"] || seen["501000"] {
		t.Fatal("catalogue includes wrong tier")
	}
	if Default().ID != DefaultVoiceID {
		t.Fatal("existing default changed")
	}
	for _, v := range Official() {
		if _, ok := Find(v.ID); !ok {
			t.Fatalf("legacy voice lost: %s", v.ID)
		}
	}
}
