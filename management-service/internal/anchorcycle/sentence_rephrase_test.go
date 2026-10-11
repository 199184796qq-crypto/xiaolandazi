package anchorcycle

import "testing"

func TestSentenceRephraseKeepsOtherContentAndRequiresSameSource(t *testing.T) {
	source := "老乡们好。最好两天内吃完。谢谢支持。"
	target, err := SelectSentenceRephraseTarget(source, 5, 13)
	if err != nil || target.Original != "最好两天内吃完。" {
		t.Fatal(target, err)
	}
	got, err := ApplySentenceRephrase(source, target, "建议两天内吃完。")
	if err != nil || got != "老乡们好。建议两天内吃完。谢谢支持。" {
		t.Fatal(got, err)
	}
	if _, err = ApplySentenceRephrase("开头改了。最好两天内吃完。谢谢支持。", target, "建议两天内吃完。"); err == nil {
		t.Fatal("stale source accepted")
	}
	if _, err = ApplySentenceRephrase(source, target, " "); err == nil {
		t.Fatal("empty content accepted")
	}
}
