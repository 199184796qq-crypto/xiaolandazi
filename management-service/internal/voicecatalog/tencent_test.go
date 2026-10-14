package voicecatalog

import "testing"

func TestCustomerOfficialVoicesExcludeForeignAndChildren(t *testing.T) {
	items := CustomerOfficial("tencent-premium")
	if len(items) != 11 {
		t.Fatal("incorrect adult catalogue", len(items))
	}
	for _, v := range items {
		for _, tag := range v.Tags {
			if tag == "童声" || tag == "英文" {
				t.Fatal("unwanted voice exposed", v)
			}
		}
		if v.Gender != "男声" && v.Gender != "女声" {
			t.Fatal("unexpected voice category", v)
		}
	}
	for _, id := range []string{"101015", "101016", "101050"} {
		if _, ok := Find(id); !ok {
			t.Fatal("saved voice compatibility lost", id)
		}
	}
}

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
