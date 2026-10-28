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
