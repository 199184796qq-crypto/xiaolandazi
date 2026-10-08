package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestFactCategoryCatalogCoversLegacyAndExpandedCategories(t *testing.T) {
	catalog, hash, err := loadFactCategoryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Categories) != 42 || len(hash) != 64 {
		t.Fatal(len(catalog.Categories), hash)
	}
	groups := map[string]bool{}
	for _, item := range catalog.Categories {
		groups[item.Group] = true
	}
	if len(groups) != 7 {
		t.Fatal(groups)
	}
	for _, value := range []string{"product", "link", "trade", "fulfillment", "identity_location", "other", "shipping_time", "return_policy", "customer_service", "ingredients", "business_history"} {
		if !catalog.contains(value) {
			t.Fatal(value)
		}
	}
}

func TestFactCategoryCatalogFollowsActiveManagedRelease(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "current", "configs", "fact-categories.json")
	if got := managedFactCategoryConfigPath(filepath.Join(root, "releases", "old-release", "bin")); got != want {
		t.Fatal(got, want)
	}
	if got := managedFactCategoryConfigPath(filepath.Join(root, "deploy", "local-package", "bin")); got != "" {
		t.Fatal("development package is not a managed release", got)
	}
}

func TestFactCategoryCatalogHotReloadAndNewCategoryFlow(t *testing.T) {
	catalog, _, err := loadFactCategoryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "categories.json")
	write := func() {
		raw, e := json.Marshal(catalog)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write()
	t.Setenv("FACT_CATEGORY_CONFIG_PATH", path)
	_, before, err := loadFactCategoryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Categories = append(catalog.Categories, factCategoryDefinition{Value: "custom_color", Label: "颜色信息", Description: "用户提供的颜色信息", Group: "商品信息", Family: "product"})
	write()
	reloaded, after, err := loadFactCategoryCatalog()
	if err != nil || before == after || !reloaded.contains("custom_color") {
		t.Fatal(reloaded, after, err)
	}
	if err = validateLiveAgentPlanFactCandidate(model.LiveAgentPlanFactCandidate{Category: "custom_color", Key: "颜色", Value: "蓝色"}); err != nil {
		t.Fatal(err)
	}
	raw := `{"reply":"已记下","selection_mode":"single","options":[],"captured_facts":[{"category":"custom_color","key":"颜色","value":"蓝色","source_quote":"蓝色"}],"completed":false}`
	guide, err := normalizeSpeechMinerGuideResponse(raw, "蓝色")
	if err != nil || len(guide.CapturedFacts) != 1 || guide.CapturedFacts[0].Category != "custom_color" {
		t.Fatal(guide, err)
	}
	confirmed, err := validateSpeechMinerConfirmation(speechMinerConfirmInput{Confirmed: true, SessionID: "catalog-flow-session", Facts: guide.CapturedFacts, History: []speechMinerMessage{{Role: "user", Text: "蓝色"}}})
	if err != nil || len(confirmed) != 1 || confirmed[0].Category != "custom_color" {
		t.Fatal(confirmed, err)
	}
	if err = os.WriteFile(path, []byte(`{"schema_version":1,"broken":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = loadFactCategoryCatalog(); err == nil {
		t.Fatal("bad hot-reload must fail, not silently serve old categories")
	}
}

func TestFactCategoryCatalogRejectsInvalidConfiguration(t *testing.T) {
	base, _, err := loadFactCategoryCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"duplicate", "unknown_family", "missing_legacy", "changed_legacy_family", "empty_label", "bad_id", "bad_version"} {
		t.Run(kind, func(t *testing.T) {
			catalog := base
			catalog.Categories = append([]factCategoryDefinition(nil), base.Categories...)
			switch kind {
			case "duplicate":
				catalog.Categories = append(catalog.Categories, catalog.Categories[0])
			case "unknown_family":
				catalog.Categories[0].Family = "made_up"
			case "missing_legacy":
				catalog.Categories = catalog.Categories[1:]
			case "changed_legacy_family":
				catalog.Categories[0].Family = "other"
			case "empty_label":
				catalog.Categories[0].Label = " "
			case "bad_id":
				catalog.Categories[0].Value = "../../invalid"
			case "bad_version":
				catalog.SchemaVersion = 99
			}
			raw, _ := json.Marshal(catalog)
			if _, err := parseFactCategoryCatalog(raw); err == nil {
				t.Fatal(kind)
			}
		})
	}
	raw, _ := json.Marshal(base)
	if _, err := parseFactCategoryCatalog(append([]byte{0xef, 0xbb, 0xbf}, raw...)); err != nil {
		t.Fatal("Windows UTF-8 BOM", err)
	}
	if _, err := parseFactCategoryCatalog(append(raw, []byte(`{}`)...)); err == nil {
		t.Fatal("trailing object")
	}
	if _, err := parseFactCategoryCatalog([]byte(strings.Repeat(" ", 128*1024+1))); err == nil {
		t.Fatal("oversized file")
	}
	t.Setenv("FACT_CATEGORY_CONFIG_PATH", filepath.Join(t.TempDir(), "missing.json"))
	if _, _, err := loadFactCategoryCatalog(); err == nil {
		t.Fatal("explicit missing path must not fall back")
	}
}
