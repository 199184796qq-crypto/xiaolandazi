package contentguard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentReviewBindsVerdictWithoutModelIDs(t *testing.T) {
	p, _ := Compile(mother(t))
	text := "开袋后最好当天吃完，第一次买可以先选小份。"
	scan := p.Scan(text, "ordinary_food")
	items, err := p.ContextContent(scan)
	raw, _ := json.Marshal(items)
	if err != nil || strings.Contains(string(raw), "hit_id") || strings.Contains(string(raw), "rule_id") {
		t.Fatal(string(raw), err)
	}
	for _, answer := range []string{"放行", "放行\n食用建议和购买顺序，不是排名宣传。", "通过：真实食用建议。", "```text\n结论：放行\n理由：食用建议。\n```", `{"decision":"pass","reason":"食用建议","ignored_id":"wrong"}`} {
		r, err := ResolveContextContent(scan, text, ContextReview{Raw: answer})
		if err != nil || !r.Passed || len(r.ContextReview.Output.Items) != len(scan.Hits) || r.ContextReview.Raw != answer {
			t.Fatal(answer, r, err)
		}
	}
	for _, answer := range []string{"", "大体不错\n可以播", `{}`, "可能放行\n无法确定"} {
		if r, err := ResolveContextContent(scan, text, ContextReview{Raw: answer}); err == nil || r.Passed {
			t.Fatal("unknown verdict approved", answer)
		}
	}
	for _, answer := range []string{"拦截\n存在本条禁止主张。", "缺依据\n需要确认规则指定凭证。"} {
		r, err := ResolveContextContent(scan, text, ContextReview{Raw: answer})
		if err != nil || r.Passed || len(r.Hits) != len(scan.Hits) {
			t.Fatal(r, err)
		}
	}
	hard := p.Scan("食品级", "ordinary_food")
	if _, err := ResolveContextContent(hard, "食品级", ContextReview{Raw: "放行\n普通描述"}); err == nil {
		t.Fatal("hard rule overridden")
	}
}
