package addressing

import "testing"

func TestValidatePhraseRejectsLoneGenderTitle(t *testing.T) {
	in := PhraseInput{Mode: "single", People: []FormSet{FormsFor("小陈", "小陈", []string{"老乡"})}, Terms: []string{"老乡"}, GroupLabels: []string{"几位朋友"}}
	if err := ValidatePhrase("小陈哥来啦", in); err == nil {
		t.Fatal("lone 哥 accepted")
	}
	if err := ValidatePhrase("小陈哥姐来啦", in); err != nil {
		t.Fatalf("paired title rejected: %v", err)
	}
	named := PhraseInput{Mode: "single", People: []FormSet{FormsFor("王哥", "王哥", nil)}, Terms: nil, GroupLabels: []string{"几位朋友"}}
	if err := ValidatePhrase("王哥来啦", named); err != nil {
		t.Fatalf("title inside the name rejected: %v", err)
	}
}

func TestKinshipNicknameDoesNotTakeAnotherTitle(t *testing.T) {
	forms := FormsFor("小张姐", "小张姐", []string{"老乡", "朋友"})
	if len(forms.Forms) != 1 || forms.Forms[0] != "小张姐" {
		t.Fatalf("kinship nickname gained a title: %+v", forms.Forms)
	}
	in := PhraseInput{Mode: "single", People: []FormSet{forms}, Terms: []string{"老乡", "朋友"}, GroupLabels: []string{"几位朋友"}}
	if err := ValidatePhrase("小张姐老乡", in); err == nil {
		t.Fatal("stacked title accepted")
	}
	if got := FallbackPhrase(in); got != "小张姐" {
		t.Fatalf("fallback=%q", got)
	}
}

func TestSingleAddressNeverUsesPluralTitle(t *testing.T) {
	in := PhraseInput{Mode: "single", People: []FormSet{FormsFor("小张", "小张", []string{"老乡"})}, Terms: []string{"老乡们", "朋友们"}, GroupLabels: []string{"几位朋友"}}
	if err := ValidatePhrase("小张老乡们", in); err == nil {
		t.Fatal("plural title accepted for single viewer")
	}
	if got := FallbackPhrase(in); got != "小张" {
		t.Fatalf("fallback=%q", got)
	}
}

