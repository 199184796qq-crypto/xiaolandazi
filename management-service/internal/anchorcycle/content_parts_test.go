package anchorcycle

import "testing"

func TestContentPartsPreservePrimaryAndOptionalExtension(t *testing.T) {
	for _, raw := range []string{`{"main":"目前未确认赠品。","extension":""}`, "【主内容】目前未确认赠品。\n【延展】"} {
		p, err := ParseContentParts(raw)
		if err != nil {
			t.Fatal(err)
		}
		e, err := ParseExtensionContent(`{"extension":"按实际需要挑就行。"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := ComposeContentParts(p, e); got != "目前未确认赠品。按实际需要挑就行。" {
			t.Fatal(got)
		}
		if p.Main != "目前未确认赠品。" {
			t.Fatal("primary changed")
		}
	}
	if _, err := ParseContentParts(`{"extension":"只剩介绍"}`); err == nil {
		t.Fatal("missing answer accepted")
	}
	if _, err := ParseExtensionContent(`{"main":"篡改主回答"}`); err == nil {
		t.Fatal("missing extension accepted")
	}
	if e, err := ParseExtensionContent(`{"extension":""}`); err != nil || e != "" {
		t.Fatal("empty extension rejected")
	}
}
