package contentguard

import (
	"fmt"
	"strings"
	"testing"
)

func TestContextReviewIsOccurrenceBoundAndHardMatchesStayBlocked(t *testing.T) {
	p, err := Compile(mother(t))
	if err != nil {
		t.Fatal(err)
	}
	text := "开袋后最好当天吃完。这是全网最低价。"
	r := p.Scan(text, "ordinary_food")
	items, err := p.ContextItems(r)
	if err != nil || len(items) < 2 {
		t.Fatal(items, err)
	}
	decisions := []string{}
	for i := range r.Hits {
		decision := "pass"
		if r.Hits[i].Start > 12 {
			decision = "block"
		}
		decisions = append(decisions, fmt.Sprintf(`{"hit_id":"h%d","decision":"%s","reason":"对应完整语境"}`, i+1, decision))
	}
	r, err = ResolveContext(r, text, ContextReview{Raw: `{"items":[` + strings.Join(decisions, ",") + `],"instruction_leak":false,"instruction_quote":""}`})
	if err != nil || r.Passed || len(r.Hits) == 0 || len(r.ContextReview.LexicalHits) < 2 {
		t.Fatal(r, err)
	}
	if r.Hits[0].Start < 12 {
		t.Fatal("consumer hint was still blocked")
	}
	for _, hard := range []string{"这件商品是食品级。", "最好当天吃完，孕妇可用。"} {
		r := p.Scan(hard, "ordinary_food")
		if NeedsContextReview(r) {
			t.Fatal("hard rule reached model", hard)
		}
		if _, err := ResolveContext(r, hard, ContextReview{}); err == nil {
			t.Fatal("hard rule overridable")
		}
	}
}

func TestContextReviewProtocolFailuresCannotProduceApproval(t *testing.T) {
	p, _ := Compile(mother(t))
	text := "开袋后最好当天吃完。"
	r := p.Scan(text, "ordinary_food")
	for _, raw := range []string{
		`{}`, `{"items":[],"instruction_leak":false}`,
		`{"items":[{"hit_id":"h2","decision":"pass","reason":"正常"}],"instruction_leak":false}`,
		`{"items":[{"hit_id":"h1","decision":"pass","reason":""}],"instruction_leak":false}`,
		`{"items":[{"hit_id":"h1","decision":"pass","reason":"正常"}],"instruction_leak":true,"instruction_quote":"伪造的指导"}`,
		`{"items":[{"hit_id":"h1","decision":"pass","reason":"正常"}],"instruction_leak":false} {}`,
	} {
		result, err := ResolveContext(r, text, ContextReview{Raw: raw})
		if err == nil || result.Passed {
			t.Fatal("invalid reply approved", raw)
		}
	}
	r, err := ResolveContext(r, text, ContextReview{Raw: "```json\n" + `{"items":[{"hit_id":"h1","decision":"allow","reason":"真实食用建议"}],"instruction_leak":false}` + "\n```"})
	if err != nil || !r.Passed || r.ContextReview.TextSHA == "" || len(r.ContextReview.LexicalHits) != 1 {
		t.Fatal(r, err)
	}
}

func TestOpeningInstructionRemovalPreservesSpokenBody(t *testing.T) {
	guide := "第一句直接说售后保障"
	body := "哥哥姐姐们，我们家支持7天无理由退货。"
	for _, separator := range []string{"。", "，", "\n"} {
		original := guide + separator + body
		cleaned, edit, err := StripOpeningInstruction(original, []string{guide}, "")
		if err != nil || edit == nil || cleaned != body || edit.Removed+cleaned != original {
			t.Fatal(cleaned, edit, err)
		}
	}
	for _, spoken := range []string{"第一次购买，先看清规格再拍。", "先看清规格再拍。", "最好当天吃完。", "哥哥姐姐们。第一句直接说售后保障。", "第一句直接说售后保障政策是怎样的？"} {
		cleaned, edit, err := StripOpeningInstruction(spoken, []string{guide, "最好当天吃完"}, "")
		if err != nil || edit != nil || cleaned != spoken {
			t.Fatal("spoken text removed", spoken, edit, err)
		}
	}
	if _, _, err := StripOpeningInstruction(guide+"。", []string{guide}, ""); err == nil {
		t.Fatal("empty speech allowed")
	}
	paraphrase := "本段先介绍售后保障。" + body
	cleaned, edit, err := StripOpeningInstruction(paraphrase, []string{"本段先介绍售后保障。"}, "本段先介绍售后保障。")
	if err != nil || edit == nil || cleaned != body || edit.Source != "context_review" {
		t.Fatal(cleaned, edit, err)
	}
}
