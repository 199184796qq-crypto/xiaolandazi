package httpapi

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestAgentDraftPreservesEvidenceAndScope(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"destination":"fact","scope_name":"1号链接","attribute":"口味","value":"麻辣","polarity":"positive","conditions":[],"exceptions":[],"source_sentence":"1号链接有麻辣和五香。"}`),
		json.RawMessage(`{"destination":"fact","scope_name":"1号链接","attribute":"口味","value":"五香","polarity":"positive","conditions":[],"exceptions":[],"source_sentence":"1号链接有麻辣和五香。"}`),
		json.RawMessage(`{"destination":"fact","scope_name":"1号链接","attribute":"包邮","value":"支持","polarity":"negative","conditions":[{"field":"地区","op":"in","values":["西藏"],"raw":"仅对西藏订单"}],"exceptions":[],"source_sentence":"1号链接不包邮。","source_context":["仅对西藏订单"]}`),
		json.RawMessage(`{"destination":"fact","scope_name":"2号链接","attribute":"自提","value":"支持","polarity":"negative","conditions":[],"exceptions":[],"source_sentence":"1号链接不支持自提。"}`),
		json.RawMessage(`{"destination":"material","scope_name":null,"attribute":"营销表达","value":"鲜甜多汁","polarity":null,"conditions":[],"exceptions":[],"source_sentence":"鲜甜多汁"}`),
		json.RawMessage(`{"destination":"fact","scope_name":"全场","attribute":"配送","value":"包邮","polarity":"positive","conditions":[],"exceptions":[{"field":"地区","op":"in","values":["西藏"],"raw":"西藏除外"}],"source_sentence":"全场包邮，西藏除外。"}`),
		json.RawMessage(`{"destination":"fact","attribute":"缺少值","conditions":[],"exceptions":[],"source_sentence":"没有标价"}`),
	}
	links := []materialLinkObject{{ID: 20, LinkKey: "1"}, {ID: 21, LinkKey: "2"}}
	objects := materialRowObjects(links, model.MaterialObjectCatalog{})
	got, issues, source := prepareAgentMaterialDraft(rows, objects)
	if len(issues) != 1 || issues[0].Index != 7 || len(got) != 6 {
		t.Fatalf("got %d items and issues %v", len(got), issues)
	}
	if strings.Count(source, "1号链接有麻辣和五香。") != 1 ||
		got[0].ClientID == got[1].ClientID || got[0].ProductLinkID != 20 ||
		got[1].ProductLinkID != 20 || got[2].Polarity != "negative" ||
		len(got[2].SourceRef.Ranges) != 2 || got[3].ScopeKind != "unresolved" ||
		got[3].ScopeName != "2号链接" || got[4].Destination != "material" || got[4].Polarity != "" ||
		got[5].ScopeKind != "plan" || len(materialPredicates(got[5].Exceptions)) != 1 {
		t.Fatalf("scope, qualifiers or provenance lost: %+v", got)
	}
	for _, item := range got {
		if err := validateMaterialImportEvidence(&item, source); err != nil {
			t.Fatalf("%s: %s", item.Attribute, err)
		}
		if item.ScopeKind != "unresolved" {
			if err := validateMaterialClaim(item); err != nil {
				t.Fatalf("draft cannot be committed: %s: %s", item.Attribute, err)
			}
		}
	}
	reversed := slices.Clone(rows)
	slices.Reverse(reversed)
	other, _, _ := prepareAgentMaterialDraft(reversed, objects)
	if got[0].ClientID != other[len(other)-1].ClientID {
		t.Fatal("retry or row reorder changed draft identity")
	}
	ambiguous := materialRowObjects(append(links, materialLinkObject{ObjectID: 5, DisplayName: "1号链接"}), model.MaterialObjectCatalog{})
	got, _, _ = prepareAgentMaterialDraft(rows, ambiguous)
	if got[0].ScopeKind != "unresolved" {
		t.Fatal("ambiguous object should require assignment")
	}
}

func TestAgentDraftReplay(t *testing.T) {
	path := os.Getenv("XIAOLAN_AGENT_DRAFT_SAMPLE")
	if path == "" {
		t.Skip("set XIAOLAN_AGENT_DRAFT_SAMPLE to replay an agent reply")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct{ Items []json.RawMessage }
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	objects := materialRowObjects([]materialLinkObject{
		{ID: 30, LinkKey: "1"}, {ID: 31, LinkKey: "2"}, {ID: 32, LinkKey: "3"},
	}, model.MaterialObjectCatalog{})
	got, issues, source := prepareAgentMaterialDraft(payload.Items, objects)
	if len(got) != 68 || len(issues) != 0 {
		t.Fatalf("%d prepared, issues: %v", len(got), issues)
	}
	facts, materials, negative := 0, 0, 0
	for _, item := range got {
		switch item.Destination {
		case "fact":
			facts++
		case "material":
			materials++
		}
		if item.Polarity == "negative" {
			negative++
		}
		if item.SourceIssue != "" || item.ConditionIssue != "" || item.PolarityIssue != "" {
			t.Errorf("%s rejected by validation: %s %s %s", item.Attribute, item.SourceIssue, item.ConditionIssue, item.PolarityIssue)
		}
		if err := validateMaterialImportEvidence(&item, source); err != nil {
			t.Errorf("%s: %s", item.Attribute, err)
		}
	}
	if facts != 62 || materials != 6 || negative != 5 {
		t.Fatalf("facts=%d materials=%d negative=%d", facts, materials, negative)
	}
	if out := os.Getenv("XIAOLAN_AGENT_DRAFT_RESULT"); out != "" {
		response, err := json.Marshal(map[string]any{"items": got, "issues": issues, "preview_revision": "local-test-preview"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, response, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgentDraftSourceQuoteSurvivesImportValidation(t *testing.T) {
	item := agentMaterialDraftItem{
		Destination: "fact", ScopeName: "1号链接", Attribute: "直播间价格",
		Value: "9.9 元 3 斤", Polarity: "positive",
		Conditions: json.RawMessage("[]"), Exceptions: json.RawMessage("[]"),
		SourceSentence: "1号链接：猕猴桃门店一斤四五块，直播间 9.9 元 3 斤。",
		SourceContext:  []string{"每满99元包邮。"},
	}
	links := []materialLinkObject{{ID: 9, LinkKey: "1"}}
	objects := materialRowObjects(links, model.MaterialObjectCatalog{})
	var id, original string
	for _, tc := range []struct{ name, quote, want string }{
		{"legacy", "", item.SourceSentence},
		{"exact excerpt", "直播间 9.9 元 3 斤", "直播间 9.9 元 3 斤"},
		{"invented text", "直播间 8.9 元 3 斤", item.SourceSentence},
		{"only in additional context", "每满99元", item.SourceSentence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item.SourceQuote = tc.quote
			payload, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			items, issues, source := prepareAgentMaterialDraft([]json.RawMessage{payload}, objects)
			if len(items) != 1 || len(issues) != 0 {
				t.Fatalf("prepared items: %d, issues: %v", len(items), issues)
			}
			if id == "" {
				id, original = items[0].ClientID, source
			}
			if items[0].ClientID != id || source != original {
				t.Fatal("summary changed draft identity or full evidence")
			}
			// The client posts the returned draft back to the existing commit flow.
			body, err := json.Marshal(items[0])
			if err != nil {
				t.Fatal(err)
			}
			var posted model.PlanMaterialClaimInput
			if err := json.Unmarshal(body, &posted); err != nil {
				t.Fatal(err)
			}
			if err := validateMaterialImportEvidence(&posted, source); err != nil {
				t.Fatal(err)
			}
			posted = normalizeMaterialClaim(posted, 1, 2)
			resolveMaterialTargets(&posted, links)
			if err := validateMaterialClaim(posted); err != nil {
				t.Fatal(err)
			}
			if posted.SourceQuote != tc.want || posted.SourceSentence != item.SourceSentence {
				t.Fatalf("summary = %q, full quote = %q", posted.SourceQuote, posted.SourceSentence)
			}
		})
	}
}

func TestAgentDraftSelectionRetainsOnlySelectedEvidence(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"destination":"fact","scope_name":null,"attribute":"旧价格","value":"19元","polarity":"positive","conditions":[],"exceptions":[],"source_sentence":"旧价格19元。"}`),
		json.RawMessage(`{"destination":"fact","scope_name":null,"attribute":"价格","value":"9元","polarity":"positive","conditions":[],"exceptions":[],"source_sentence":"价格9元。","source_quote":"9元","source_context":["限当天领取。"]}`),
	}
	all, issues, source := prepareAgentMaterialDraft(rows, nil)
	if len(issues) != 0 || len(all) != 2 {
		t.Fatalf("preview items = %d, issues = %v", len(all), issues)
	}
	id := all[1].ClientID
	selected, selectedSource, err := selectAgentMaterialDraft(all, source, []string{id})
	if err != nil || len(selected) != 1 {
		t.Fatalf("selected items = %d, error = %v", len(selected), err)
	}
	ref := selected[0].SourceRef
	if selectedSource != "价格9元。\n限当天领取。\n" || ref.ID != materialSourceID(selectedSource) || selected[0].ClientID != id ||
		ref.Ranges[0].Start != 0 || ref.Ranges[0].End != len("价格9元。") {
		t.Fatalf("unexpected selection: %q, %+v", selectedSource, ref)
	}
	if err := validateMaterialImportEvidence(&selected[0], selectedSource); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{}, {id, id}, {"not-in-preview"}} {
		if _, _, err := selectAgentMaterialDraft(all, source, ids); err == nil {
			t.Fatalf("accepted invalid IDs: %q", ids)
		}
	}
}
