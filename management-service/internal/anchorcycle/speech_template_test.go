package anchorcycle

import "testing"

func TestSpeechTemplateBindingDoesNotUseModelListNumbers(t *testing.T) {
	b := SpeechTemplateBinding{OwnerKey: "program-product/task", SourceSHA: "facts", P0SHA: "p0", MotherSHA: "mother"}
	a, err := NewSpeechTemplateAsset("2. 功能尚未确认。\n2. 这项还需要核实。\n99. 目前无法给确定答复。", "issued", b)
	if err != nil || len(a.Cards) != 3 || a.Cards[1].ID != "issued/2" {
		t.Fatal(a, err)
	}
	if _, err = a.Select(b, 0); err == nil {
		t.Fatal("unreviewed template selected")
	}
	for _, c := range a.Cards {
		if err = a.RecordReview(c.ID, SpeechTemplateReceipt{TextSHA: ProductDigest(c.Action), BindingSHA: ProductDigest(b), Accepted: true}); err != nil {
			t.Fatal(err)
		}
	}
	x, _ := a.Select(b, 0)
	y, _ := a.Select(b, 1)
	if x == y {
		t.Fatal("rotation ignored")
	}
	changed := b
	changed.SourceSHA = "updated"
	if _, err = a.Select(changed, 0); err == nil {
		t.Fatal("old fact condition used")
	}
	changed = b
	changed.OwnerKey = "same-product/another-task"
	if _, err = a.Select(changed, 0); err == nil {
		t.Fatal("same product mixed fact tasks")
	}
	if err = a.RecordReview(a.Cards[0].ID, SpeechTemplateReceipt{TextSHA: ProductDigest(a.Cards[1].Action), BindingSHA: ProductDigest(b), Accepted: true}); err == nil {
		t.Fatal("another content receipt accepted")
	}
	if !SpeechTemplateRetained("老乡，"+x+"先确认清楚。", x) || SpeechTemplateRetained("其他结论。", x) {
		t.Fatal("locked sentence check")
	}
	composed, err := ComposeSpeechTemplate(x, "按自己的需求选择就好。")
	if err != nil || !SpeechTemplateRetained(composed, x) {
		t.Fatal("program changed the verified clause")
	}
	if _, err = ComposeSpeechTemplate("", "任意正文。"); err == nil {
		t.Fatal("missing clause accepted")
	}
}
