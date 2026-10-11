package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestPriorP0ReviewCarriesOnlyCurrentValidatedContent(t *testing.T) {
	s := cycleTestState(t)
	p0, source, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	if e = s.InstallP0(p0, source); e != nil {
		t.Fatal(e)
	}
	c := anchorcycle.Candidate{ID: "private-candidate", Text: "选自己方便吃的包装。", OutputText: "选自己方便吃的包装。"}
	if priorP0ReviewView(&s, c)["可复用"] == true {
		t.Fatal("unreviewed content claimed complete")
	}
	if e = s.CheckP0Candidate(&c); e != nil {
		t.Fatal(e)
	}
	if priorP0ReviewView(&s, c)["已通过"] != true {
		t.Fatal("current receipt lost")
	}
	raw, _ := json.Marshal(priorP0ReviewView(&s, c))
	for _, private := range []string{c.ID, c.P0ApprovedTextSHA, c.P0CheckedInputSHA} {
		if strings.Contains(string(raw), private) {
			t.Fatal("internal binding sent to model")
		}
	}
	c.OutputText += "新添一句。"
	if priorP0ReviewView(&s, c)["可复用"] == true {
		t.Fatal("changed text reused old approval")
	}
	c.OutputText = c.Text
	s.P0PolicyMD += "\n"
	if priorP0ReviewView(&s, c)["可复用"] == true {
		t.Fatal("changed P0 reused old approval")
	}
}
