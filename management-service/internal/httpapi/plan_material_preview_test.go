package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestMaterialPreviewContractRetainsDestinationsAndScopes(t *testing.T) {
	raw := `{"items":[{"destination":"qa","key":"invoice","question":"能开发票吗","answer":"支持，需备注","scope":{"type":"store","store":"南充总店"},"content":"支持，需备注"},{"destination":"fact","key":"shipping_fee","content":"包邮","value":"包邮","scope":{"type":"offer","link_no":2},"conditions":[{"field":"amount","op":"gte","values":[99],"raw":"满99元"}],"exceptions":[{"field":"region","op":"in","values":["新疆","西藏"],"universe":"china_provinces","raw":"新疆、西藏除外"}],"source_quote":"满99元包邮，新疆、西藏除外"},{"destination":"gap","question":"明天能到吗"},{"destination":"word","content":"切：切块"},{"destination":"fact","key":"price","content":"49元","scope":{"type":"voucher","variant":"双人券"}}]}`
	items, err := parseMaterialClaimItems(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("lost rows: %+v", items)
	}
	for i := range items {
		items[i] = normalizeMaterialClaim(items[i], 14, 4)
	}
	if items[0].Destination != "qa" || items[0].Question != "能开发票吗" || items[0].StoreName != "南充总店" {
		t.Fatalf("lost QA: %+v", items[0])
	}
	if items[1].ProductLinkID != 2 || !strings.Contains(string(items[1].Exceptions), "西藏") || items[1].FactType != "conditional" {
		t.Fatal(items[1])
	}
	if items[2].Destination != "gap" || items[2].Content == "" || items[3].Destination != "word" || items[4].ScopeName != "" || items[4].ScopeKind != "unresolved" {
		t.Fatal(items)
	}
}
func TestMaterialDocumentCarriesQuestionScopeAndExceptions(t *testing.T) {
	it := model.PlanMaterialClaimInput{ClaimKey: "shipping", Topic: "运费", Content: "包邮", ScopeKind: "product", ProductLinkID: 2, Polarity: "positive", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", Exceptions: json.RawMessage(`[{"field":"region","op":"in","values":["新疆"],"raw":"新疆除外"}]`)}}
	text := materialDocumentText(it)
	for _, want := range []string{"2号链接", "运费", "包邮", "新疆", "例外"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	it.Destination = "qa"
	it.Question = "新疆包邮吗"
	it.Answer = "新疆不适用"
	it.Aliases = []string{"新疆免邮吗"}
	text = materialDocumentText(it)
	for _, want := range []string{it.Question, it.Answer, it.Aliases[0]} {
		if !strings.Contains(text, want) {
			t.Fatal(text)
		}
	}
}
func TestMaterialConfirmationDoesNotAdoptWordsGapsOrUnresolvedScope(t *testing.T) {
	for _, tc := range []struct{ destination, scope string }{{"word", "plan"}, {"gap", "plan"}, {"fact", "unresolved"}} {
		it := normalizeMaterialClaim(model.PlanMaterialClaimInput{Content: "待核实", ScopeKind: tc.scope, MaterialClaimDetails: model.MaterialClaimDetails{Destination: tc.destination}}, 14, 4)
		if validateMaterialClaim(it) == nil {
			t.Fatalf("accepted: %+v", it)
		}
	}
	it := normalizeMaterialClaim(model.PlanMaterialClaimInput{Content: "包邮", ScopeKind: "plan", SourceQuote: "包邮，新疆除外", Conditions: json.RawMessage(`[{"field":"region","op":"not_in","values":["新疆"],"raw":"新疆除外"}]`)}, 14, 4)
	if validateMaterialClaim(it) == nil {
		t.Fatal("complement without universe accepted")
	}
	it.Conditions = json.RawMessage(`[]`)
	it.Exceptions = json.RawMessage(`[{"field":"region","op":"in","values":["新疆"],"universe":"china_provinces","raw":"新疆除外"}]`)
	if err := validateMaterialClaim(it); err != nil {
		t.Fatal(err)
	}
}

type materialBatchEmbedder struct {
	calls []int
	fail  bool
}

func (e *materialBatchEmbedder) EmbedTexts(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, len(texts))
	if e.fail && len(e.calls) == 2 {
		return nil, errors.New("provider failure")
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 2, 3}
	}
	return out, nil
}
func TestMaterialQwenBatchesNeverPartiallyAdopt(t *testing.T) {
	e := &materialBatchEmbedder{}
	vectors, err := embedMaterialTexts(context.Background(), e, make([]string, 17))
	if err != nil || len(vectors) != 17 || len(e.calls) != 3 || e.calls[0] != 8 || e.calls[2] != 1 {
		t.Fatalf("%v %v", e.calls, err)
	}
	e = &materialBatchEmbedder{fail: true}
	vectors, err = embedMaterialTexts(context.Background(), e, make([]string, 17))
	if err == nil || vectors != nil {
		t.Fatal("partial paid response treated as success")
	}
	if !agentgateway.UsesReviewModel("material_import_analyze") {
		t.Fatal("lost server review role")
	}
}
func TestMaterialDifferentConditionsNotDeduplicated(t *testing.T) {
	a := model.PlanMaterialClaimInput{ClaimKey: "packaging", ScopeKind: "plan", Value: "真空", Conditions: json.RawMessage(`[{"field":"region","op":"in","values":["上海"]}]`)}
	b := a
	b.Conditions = json.RawMessage(`[{"field":"region","op":"in","values":["四川"]}]`)
	if len(dedupeMaterialClaims([]model.PlanMaterialClaimInput{a, b})) != 2 {
		t.Fatal("conditional rules collapsed")
	}
}

func TestMaterialExplicitEnumeration(t *testing.T) {
	for _, tc := range []struct {
		text  string
		count int
	}{{"1号、2号（礼品装）、3号（半只装01）均为正宗南充特产绸都杨鸭子", 3}, {"1号、2号、3号均为87.6元", 1}, {"双人餐、四人餐均为198元", 2}, {"总店、分店均为需提前一天预约", 1}, {"全场包邮，新疆西藏除外", 1}, {"香、辣都是口味", 1}} {
		rows := expandMaterialClaim(model.PlanMaterialClaimInput{Content: tc.text, SourceQuote: tc.text, ScopeKind: "plan", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact"}})
		if len(rows) != tc.count {
			t.Fatalf("%s: got %d", tc.text, len(rows))
		}
		for _, it := range rows {
			if it.SourceQuote != tc.text {
				t.Fatal("lost original")
			}
		}
	}
	it := normalizeMaterialClaim(model.PlanMaterialClaimInput{Content: "包邮", FactType: "conditional", SourceQuote: "包邮"}, 14, 4)
	annotateMaterialClaim(&it, "包邮")
	if it.ConditionIssue == "" {
		t.Fatal("conditional without a condition silently accepted")
	}
}
