package anchorstylescore

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func productionFeedbackTemplate(t *testing.T) FeedbackTemplate {
	t.Helper()
	md, err := os.ReadFile("../../../configs/prompts/anchor-style/score-feedback-template.md")
	if err != nil {
		t.Fatal(err)
	}
	template, err := ParseFeedbackTemplate(string(md))
	if err != nil {
		t.Fatal(err)
	}
	return template
}

func TestFeedbackOneIssueStableAndUsesAnchorBaseline(t *testing.T) {
	b := sampleBaseline()
	b.Quant.Questions, b.Quant.Exclamations = 0, 0
	r := Score("这是一段没有主播词的普通说明。", b, "产地说明")
	template := productionFeedbackTemplate(t)
	f := BuildFeedback(r, b, template)
	if f.Status != "ready" || f.IssueKey != "particle_density" || f.Target != "每百字2次" || !strings.Contains(f.Prompt, "哟") || strings.Contains(f.Prompt, "{{") {
		t.Fatalf("bad feedback: %+v", f)
	}
	first, _ := json.Marshal(f)
	for i := 0; i < 10; i++ {
		next, _ := json.Marshal(BuildFeedback(r, b, template))
		if string(next) != string(first) {
			t.Fatal("feedback is not deterministic")
		}
	}
	b.Identity.Particles.Main = "呢"
	b.Quant.MainParticle = 3.25
	next := BuildFeedback(Score("普通说明。", b, "产地说明"), b, template)
	if next.Target != "每百字3.25次" || !strings.Contains(next.Prompt, "呢") || strings.Contains(next.Prompt, "「哟」") {
		t.Fatalf("feedback used another anchor: %+v", next)
	}
}

func TestFeedbackSelectsDimensionThenCheckWithStableTies(t *testing.T) {
	b := sampleBaseline()
	r := Result{Stats: Stats{Chars: 300}, Dimensions: []Dimension{
		{Name: "自指词密度", Applicable: true, Deduction: 8, Checks: []Check{{Label: "我们家", Deduction: 8}}},
		{Name: "情绪（问号＋感叹号）", Applicable: true, Deduction: 10, Checks: []Check{{Label: "问号", Actual: "0个", Deduction: 5}, {Label: "感叹号", Actual: "0个", Deduction: 5}}},
		{Name: "句式节奏", Applicable: true, Deduction: 10, Checks: []Check{{Label: "确认句", Deduction: 10}}},
	}}
	f := BuildFeedback(r, b, productionFeedbackTemplate(t))
	if f.IssueKey != "questions" || f.DimensionDeduction != 10 || f.Deduction != 5 || f.Issue != "问号" || !strings.Contains(f.Target, "约3个问号") {
		t.Fatalf("top1 or tie handling failed: %+v", f)
	}
}

func TestFeedbackDominanceNoIssueAndLowQuestion(t *testing.T) {
	b := sampleBaseline()
	b.Quant.Ratio = "哟:啊:嘛 = 10:5:1"
	r := Result{Stats: Stats{Chars: 100, Particles: []Count{{Term: "哟", Count: 1}, {Term: "啊", Count: 3}}}, Dimensions: []Dimension{{Name: "语气词配比", Applicable: true, Deduction: 8, Checks: []Check{{Label: "主力语气词 哟", Deduction: 8}}}}}
	f := BuildFeedback(r, b, productionFeedbackTemplate(t))
	if f.IssueKey != "particle_dominance" || !strings.Contains(f.Actual, "啊3次") || !strings.Contains(f.Target, "占比最高") {
		t.Fatalf("dominance not mapped: %+v", f)
	}
	if clean := BuildFeedback(Result{OverallScore: 100}, b, productionFeedbackTemplate(t)); clean.Status != "no_issue" || clean.Prompt != "" {
		t.Fatalf("manufactured feedback: %+v", clean)
	}
	b.Identity.Rhythm.QuestionFrequency = "低"
	actual := Score("哥哥姐姐们，我们家哟！这个哟，那个哟啊！", b, "产地说明")
	if got := BuildFeedback(actual, b, productionFeedbackTemplate(t)); got.IssueKey == "questions" {
		t.Fatalf("low-question anchor must not get question correction: %+v", got)
	}
}

func TestFeedbackMapsEveryScoredCheck(t *testing.T) {
	template := productionFeedbackTemplate(t)
	b := sampleBaseline()
	for _, test := range []struct{ name, label, key string }{
		{"语气词配比", "主力密度", "particle_density"}, {"语气词配比", "主力占比", "particle_share"},
		{"主力称呼密度", "哥哥姐姐们", "audience_density"}, {"自指词密度", "我们家", "self_density"},
		{"句式节奏", "确认句", "confirmation"}, {"句式节奏", "平均分句长度", "clause_length"},
		{"情绪（问号＋感叹号）", "问号", "questions"}, {"情绪（问号＋感叹号）", "感叹号", "exclamations"},
		{"数字比价", "价格表达", "price"}, {"数字比价", "算账表达", "saving"}, {"数字比价", "数字卡壳", "hesitation"},
		{"重复回环", "基准回环短语", "loop"}, {"环节位置", "呼告／行动提示", "cue"}, {"环节位置", "行动提示位置", "cue_position"},
	} {
		t.Run(test.key, func(t *testing.T) {
			r := Result{Stats: Stats{Chars: 300}, Dimensions: []Dimension{{Name: test.name, Applicable: true, Deduction: 3, Checks: []Check{{Label: test.label, Actual: "实测值", Target: "基准值", Deduction: 3}}}}}
			f := BuildFeedback(r, b, template)
			if f.Status != "ready" || f.IssueKey != test.key || f.Prompt == "" || strings.Contains(f.Prompt, "{{") {
				t.Fatalf("mapping failed: %+v", f)
			}
		})
	}
}

func TestFeedbackTemplateValidation(t *testing.T) {
	if _, err := ParseFeedbackTemplate("没有模板JSON"); err == nil {
		t.Fatal("invalid template accepted")
	}
	valid := productionFeedbackTemplate(t)
	delete(valid.Instructions, "loop")
	raw, _ := json.Marshal(valid)
	if _, err := ParseFeedbackTemplate("```json\n" + string(raw) + "\n```"); err == nil {
		t.Fatal("incomplete template accepted")
	}
}
