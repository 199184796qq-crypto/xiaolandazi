package anchorstylescore

import (
	"strings"
	"testing"
)

func macroBaseline() Baseline {
	var b Baseline
	b.Identity.Particles.Main = "啊"
	b.Identity.Audience.Main = "哥哥姐姐们"
	b.Quant.MainParticle = 1
	b.Quant.Audience = 1
	return b
}
func TestMacroPartialWindowOnlyObservesDensity(t *testing.T) {
	b := macroBaseline()
	r := AnalyzeMacro1000(strings.Repeat("字", 999), []string{"老乡，第一段。", "对呀，第二段。", "先说事，第三段。"}, 1, b, "称呼：‘哥哥姐姐们’‘老乡’。")
	if !r.ObservationOnly || r.Stats.Chars != 999 || r.WindowLimit != 1000 {
		t.Fatal(r)
	}
	for _, issue := range r.Issues {
		if issue.Category == "style" {
			t.Fatal("partial macro window forced density")
		}
	}
}
func TestMacroMeasuresThousandCharsNotFiveSegments(t *testing.T) {
	b := macroBaseline()
	recent := []string{"哥哥姐姐们啊啊啊！", "哥哥姐姐们啊啊啊！", "哥哥姐姐们啊啊啊！"}
	r := AnalyzeMacro1000(strings.Repeat("字", 1400), recent, 10, b, "称呼：‘哥哥姐姐们’。")
	if r.Stats.Chars != 1000 || r.Stats.MainAudienceCount != 0 || r.ObservationOnly {
		t.Fatal(r)
	}
	style := false
	for _, issue := range r.Issues {
		if issue.Category == "style" {
			style = true
		}
		if issue.Key == "style_burst" {
			t.Fatal("single draft burst must not drive macro")
		}
	}
	if !style || r.Edges[0].Index != 10 {
		t.Fatal(r)
	}
}
func TestMacroStableBandDoesNotIssueDensityCorrection(t *testing.T) {
	b := macroBaseline()
	text := strings.Repeat("哥哥姐姐们啊"+strings.Repeat("字", 94), 10)
	r := AnalyzeMacro1000(text, []string{"开头一。", "开头二。", "开头三。"}, 1, b, "称呼：‘哥哥姐姐们’。")
	if r.Stats.Chars != 1000 {
		t.Fatal(r.Stats)
	}
	for _, issue := range r.Issues {
		if issue.Category == "style" {
			t.Fatalf("stable distribution rejected: %+v", issue)
		}
	}
}
