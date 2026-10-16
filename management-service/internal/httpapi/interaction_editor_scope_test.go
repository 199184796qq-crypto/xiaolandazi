package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"testing"
)

func TestEditorInteractionScopePreservesOriginalFactsAndBindings(t *testing.T) {
	original := "- [商品卡] 链接=1号链接；商品=设备；价格=299元\n- [other] 未核销可申请退款；旧表达。\n保留多行条件。"
	safe := "- [商品卡] 链接=1号链接；商品=设备；价格=299元\n- [other] 未核销可申请退款；安全改写=请按未核销条件申请。\n保留多行条件。"
	blocks := productFactBlocks(safe)
	focus := &anchorcycle.ProductFocus{MaterialVersion: productCommonMaterialVersion, FactsSHA: derivedHash(safe), Products: []anchorcycle.FocusProduct{{LinkKey: "1号链接", Name: "设备"}}, Facts: []anchorcycle.ScopedFact{{Text: blocks[0], Links: []string{"1号链接"}, Applicable: true}, {Text: blocks[1], Shared: true}}}
	asset := model.LiveDerivedSafetyAsset{OriginalFacts: original, SafeFacts: safe}
	got := editorInteractionScopeForSafety(original, focus, asset)
	if got == nil || got.FactsSHA != derivedHash(original) || got.Facts[1].Text != productFactBlocks(original)[1] || got.Facts[0].Links[0] != "1号链接" {
		t.Fatal(got)
	}
	if focus.FactsSHA != derivedHash(safe) || focus.Facts[1].Text != blocks[1] {
		t.Fatal("saved version mutated")
	}
	if editorInteractionScopeForSafety(original+"\n新资料", focus, asset) != nil {
		t.Fatal("unsaved facts reused old ownership")
	}
	asset.SafeFacts += "\n- [other] 新条件"
	if editorInteractionScopeForSafety(original, focus, asset) != nil {
		t.Fatal("incomplete saved mapping accepted")
	}
}
