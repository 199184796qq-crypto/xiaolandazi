package anchorstylescore

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type FeedbackTemplate struct {
	Version      string            `json:"version"`
	Template     string            `json:"template"`
	Instructions map[string]string `json:"instructions"`
}

type Feedback struct {
	Status             string `json:"status"`
	IssueKey           string `json:"issue_key,omitempty"`
	Dimension          string `json:"dimension,omitempty"`
	Issue              string `json:"issue,omitempty"`
	DimensionDeduction int    `json:"dimension_deduction,omitempty"`
	Deduction          int    `json:"deduction,omitempty"`
	Actual             string `json:"actual,omitempty"`
	Target             string `json:"target,omitempty"`
	Prompt             string `json:"prompt"`
	TemplateVersion    string `json:"template_version,omitempty"`
	TemplateSource     string `json:"template_source,omitempty"`
	Error              string `json:"error,omitempty"`
}

var feedbackKeys = []string{"particle_density", "particle_dominance", "particle_share", "audience_density", "self_density", "confirmation", "clause_length", "questions", "exclamations", "price", "saving", "hesitation", "loop", "cue", "cue_position"}

func ParseFeedbackTemplate(md string) (FeedbackTemplate, error) {
	var t FeedbackTemplate
	_, tail, ok := strings.Cut(md, "```json")
	if !ok {
		return t, fmt.Errorf("回灌模板缺少JSON配置代码块")
	}
	body, _, ok := strings.Cut(tail, "```")
	if !ok || json.Unmarshal([]byte(strings.TrimSpace(body)), &t) != nil {
		return t, fmt.Errorf("回灌模板JSON配置无效")
	}
	if t.Version == "" || t.Template == "" {
		return t, fmt.Errorf("回灌模板缺少版本或正文")
	}
	for _, key := range []string{"dimension", "issue", "actual", "target", "instruction"} {
		if !strings.Contains(t.Template, "{{"+key+"}}") {
			return t, fmt.Errorf("回灌模板缺少占位符：%s", key)
		}
	}
	for _, key := range feedbackKeys {
		if strings.TrimSpace(t.Instructions[key]) == "" {
			return t, fmt.Errorf("回灌模板缺少指令：%s", key)
		}
	}
	return t, nil
}

