package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestProductCommonClassificationKeepsOriginalAndProgramBindings(t *testing.T) {
	state, links := independentFactSnapshotFixture()
	state.Facts += "\n- [other/专属甜点] 仅2号链接送甜点；退款需要先取消预约\n【必说】限2号链接，周末不送"
	facts, err := classifyProductCommonFacts(context.Background(), state.Facts, links, func(_ context.Context, p map[string]any) (string, error) {
		text := p["fact"].(string)
		if p["operation"] == "applies" {
			if strings.HasPrefix(p["focus_product"].(string), "2号链接：") {
				return `{"适用":true}`, nil
			}
			return `{"适用":false}`, nil
		}
		if strings.Contains(text, "仅2号链接送甜点") {
			return `{"归属":"商品"}`, nil
		}
		return `{"归属":"公共"}`, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateProductFactSnapshot(state.Facts, &anchorcycle.ProductFocus{Products: []anchorcycle.FocusProduct{{LinkKey: "1号链接"}, {LinkKey: "2号链接"}, {LinkKey: "3号链接"}}, Facts: facts}); err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		text := classifiedProductFactPackage(link.LinkKey, facts)
		if !strings.Contains(text, couponFactBlock) || strings.Contains(text, "周末不送") != (link.LinkKey == "2号链接") {
			t.Fatal("classification lost conditions or expanded ownership", link.LinkKey, text)
		}
	}
	for _, bad := range []string{`{"归属":"待确认"}`, `{"归属":"公共","product_ids":["2"]}`, `{}`} {
		if _, e := classifyProductCommonFacts(context.Background(), state.Facts, links, func(context.Context, map[string]any) (string, error) { return bad, nil }); e == nil {
			t.Fatal("invalid/uncompleted judgment became shared", bad)
		}
	}
	if _, e := classifyProductCommonFacts(context.Background(), state.Facts, links, func(context.Context, map[string]any) (string, error) { return "", errors.New("provider unavailable") }); e == nil {
		t.Fatal("provider failure became success")
	}
}

func TestProductCommonSnapshotReloadIncludesCommonWheelAndComparisonFacts(t *testing.T) {
	input, links := independentFactSnapshotFixture()
	facts, err := classifyProductCommonFacts(context.Background(), input.Facts, links, func(context.Context, map[string]any) (string, error) { return `{"归属":"公共"}`, nil })
	if err != nil {
		t.Fatal(err)
	}
	wheel := anchorStyleSellingPointWheel{Items: []anchorStyleSellingPointWheelItem{
		{Name: "门店停车", Scope: "公共", Angles: []string{"说明停车位置"}},
		{Name: "套餐对比", Scope: "公共", Angles: []string{"比较1号链接与2号链接，甜点仅2号赠送"}},
		{Name: "套餐价格", Scope: "商品", Angles: []string{"当前套餐价格"}},
	}}
	focus, err := buildClassifiedProductFocus(input, links, facts, wheel)
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range focus.Products {
		if len(product.CommonPoints) != 2 || len(product.Facts) == 0 || len(product.SellingPoints) != 1 {
			t.Fatal("product bundle incomplete", product)
		}
	}
	scheme := liveAnchorScheme{Version: 1, ProductFocus: focus, Config: anchorcycle.Config{Rolling: true, MaxChars: 300}}
	raw, _ := json.Marshal(scheme)
	var frozen liveAnchorScheme
	if json.Unmarshal(raw, &frozen) != nil {
		t.Fatal("decode")
	}
	state, err := (&Server{}).prepareSavedAnchorTaskState(context.Background(), 14, 5, nil, frozen, model.LiveDerivedSafetyAsset{SafeFacts: input.Facts, SafeWheelJSON: `{"卖点列表":[{"卖点名":"全局","角度":["全局"]}]}`})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Queue) != 5 {
		t.Fatal("common wheel was multiplied per product", len(state.Queue))
	}
	state.Facts = input.Facts
	common := 0
	for i, task := range state.Queue {
		if !task.Common {
			continue
		}
		common++
		state.TaskIndex = i
		state.Phase = "body"
		state.ProductFocus.CurrentLink = "1号链接"
		active := state.Task()
		if active.ProductMode != "common" || len(active.ProductLinks()) != 3 || !strings.Contains(state.ProductFacts(active), "二号专属甜点") {
			t.Fatal("public comparison missing product-specific facts", active)
		}
		found := false
		for _, fact := range state.HookFacts() {
			if strings.Contains(fact.Data, "二号专属甜点") {
				found = len(fact.Products) == 1 && fact.Products[0] == "2号链接"
			}
		}
		if !found {
			t.Fatal("common task promoted product fact to shared or dropped it")
		}
	}
	if common != 2 {
		t.Fatal(common)
	}
	after, _ := json.Marshal(frozen)
	if string(raw) != string(after) {
		t.Fatal("runtime mutated saved snapshot")
	}
}

