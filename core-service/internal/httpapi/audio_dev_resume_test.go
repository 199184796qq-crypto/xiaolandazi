package httpapi

import "testing"

func testDevSentence(t *testing.T, id string) devMainlineSentence {
	t.Helper()
	data, err := loadDevMainlineMap()
	if err != nil {
		t.Fatal(err)
	}
	for _, sentence := range data.Sentences {
		if sentence.ID == id {
			return sentence
		}
	}
	t.Fatalf("sentence %s not found", id)
	return devMainlineSentence{}
}

func TestResolveDevMainlineResumeSkipsImmediateDeliveryBlock(t *testing.T) {
	stop := testDevSentence(t, "S012").PlayStartMS
	resume := testDevSentence(t, "S014")
	got := resolveDevMainlineResume(
		stop,
		[]string{"SHIPPING"},
		"江浙沪皖隔天到，其他地方两三天。",
		"",
		nil,
	)
	if !got.Changed {
		t.Fatal("expected semantic resume change")
	}
	if got.Mode != "CROSS_RESUME" {
		t.Fatalf("mode=%s", got.Mode)
	}
	if got.ResumeUnit != "S014" || got.ResumeOffsetMS != resume.PlayStartMS {
		t.Fatalf("resume=%s offset=%d expected=%d", got.ResumeUnit, got.ResumeOffsetMS, resume.PlayStartMS)
	}
	if len(got.SkipUnits) != 2 || got.SkipUnits[0] != "S012" || got.SkipUnits[1] != "S013" {
		t.Fatalf("skip=%#v", got.SkipUnits)
	}
	if got.ResumeText == "" {
		t.Fatal("resume text is empty")
	}
}

func TestResolveDevMainlineResumeDoesNotSkipAcrossUncoveredContent(t *testing.T) {
	stop := testDevSentence(t, "S009").PlayStartMS
	got := resolveDevMainlineResume(
		stop,
		[]string{"delivery"},
		"物流隔天到。",
		"",
		nil,
	)
	if got.Changed {
		t.Fatalf("should not jump across cooking/spec units: %#v", got)
	}
}
