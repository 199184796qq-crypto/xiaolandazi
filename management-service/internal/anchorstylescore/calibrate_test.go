package anchorstylescore

import (
	"encoding/json"
	"strings"
	"testing"
)

const qualitativeResponse = `{
 "主播识别信息":{
  "语气词系统":{"候选语气词":["哟","啊","嘛","了"],"各词功能":{"啊":"强调","哟":"招呼","嘛":"解释","了":"语法助词"}},
  "称呼体系":{"候选称呼":["老乡","哥哥姐姐们","哥哥姐们"],"称呼别名":{},"称呼使用场景":"对全场呼告"},
  "自指体系":{"自指词":["我","我们","我们家"],"我们是否=我们家":true,"禁用自指":[]},
  "句式节奏":{"平均分句长度区间":"约8~25字","长句短句搭配":"长句接短问句","确认句库":["拍好了不？"],"问句使用频率":"低"},
  "数字比价习惯":{"价格表述方式":"无价格","算账句式":[],"是否爱帮观众算账":false,"比价锚点举例":[]},
  "方言口头禅":{"方言词库":[],"口头禅":[]},
  "重复回环习惯":{"高频回环短语":[],"回环强度":"低"},
  "情绪特征":{"整体情绪":"热情","情绪判断依据":"句尾上扬","情绪高点出现在":"呼告时"}
 },
 "禁用词（反推）":{"禁用书面客服语":[],"禁用过渡词":[],"为什么禁用":"口语主播"}
}`

func TestQualitativeOnlyResponseBuildsCompleteProgramBaseline(t *testing.T) {
	sample := "哥哥姐姐们，哥哥姐们，老乡！我说啊，啊，啊，哟，嘛，我们家好了，我们讲过了！"
	original := qualitativeResponse
	canonical, err := Calibrate(original, sample, false)
	if err != nil {
		t.Fatal(err)
	}
	if original != qualitativeResponse {
		t.Fatal("model raw mutated")
	}
	b, err := Parse(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if b.Identity.Particles.Main != "啊" || b.Identity.Particles.Other[0] != "哟" && b.Identity.Particles.Other[0] != "嘛" {
		t.Fatalf("main/other not ranked: %+v", b.Identity.Particles)
	}
	if b.Identity.Audience.Main != "哥哥姐姐们" {
		t.Fatalf("merged address rank: %+v", b.Identity.Audience)
	}
	if len(b.Identity.Self.Terms) != 1 || b.Identity.Self.Terms[0] != "我们家" {
		t.Fatalf("bare pronouns leaked: %+v", b.Identity.Self)
	}
	s := Measure(sample, b)
	if s.LeCount != 2 || s.LeIncluded || s.SelfCount != 1 {
		t.Fatalf("separate counters: %+v", s)
	}
	if b.Identity.Rhythm.LengthRange != "约8~25字" || b.Quant.AverageClauseLength <= 0 {
		t.Fatal("model interval was overwritten by program mean")
	}
	var root map[string]json.RawMessage
	_ = json.Unmarshal([]byte(canonical), &root)
	if root["量化基准"] == nil || root["量化基准（供打分用）"] != nil {
		t.Fatal("new schema missing")
	}
	for i := 0; i < 10; i++ {
		again, err := Calibrate(original, sample, false)
		if err != nil || again != canonical {
			t.Fatalf("calibration not deterministic: %v", err)
		}
	}
}

func TestModelEstimatesAndMainGuessCannotInfluenceQuantitativeRuler(t *testing.T) {
	var root map[string]any
	_ = json.Unmarshal([]byte(qualitativeResponse), &root)
	identity := root["主播识别信息"].(map[string]any)
	identity["语气词系统"].(map[string]any)["主力语气词"] = "哟"
	identity["称呼体系"].(map[string]any)["主力称呼"] = "老乡"
	identity["称呼体系"].(map[string]any)["各称呼频次"] = map[string]any{"老乡": "估计999"}
	root["量化基准"] = map[string]any{"样本总字数_N": 999, "每百字_主力语气词": "瞎估", "每百字_自指词": 80}
	poison, _ := json.Marshal(root)
	sample := "哥哥姐姐们啊！哥哥姐们啊！我们家啊，老乡哟！"
	first, err := Calibrate(qualitativeResponse, sample, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Calibrate(string(poison), sample, false)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("model guesses changed the calibrated output:\n%s\n%s", first, second)
	}
}

func TestLeSwitchChangesCountedVocabularyButAlwaysReportsLiteralCount(t *testing.T) {
	sample := "我们家啊，哥哥姐姐们，好了好了好了好了。"
	separate, err := Calibrate(qualitativeResponse, sample, false)
	if err != nil {
		t.Fatal(err)
	}
	included, err := Calibrate(qualitativeResponse, sample, true)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Parse(separate)
	b, _ := Parse(included)
	if a.Identity.Particles.Main != "啊" || b.Identity.Particles.Main != "了" {
		t.Fatalf("switch failed: %q / %q", a.Identity.Particles.Main, b.Identity.Particles.Main)
	}
	as, bs := Measure(sample, a), Measure(sample, b)
	if as.LeCount != 4 || bs.LeCount != 4 || as.LeIncluded || !bs.LeIncluded {
		t.Fatal("standalone le count changed or mode missing")
	}
	for _, word := range as.Particles {
		if word.Term == "了" {
			t.Fatal("le in default ratio")
		}
	}
}

func TestRecalibrationIdempotentAndWhitespaceDoesNotChangeDenominator(t *testing.T) {
	sample := "哥哥姐姐们啊！\n我们家哟。"
	first, err := Calibrate(qualitativeResponse, sample, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Calibrate(first, sample, false)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("confirm recalibration changed an untouched baseline:\n%s\n%s", first, second)
	}
	b, _ := Parse(first)
	if got := Measure(sample, b).Chars; got != len([]rune(strings.ReplaceAll(sample, "\n", ""))) {
		t.Fatalf("denominator=%d", got)
	}
}
