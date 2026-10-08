package anchorstylescore

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func sampleBaseline() Baseline {
	var b Baseline
	b.Identity.Particles.Main = "哟"
	b.Identity.Particles.Other = []string{"啊", "嘛"}
	b.Identity.Audience.Main = "哥哥姐姐们"
	b.Identity.Audience.Aliases = map[string][]string{"哥哥姐姐们": {"哥哥姐们"}}
	b.Identity.Self.Terms = []string{"我们家"}
	b.Identity.Rhythm.Length = "约 12 字"
	b.Identity.Rhythm.QuestionFrequency = "中"
	b.Quant.MainParticle = 2
	b.Quant.Audience = 1
	b.Quant.Self = 1
	b.Quant.Questions = 1
	b.Quant.Exclamations = 1
	return b
}

func dimension(t *testing.T, r Result, name string) Dimension {
	t.Helper()
	for _, d := range r.Dimensions {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("missing dimension: %s", name)
	return Dimension{}
}

func TestCountAliasesSelfAndUnicode(t *testing.T) {
	b := sampleBaseline()
	s := Measure("哥哥姐姐们，哥哥姐们！我们家哟，\n我和我们🙂？", b)
	if s.MainAudienceCount != 2 || s.SelfCount != 1 || len(s.Self) != 1 || s.Self[0].Term != "我们家" {
		t.Fatalf("aliases/self incorrectly counted: %+v", s)
	}
	if s.Questions != 1 || s.Exclamations != 1 {
		t.Fatalf("punctuation: %+v", s)
	}
	text := "甲\n乙 🙂!"
	if got := Measure(text, b).Chars; got != 4 {
		t.Fatalf("Unicode/whitespace count=%d want4", got)
	}
	b.Identity.Self.Terms = []string{"我", "我们", "我们家"}
	s = Measure("我们家，我们，我", b)
	if s.SelfCount != 1 || len(s.SelfExcluded) != 2 || s.SelfExcluded[0].Count != 1 || s.SelfExcluded[1].Count != 1 {
		t.Fatalf("bare self pronouns must be separate: %+v / %+v", s.Self, s.SelfExcluded)
	}
}

func TestScorerChangesWithAnchorAndIsDeterministic(t *testing.T) {
	b := sampleBaseline()
	text := "哥哥姐们，我们家哟！这个哟，那个哟，听好了啊！"
	a := Score(text, b, "产地说明")
	first, _ := json.Marshal(a)
	for i := 0; i < 10; i++ {
		next, _ := json.Marshal(Score(text, b, "产地说明"))
		if string(next) != string(first) {
			t.Fatal("same input yielded different result")
		}
	}
	if len(a.Dimensions) != 8 || a.OverallScore < 0 || a.OverallScore > 100 {
		t.Fatalf("invalid total: %+v", a)
	}
	sum := 0
	for _, d := range a.Dimensions {
		if d.Score < 0 || d.Score > d.MaxScore || d.Deduction != d.MaxScore-d.Score {
			t.Fatalf("invalid dimension: %+v", d)
		}
		sum += d.Score
	}
	if sum != a.OverallScore {
		t.Fatalf("sum=%d total=%d", sum, a.OverallScore)
	}
	b.Identity.Particles.Main = "啊"
	b.Identity.Particles.Other = []string{"哟", "嘛"}
	c := Score(text, b, "产地说明")
	if dimension(t, c, "语气词配比").Deduction <= dimension(t, a, "语气词配比").Deduction {
		t.Fatal("changing the main particle did not change scoring")
	}
}

func TestLowQuestionFrequencyAndEmptyTargets(t *testing.T) {
	b := sampleBaseline()
	b.Identity.Rhythm.QuestionFrequency = "低"
	r := Score("哥哥姐们，我们家哟！", b, "")
	emotion := dimension(t, r, "情绪（问号＋感叹号）")
	if emotion.Checks[0].Deduction != 0 {
		t.Fatalf("low-question anchor penalized: %+v", emotion)
	}
	b.Identity.Self.Terms = nil
	b.Quant.Self = 0
	b.Identity.Audience.Main = ""
	b.Quant.Audience = 0
	r = Score("今天的讲解。", b, "")
	if dimension(t, r, "自指词密度").Deduction != 0 || dimension(t, r, "主力称呼密度").Deduction != 0 {
		t.Fatal("missing sample traits must not be manufactured")
	}
}

func TestPricesOnlyScoreApplicableSegments(t *testing.T) {
	b := sampleBaseline()
	b.Identity.Numbers.Style = "精确到几块几"
	b.Identity.Numbers.LikesArithmetic = true
	if dimension(t, Score("讲讲产地哟！", b, "产地说明"), "数字比价").Applicable {
		t.Fatal("price dimension applied to unrelated segment")
	}
	d := dimension(t, Score("今天先说优惠啊！", b, "价格算账"), "数字比价")
	if !d.Applicable || d.Deduction != 7 {
		t.Fatalf("missing price deduction: %+v", d)
	}
}

func TestRatioUsesTargetAndPenaltyDoesNotDoubleCount(t *testing.T) {
	b := sampleBaseline()
	b.Quant.Ratio = "哟:啊:嘛 = 3:1:0"
	r := Score("哟哟哟啊", b, "")
	d := dimension(t, r, "语气词配比")
	if len(d.Checks) != 3 || d.Checks[2].Deduction != 0 {
		t.Fatalf("matching ratio penalized: %+v", d)
	}
	r = Score("啊啊啊", b, "")
	d = dimension(t, r, "语气词配比")
	if d.Deduction != 12 {
		t.Fatalf("zero main should not add takeover/density penalties: %+v", d)
	}
}

const validWireBaseline = `{
 "主播识别信息":{
   "语气词系统":{"主力语气词":"哟","辅助语气词":["啊","嘛"],"配比":"哟:啊=2:1"},
   "称呼体系":{"主力称呼":"哥哥姐姐们","辅助称呼":[],"各称呼频次":{"哥哥姐姐们":99}},
   "自指体系":{"自指词":["我们家"]},
   "句式节奏":{"平均分句长度":"约12字","确认句库":[],"问句使用频率":"低"},
   "数字比价习惯":{"价格表述方式":"无价格","算账句式":"无","是否爱帮观众算账":false},
   "方言口头禅":{},"重复回环习惯":{"高频回环短语":[],"回环强度":"低"},"情绪特征":{}
 },
 "量化基准（供打分用）":{
   "每百字_主力语气词":99,"每百字_辅助语气词1":99,"每百字_辅助语气词2":0,
   "每百字_主力称呼":99,"每百字_自指词":99,"每百字_问号":0,"每百字_感叹号":0,
   "语气词配比目标":"哟:啊=2:1"
 },
 "禁用词（反推）":{}
}`

func TestVerifySampleRecountsRatherThanTrustingModelNumbers(t *testing.T) {
	sample := "哥哥姐姐们哟！哥哥姐们哟！我们家啊，继续。"
	verified, err := VerifySample(validWireBaseline, sample)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(verified)
	if err != nil {
		t.Fatal(err)
	}
	stats := Measure(sample, b)
	if stats.MainAudienceCount != 2 || stats.SelfCount != 1 {
		t.Fatalf("sample counts: %+v", stats)
	}
	if math.Abs(b.Quant.MainParticle-density(2, stats.Chars)) > 0.0001 {
		t.Fatalf("model number not corrected: %+v", b.Quant)
	}
	if len(b.Identity.Audience.Aliases["哥哥姐姐们"]) != 1 || b.Identity.Audience.Aliases["哥哥姐姐们"][0] != "哥哥姐们" {
		t.Fatalf("sample contraction not derived: %+v", b.Identity.Audience.Aliases)
	}
	if !strings.Contains(verified, "样本统计校验") {
		t.Fatal("missing auditable counts")
	}
	r := Score(sample, b, "")
	if dimension(t, r, "自指词密度").Deduction != 0 || dimension(t, r, "主力称呼密度").Deduction != 0 || dimension(t, r, "情绪（问号＋感叹号）").Deduction != 0 {
		t.Fatalf("the reference sample itself was penalized due to rounding: %+v", r)
	}
}

func TestParseRejectsInvalidNumbersAndIncompleteTables(t *testing.T) {
	for _, raw := range []string{
		"null", "[]", "{}", strings.Replace(validWireBaseline, `"每百字_主力语气词":99`, `"每百字_主力语气词":-1`, 1),
		strings.Replace(validWireBaseline, `"每百字_主力语气词":99`, `"每百字_主力语气词":null`, 1),
		strings.Replace(validWireBaseline, `"每百字_主力语气词":99`, `"每百字_主力语气词":"1.4次"`, 1),
	} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("accepted invalid baseline: %s", raw)
		}
	}
}