func TestProductCommonSingleProductAndEmptyCommonPools(t *testing.T) {
	input, links := independentFactSnapshotFixture()
	links = links[:1]
	input.Facts = "- [商品卡] 链接=1号链接；商品=一号套餐\n- [identity_location/门店] 门店有停车位"
	for _, onlyCommon := range []bool{false, true} {
		own := links[0].SellingPoints
		wheel := anchorStyleSellingPointWheel{Items: []anchorStyleSellingPointWheelItem{{Name: "套餐", Scope: "商品", Angles: []string{"价格"}}}}
		if onlyCommon {
			links[0].SellingPoints = nil
			wheel.Items[0].Scope = "公共"
		}
		facts, e := classifyProductCommonFacts(context.Background(), input.Facts, links, func(context.Context, map[string]any) (string, error) { return `{"归属":"公共"}`, nil })
		if e != nil {
			t.Fatal(e)
		}
		focus, e := buildClassifiedProductFocus(input, links, facts, wheel)
		if e != nil || len(input.Queue) != 1 || focus.ValidateProducts() != nil {
			t.Fatal("single product/common material snapshot was lost", e)
		}
		scheme := liveAnchorScheme{Config: anchorcycle.Config{Rolling: true, MaxChars: 300}, ProductFocus: focus}
		prepared, prepareErr := (&Server{}).prepareSavedAnchorTaskState(context.Background(), 14, 5, nil, scheme, model.LiveDerivedSafetyAsset{SafeFacts: input.Facts, SafeWheelJSON: `{"卖点列表":[{"卖点名":"套餐","角度":["价格"]}]}`})
		if onlyCommon {
			if prepareErr == nil || !strings.Contains(prepareErr.Error(), "单品任务") {
				t.Fatal("common-only material was accepted as a product rotation", prepareErr)
			}
		} else if prepareErr != nil || prepared.Task().Common {
			t.Fatal("single product with own points cannot start", prepareErr)
		}
		links[0].SellingPoints = own
	}
}

func TestSellingPointScopeContractAndLegacyCompatibility(t *testing.T) {
	legacy := `{"卖点列表":[{"卖点名":"停车","角度":["门店停车"]}],"事实补充建议":[]}`
	if _, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(legacy); err != nil {
		t.Fatal(err)
	}
	if validateSellingPointPromptContract(legacy, `"归属"`) == nil {
		t.Fatal("new scope contract accepted old unclassified output")
	}
	valid := strings.Replace(legacy, `"卖点名":"停车"`, `"卖点名":"停车","归属":"公共"`, 1)
	canonical, _, _, err := normalizeAnchorStyleSellingPointWheelJSON(valid)
	var decoded anchorStyleSellingPointWheel
	_ = json.Unmarshal([]byte(canonical), &decoded)
	if err != nil || len(decoded.Items) != 1 || decoded.Items[0].Scope != "公共" {
		t.Fatal("scope silently discarded", canonical, err)
	}
	if _, _, _, err = normalizeAnchorStyleSellingPointWheelJSON(strings.Replace(valid, "公共", "待确认", 1)); err == nil {
		t.Fatal("third category accepted")
	}
}

func TestScopedWheelProgramDedupePreservesConditionsAndProductSeparation(t *testing.T) {
	raw := `{"卖点列表":[{"卖点名":"停车","归属":"公共","角度":["说明免费停车","说明免费停车"]},{"卖点名":"停车","归属":"公共","角度":["停车入口说明"]},{"卖点名":"到店","归属":"公共","角度":["说明免费停车"]},{"卖点名":"用券","归属":"商品","角度":["每桌一张券"]},{"卖点名":"使用","归属":"商品","角度":["每桌一张券","每桌一张券，周末除外"]}],"事实补充建议":[]}`
	var result anchorStyleSellingPointWheel
	clean := dedupeScopedWheelJSON(raw, true)
	if _, _, _, e := normalizeAnchorStyleSellingPointWheelJSON(clean); e != nil {
		t.Fatal(e)
	}
	_ = json.Unmarshal([]byte(clean), &result)
	if len(result.Items) != 3 || len(result.Items[0].Angles) != 2 || result.Items[2].Angles[0] != "每桌一张券，周末除外" {
		t.Fatal("dedupe lost condition or retained literal duplicates", clean)
	}
	// A separate product receives its own pool; same wording is still available.
	if !strings.Contains(dedupeScopedWheelJSON(`{"卖点列表":[{"卖点名":"用券","归属":"商品","角度":["每桌一张券"]}],"事实补充建议":[]}`, true), "每桌一张券") {
		t.Fatal("dedupe state leaked across products")
	}
	invalid := strings.Replace(raw, `"卖点名":"到店"`, `"unexpected":true,"卖点名":"到店"`, 1)
	if _, _, _, e := normalizeAnchorStyleSellingPointWheelJSON(dedupeScopedWheelJSON(invalid, true)); e == nil {
		t.Fatal("dedupe hid invalid fields")
	}
	badType := `{"卖点列表":[{"卖点名":"停车","归属":"公共","角度":["免费停车"]},{"卖点名":"停车","归属":"公共","目标人群":{},"角度":["免费停车"]}],"事实补充建议":[]}`
	if _, _, _, e := normalizeAnchorStyleSellingPointWheelJSON(dedupeScopedWheelJSON(badType, true)); e == nil {
		t.Fatal("dedupe hid malformed duplicate metadata")
	}
}
