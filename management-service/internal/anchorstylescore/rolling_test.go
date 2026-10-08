package anchorstylescore

import (
	"reflect"
	"strings"
	"testing"
)

func rollingBaseline(t *testing.T) Baseline {
	t.Helper()
	raw, err := Calibrate(qualitativeResponse, "哥哥姐姐们啊！哥哥姐们哟，我们家啊，老乡嘛？", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const rollingStyle = "【口语风格】\n2. 称呼：观众用「哥哥姐姐们」「哥哥姐们」「老乡」。反推禁用「家人们」「宝宝们」。\n3. 自指：我们家。"

func hasRollingCategory(r RollingReport, category string) bool {
	for _, issue := range r.Issues {
		if issue.Category == category {
			return true
		}
	}
	return false
}

func TestRollingAddressesOnlyComeFromAffirmativeStyle(t *testing.T) {
	b := rollingBaseline(t)
	got := StyleAddresses(rollingStyle, b)
	if !reflect.DeepEqual(got, []string{"哥哥姐姐们", "哥哥姐们", "老乡"}) {
		t.Fatalf("unexpected addresses %v", got)
	}
	if got := StyleAddresses("2. 称呼：观众用「老乡」；自称「我们家」「我」。\n【禁用词】「家人们」", b); !reflect.DeepEqual(got, []string{"老乡"}) {
		t.Fatalf("self or blacklist leaked: %v", got)
	}
	if len(StyleAddresses("只有禁用词：家人们，哥哥姐姐们", b)) != 0 {
		t.Fatal("invented a fallback address")
	}
}

func TestRollingDetectsThreeCategoriesAndAbsoluteIndices(t *testing.T) {
	b := rollingBaseline(t)
	texts := []string{
		"哥哥姐姐们，这是第一段。" + strings.Repeat("产品说明", 20) + "直接拍1号链接啊！",
		"哥哥姐们，这是第二段。" + strings.Repeat("产品说明", 20) + "直接拍2号链接哟。",
		"老乡，这是第三段。" + strings.Repeat("产品说明", 20) + "规格慢慢给你们说。",
		"哥哥姐姐们，这是第四段。" + strings.Repeat("产品说明", 20) + "直接拍1号链接嘛。",
		"说到这里，这是第五段。" + strings.Repeat("产品说明", 20) + "我们接着说。",
	}
	r := AnalyzeRolling(texts, 11, b, rollingStyle)
	for _, category := range []string{"opening", "style", "ending"} {
		if !hasRollingCategory(r, category) {
			t.Fatalf("missing %s: %+v", category, r.Issues)
		}
	}
	if r.Openings[0].Count != 3 || r.Openings[0].Share != 0.6 || !reflect.DeepEqual(r.Openings[0].Adjacent, [][2]int{{11, 12}}) {
		t.Fatalf("wrong opening stats: %+v", r.Openings)
	}
	if r.Endings[0].Count != 3 {
		t.Fatalf("varying link numbers or particles split ending: %+v", r.Endings)
	}
}

func TestRollingNormalWindowStopsRotationFeedback(t *testing.T) {
	b := rollingBaseline(t)
	texts := []string{"哥哥姐姐们，一段啊！尾甲。", "老乡，二段哟！尾乙。", "直接说，三段嘛！尾丙。", "哥哥姐们，四段啊！尾丁。", "换个角度，五段啊！尾戊。"}
	r := AnalyzeRolling(texts, 1, b, rollingStyle)
	if hasRollingCategory(r, "opening") || hasRollingCategory(r, "ending") {
		t.Fatalf("unnecessary rotation correction %+v", r.Issues)
	}
}

func TestRollingDoesNotRequirePunctuationPerShortSegment(t *testing.T) {
	b := rollingBaseline(t)
	b.Quant.Exclamations = 0.0898
	b.Quant.Questions = 0.0898
	b.Quant.MainParticle = 0
	b.Quant.Audience = 0
	b.Quant.Self = 0
	r := AnalyzeRolling([]string{"甲" + strings.Repeat("说明", 45), "乙" + strings.Repeat("介绍", 40), "丙" + strings.Repeat("规格", 40)}, 1, b, rollingStyle)
	for _, issue := range r.Issues {
		if strings.Contains(issue.Actual, "问号") || strings.Contains(issue.Actual, "感叹号") {
			t.Fatalf("fractional count rounded up %+v", issue)
		}
	}
}

func TestRollingWeightedRatesAndLocalBurst(t *testing.T) {
	b := rollingBaseline(t)
	b.Quant.MainParticle = 1.3
	texts := []string{strings.Repeat("甲", 190) + "啊", strings.Repeat("乙", 190) + "啊", strings.Repeat("丙", 90) + "啊啊啊啊啊啊"}
	r := AnalyzeRolling(texts, 4, b, rollingStyle)
	for _, issue := range r.Issues {
		if issue.Key == "style_burst" {
			if !reflect.DeepEqual(issue.Evidence, []int{6}) {
				t.Fatal(issue)
			}
			return
		}
	}
	t.Fatalf("missed burst %+v", r.Issues)
}