func fillFeedback(text string, fields map[string]string) string {
	// Fixed substitution order, with no recursive interpretation of source text.
	keys := []string{"dimension", "issue", "actual", "target", "instruction", "terms"}
	pairs := []string{}
	for _, key := range keys {
		pairs = append(pairs, "{{"+key+"}}", fields[key])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// BuildFeedback selects one scored check only; it never requests a model,
// changes the score or speech, or compensates a previous segment's deficit.
func BuildFeedback(r Result, b Baseline, template FeedbackTemplate) Feedback {
	f := Feedback{Status: "no_issue", Prompt: "", TemplateVersion: template.Version}
	// Explicit plan blacklists take priority over density advice. This is an
	// actionable issue, not an extra hidden score veto or mandatory rewrite.
	for _, hit := range r.Stats.Forbidden {
		if hit.Count == 0 {
			continue
		}
		f.Status, f.IssueKey, f.Dimension, f.Issue = "ready", "forbidden_word", "禁用表达", hit.Term
		f.Actual, f.Target = fmt.Sprintf("%s%d次", hit.Term, hit.Count), "本方案明确禁用；用已确认允许的称呼或口语替换"
		instruction := template.Instructions["forbidden_word"]
		if instruction == "" {
			f.Status, f.Error = "unmapped_issue", "回灌模板缺少forbidden_word"
			return f
		}
		fields := map[string]string{"dimension": f.Dimension, "issue": hit.Term, "actual": f.Actual, "target": f.Target, "terms": hit.Term}
		fields["instruction"] = fillFeedback(instruction, fields)
		f.Prompt = fillFeedback(template.Template, fields)
		return f
	}
	var top *Dimension
	for i := range r.Dimensions {
		d := &r.Dimensions[i]
		if d.Applicable && d.Deduction > 0 && (top == nil || d.Deduction > top.Deduction) {
			top = d
		}
	}
	if top == nil {
		return f
	}
	var check *Check
	for i := range top.Checks {
		c := &top.Checks[i]
		if c.Deduction > 0 && (check == nil || c.Deduction > check.Deduction) {
			check = c
		}
	}
	if check == nil {
		f.Status = "unmapped_issue"
		f.Dimension = top.Name
		return f
	}
	f.Dimension, f.Issue = top.Name, check.Label
	f.DimensionDeduction, f.Deduction = top.Deduction, check.Deduction
	f.Actual, f.Target = check.Actual, check.Target
	terms := ""
	key := ""
	frequency := func(v float64) string { return fmt.Sprintf("每百字%.4g次", v) }
	switch top.Name {
	case "语气词配比":
		terms = b.Identity.Particles.Main
		main, otherMax := 0, 0
		for _, count := range r.Stats.Particles {
			if count.Term == terms {
				main = count.Count
			} else {
				otherMax = max(otherMax, count.Count)
			}
		}
		if check.Label == "主力占比" {
			key = "particle_share"
		} else if main > 0 && main < otherMax && strings.HasPrefix(check.Label, "主力语气词 ") {
			key = "particle_dominance"
			parts := []string{}
			for _, count := range r.Stats.Particles {
				parts = append(parts, fmt.Sprintf("%s%d次", count.Term, count.Count))
			}
			f.Actual = strings.Join(parts, "、")
			f.Target = fmt.Sprintf("「%s」占比最高（样本配比%s）", terms, b.Quant.Ratio)
		} else {
			key = "particle_density"
			f.Target = frequency(b.Quant.MainParticle)
		}
	case "主力称呼密度":
		key = "audience_density"
		terms = b.Identity.Audience.Main
		f.Target = frequency(b.Quant.Audience)
	case "自指词密度":
		key = "self_density"
		terms = strings.Join(b.Identity.Self.Terms, "／")
		f.Target = frequency(b.Quant.Self)
	case "句式节奏":
		if check.Label == "确认句" {
			key = "confirmation"
		} else {
			key = "clause_length"
		}
	case "情绪（问号＋感叹号）":
		if check.Label == "问号" && b.Identity.Rhythm.QuestionFrequency != "低" {
			key = "questions"
			label := "问号"
			if b.Quant.QuestionSentences > 0 {
				label = "问句（含无问号的正反问）"
			}
			f.Target = fmt.Sprintf("按本段%d字符约%d个%s（%s）", r.Stats.Chars, int(math.Ceil(questionTarget(b)*float64(r.Stats.Chars)/100)), label, frequency(questionTarget(b)))
		} else if check.Label == "感叹号" {
			key = "exclamations"
			f.Target = fmt.Sprintf("按本段%d字符约%d个感叹号（%s）", r.Stats.Chars, int(math.Ceil(b.Quant.Exclamations*float64(r.Stats.Chars)/100)), frequency(b.Quant.Exclamations))
		}
	case "数字比价":
		switch check.Label {
		case "价格表达":
			key = "price"
		case "算账表达":
			key = "saving"
		case "数字卡壳":
			key = "hesitation"
		}
	case "重复回环":
		key = "loop"
	case "环节位置":
		if check.Label == "行动提示位置" {
			key = "cue_position"
		} else {
			key = "cue"
		}
	}
	f.IssueKey = key
	if key == "" || template.Instructions[key] == "" {
		f.Status = "unmapped_issue"
		return f
	}
	fields := map[string]string{"dimension": f.Dimension, "issue": f.Issue, "actual": f.Actual, "target": f.Target, "terms": terms}
	fields["instruction"] = fillFeedback(template.Instructions[key], fields)
	f.Prompt = fillFeedback(template.Template, fields)
	f.Status = "ready"
	return f
}
