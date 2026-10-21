package speechlibrary

import (
	"encoding/json"
	"testing"

	"livecompanion/management/internal/anchorcycle"
)

func TestAdmissionModesBindProseContextAndLegacy(t *testing.T) {
	sc := Scope{TextContext: Digest("facts")}
	c := anchorcycle.Candidate{OutputText: "本款按当前资料选择规格。"}
	e := Entry{Candidate: c, Admission: NewAdmission(sc, c, GenerationAccepted)}
	if err := e.ValidateAdmission(sc, GenerationAccepted); err != nil {
		t.Fatal(err)
	}
	if err := e.ValidateAdmission(sc, ReviewPassed); err == nil {
		t.Fatal("unreviewed text acquired review authority")
	}
	raw, err := e.MarshalPayload()
	if err != nil {
		t.Fatal(err)
	}
	var read Entry
	if err = read.UnmarshalPayload(raw); err != nil || read.ValidateAdmission(sc, GenerationAccepted) != nil {
		t.Fatal("durable receipt lost", err)
	}
	read.Candidate.OutputText += "价格改变。"
	if read.ValidateAdmission(sc, GenerationAccepted) == nil {
		t.Fatal("edited text retained admission")
	}
	sc.TextContext = Digest("changed condition")
	if e.ValidateAdmission(sc, GenerationAccepted) == nil {
		t.Fatal("changed facts retained admission")
	}
	raw, _ = json.Marshal(c)
	if err = read.UnmarshalPayload(raw); err != nil || read.Admission != nil || read.ValidateAdmission(sc, "") == nil {
		t.Fatal("legacy payload gained new admission", err)
	}
	e.Admission.Mode = ReviewPassed
	if e.ValidateAdmission(Scope{TextContext: e.Admission.ContextSHA}, ReviewPassed) == nil {
		t.Fatal("missing review accepted")
	}
}
