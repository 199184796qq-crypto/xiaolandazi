package httpapi

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPlanPromptPlaceholderCatalogMatchesRenderers(t *testing.T) {
	items := planPromptPlaceholderCatalog()
	if !reflect.DeepEqual(items, planPromptPlaceholderCatalog()) {
		t.Fatal("catalog order must be stable")
	}
	expected := map[string]bool{"直播间名": true, "商品": true, "福利": true, "样本话术": true, "已有开场": true, "开场方式": true, "近期写作记录": true}
	for key := range planPromptDefaults {
		expected[key] = true
	}
	validTemplates := map[string]bool{"persona_md": true, "facts_md": true, "red_lines_md": true, "speech_requirements_md": true, "interaction_requirements_md": true, "rewrite_planning_md": true}
	seen := map[string]bool{}
	for _, item := range items {
		t.Run(item.Key, func(t *testing.T) {
			if !expected[item.Key] || seen[item.Key] || item.Label == "" || item.Description == "" || item.Group == "" || len(item.Templates) == 0 {
				t.Fatalf("invalid or duplicate item: %+v", item)
			}
			seen[item.Key] = true
			for _, template := range item.Templates {
				if !validTemplates[template] {
					t.Fatalf("unknown template: %s", template)
				}
			}
			if item.Group == "改写运行数据" {
				if !reflect.DeepEqual(item.Templates, []string{"rewrite_planning_md"}) {
					t.Fatal("runtime slots must not be stripped by the plan pull renderer")
				}
				got, err := renderScriptRewriteRequest("前文\n"+item.InsertText+"\n后文", "本条样本", "旧开场", "先讲体验", scriptRewriteOutput{})
				if err != nil || strings.Contains(got, item.InsertText) || !strings.Contains(got, "前文") || !strings.Contains(got, "后文") {
					t.Fatalf("runtime slot not consumed: %q / %v", got, err)
				}
				return
			}
			data := planPromptRenderData{PlanName: "当前方案", Links: []model.LiveAgentPlanProductLink{{LinkKey: "3", ProductName: "有效商品", Status: "active"}, {LinkKey: "9", ProductName: "停用商品", Status: "inactive"}}, Benefits: []model.LiveAgentPlanBenefit{{Activity: "有效福利", Status: "active"}, {Activity: "停用福利", Status: "inactive"}}}
			got := renderPlanPrompt("前文\n"+item.InsertText+"\n后文", data)
			if strings.Contains(got, "{{") || strings.Contains(got, "}}") || !strings.Contains(got, "前文") || !strings.Contains(got, "后文") {
				t.Fatalf("pull slot not consumed: %q", got)
			}
			want := planPromptDefaults[item.Key]
			switch item.Key {
			case "直播间名":
				want = "当前方案"
			case "商品":
				want = "有效商品"
			case "福利":
				want = "有效福利"
			}
			if want == "" || !strings.Contains(got, want) || strings.Contains(got, "停用") {
				t.Fatalf("incorrect substitution: %q, want %q", got, want)
			}
			if item.Key == "商品" || item.Key == "福利" {
				if !reflect.DeepEqual(item.Templates, []string{"facts_md"}) {
					t.Fatal("facts blocks belong to the facts template")
				}
				if empty := renderPlanPrompt(item.InsertText, planPromptRenderData{}); strings.TrimSpace(empty) != "" {
					t.Fatalf("empty block must not invent facts: %q", empty)
				}
			} else if len(item.Templates) != 5 {
				t.Fatal("pull values belong to the five rendered templates")
			}
		})
	}
	if len(seen) != len(expected) {
		t.Fatalf("catalog incomplete: %d / %d", len(seen), len(expected))
	}
	// This fixture is consumed by the offline browser regression, so it tests the actual API catalog.
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("PLACEHOLDER_CATALOG_JSON:" + string(raw))
}

func TestPlanPromptPlaceholderRuntimeValuesAreExact(t *testing.T) {
	got, err := renderScriptRewriteRequest("{{样本话术}}\n{{已有开场}}\n{{开场方式}}\n{{近期写作记录}}", "样本保留{{已有开场}}字面量", "实际旧开场", "实际开场方式", scriptRewriteOutput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"样本保留{{已有开场}}字面量", "实际旧开场", "实际开场方式", "（暂无上次写作记录）"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
}

func TestPlanPromptPlaceholderCompatibilitySnapshotMatchesCatalog(t *testing.T) {
	raw, err := os.ReadFile("../../../web-console/src/planPromptPlaceholderCatalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot []planPromptPlaceholder
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, planPromptPlaceholderCatalog()) {
		t.Fatal("frontend compatibility catalog must be regenerated from the tested backend catalog")
	}
}
