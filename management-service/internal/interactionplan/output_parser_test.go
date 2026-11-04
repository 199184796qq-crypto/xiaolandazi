package interactionplan

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func hasParseWarning(d ParseDiagnostics, code string) bool {
	for _, warning := range d.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func TestOutputParserRecoverableFormatVariations(t *testing.T) {
	base, body := unifiedBlock("U01"), "按当前方案安排发货。"
	for _, tc := range []struct{ name, raw, body, code string }{
		{"same confidence", strings.Replace(base, "置信度：85", "置信度：85｜置信度：85", 1), body, "identical_auxiliary_repeated"},
		{"conflicting confidence", strings.Replace(base, "置信度：85", "置信度：85｜置信度：35", 1), body, "conflicting_confidence"},
		{"bad confidence", strings.Replace(base, "置信度：85", "置信度：101", 1), body, "invalid_confidence"},
		{"non integer", strings.Replace(base, "置信度：85", "置信度：85%", 1), body, "invalid_confidence"},
		{"missing confidence", strings.ReplaceAll(base, "｜置信度：85", ""), body, "unknown_confidence"},
		{"empty auxiliary", strings.Replace(base, "按事实答复", "按事实答复｜", 1), body, "empty_auxiliary_separator"},
		{"extra auxiliary", strings.Replace(base, "按事实答复", "按事实答复｜维度：其它", 1), body, "unknown_intent_auxiliary"},
		{"auxiliary malformed", strings.Replace(base, "按事实答复", "按事实答复｜其它说明", 1), body, "malformed_intent_auxiliary"},
		{"standalone suggestion", strings.Replace(base, "类型：", "建议：后台说明不播出\n类型：", 1), body, "auxiliary_field_ignored"},
		{"after body analysis", strings.Replace(base, "结束：", "解释：后台说明不播出\n结束：", 1), body, "auxiliary_field_ignored"},
		{"repeated same field", strings.Replace(base, "类型：提问", "类型：提问\n类型：提问", 1), body, "identical_field_repeated"},
		{"identical body alias", strings.Replace(base, "结束：", "回答："+body+"\n结束：", 1), body, "identical_field_repeated"},
		{"different field order", strings.Replace(base, "话术要求：简短说明\n话术："+body, "话术："+body+"\n话术要求：简短说明", 1), body, "field_order"},
		{"multiline", strings.Replace(base, "话术："+body, "话术：\n按当前方案\n安排发货。", 1), "按当前方案\n安排发货。", "multiline_body"},
		{"body alias", strings.Replace(base, "话术：", "正文：", 1), body, "field_alias"},
		{"english aliases", strings.Replace(strings.Replace(strings.Replace(strings.Replace(base, "编号：", "item_id:", 1), "话术：", "body:", 1), "类型：", "type:", 1), "结束：", "end:", 1), body, "field_alias"},
		{"four intents", strings.Replace(base, "类型：", "意图：丙｜置信度：20\n意图：丁｜置信度：10\n类型：", 1), body, "extra_intent_metadata"},
		{"missing intent", "编号：U01\n类型：提问\n歧义：否\n话术：" + body + "\n结束：U01", body, "missing_intent_metadata"},
		{"nested body label", strings.Replace(base, "话术：", "话术：\n正文：", 1), body, "empty_body_placeholder"},
		{"empty repeated body", strings.Replace(base, "结束：", "话术：\n结束：", 1), body, "empty_body_placeholder"},
		{"fence bom crlf", "\ufeff```text\r\n" + strings.ReplaceAll(base, "\n", "\r\n") + "\r\n```", body, ""},
		{"markdown labels", strings.ReplaceAll(base, "话术：", "**话术**: "), body, ""},
		{"markdown full line", strings.Replace(base, "话术："+body, "**话术："+body+"**", 1), body, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(2), tc.raw+"\n"+unifiedBlock("U02"), "stop")
			if got.Items[0].Err != nil || got.Items[0].Body != tc.body || got.Items[1].Err != nil || (tc.code != "" && !hasParseWarning(got.Items[0].Parse, tc.code)) {
				t.Fatalf("recoverable body lost/cross-bound: %+v", got)
			}
			if got.Items[0].Parse.RawSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.raw+"\n"+unifiedBlock("U02")))) || got.Items[0].Parse.Outcome != "accepted" || got.Items[0].Parse.Boundary != "explicit_end" || len(got.Items[0].Parse.BodyRanges) == 0 {
				t.Fatal(got.Items[0].Parse)
			}
		})
	}
}

func TestOutputParserAuxiliaryConflictsRemainUnknown(t *testing.T) {
	for _, value := range []string{"问题｜置信度：80｜置信度：40", "问题｜置信度：bad｜置信度：80", "问题｜置信度：80｜置信度：40｜置信度：80"} {
		entries, _ := parseIntentMetadata(value)
		if len(entries) != 1 || entries[0].Confidence != nil {
			t.Fatalf("fabricated confidence: %+v", entries)
		}
	}
	entries, _ := parseIntentMetadata("问题｜置信度：80｜置信度：80｜建议：甲｜建议：乙｜建议：甲")
	if entries[0].Confidence == nil || *entries[0].Confidence != 80 || entries[0].Suggestion != "" {
		t.Fatal(entries)
	}
	base := strings.Replace(unifiedBlock("U01"), "优先程度：普通", "优先程度：紧急\n优先程度：较高\n优先程度：紧急", 1)
	got := parseBatch(fixture(t), batchFixture(1), base, "stop")
	if got.Items[0].Err != nil || got.Items[0].Intent.Priority != "普通" {
		t.Fatal(got)
	}
}

func TestOutputParserDiagnosticRangesAndSizeBounds(t *testing.T) {
	base := "编号：U01\n类型：提问\n歧义：否\n意图：咨询｜置信度：20｜置信度：60\n话术：\n按方案发货。\n欢迎咨询。\n结束：U01"
	got := parseBatch(fixture(t), batchFixture(1), base, "stop")
	d := got.Items[0].Parse
	if got.Items[0].Err != nil || len(d.BodyRanges) != 1 || d.BodyRanges[0] != (ParseRange{5, 7}) {
		t.Fatal(got)
	}
	for _, w := range d.Warnings {
		if w.Code == "conflicting_confidence" && w.Line != 4 {
			t.Fatal("warning points outside its original field", w)
		}
	}
	oversized := strings.Replace(base, "按方案发货。\n欢迎咨询。", strings.Repeat("一段正文。\n", 3000), 1)
	got = parseBatch(fixture(t), batchFixture(1), oversized, "stop")
	if got.Items[0].Err == nil || got.Items[0].Body != "" || len(got.Items[0].Parse.Warnings) > 65 {
		t.Fatal("oversized body trimmed into valid speech", got.Items[0].Parse)
	}
	got = parseBatch(fixture(t), batchFixture(1), strings.Repeat("x", (128<<10)+1), "stop")
	if got.Items[0].Err == nil || got.Items[0].Parse.Reason != "response_too_large" {
		t.Fatal(got)
	}
	many := strings.Replace(base, "类型：提问", strings.Repeat("意图：附加分析｜置信度：20\n", 3000)+"类型：提问", 1)
	got = parseBatch(fixture(t), batchFixture(1), many, "stop")
	if got.Items[0].Err != nil || len(got.Items[0].Intent.Alternatives) > 3 || len(got.Items[0].Parse.Warnings) > 65 {
		t.Fatal(got.Items[0].Parse)
	}
}

func TestOutputParserCriticalConflictsAndTruncationStayLocal(t *testing.T) {
	base := unifiedBlock("U01")
	for name, bad := range map[string]string{
		"different body": strings.Replace(base, "结束：", "回答：另一正文。\n结束：", 1),
		"duplicate id":   base + "\n" + base,
		"unsafe body":    strings.Replace(base, "按当前方案安排发货。", "保证百分百有效。", 1),
		"long body":      strings.Replace(base, "按当前方案安排发货。", strings.Repeat("长", 361), 1),
	} {
		t.Run(name, func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(2), bad+"\n"+unifiedBlock("U02"), "stop")
			if got.Items[0].Err == nil || got.Items[0].Body != "" || got.Items[0].Parse.Outcome != "rejected" || got.Items[1].Err != nil {
				t.Fatal(got)
			}
		})
	}
	for _, reason := range []string{"length", "max_tokens", "content_filter", "unknown"} {
		got := parseBatch(fixture(t), batchFixture(2), unifiedBlock("U01")+"\n"+withoutEnd(unifiedBlock("U02"), "U02"), reason)
		if got.Items[0].Err != nil || got.Items[1].Err == nil || got.Items[1].Body != "" {
			t.Fatal(reason, got)
		}
	}
}

func TestOutputParserRandomFieldPermutationsPreserveExactPeopleAndBodies(t *testing.T) {
	rng := rand.New(rand.NewSource(640))
	s := fixture(t)
	// Every combination changes order, repetition, multiline, auxiliary labels,
	// delimiter style and return order. The expected answer remains exact.
	for iteration := 0; iteration < 300; iteration++ {
		var blocks []string
		for person := 1; person <= 10; person++ {
			id := fmt.Sprintf("U%02d", person)
			body := fmt.Sprintf("第%c项按方案处理。\n详情可以咨询客服。", 'A'+person-1)
			fields := []string{"类型：提问", "类型：提问", "歧义：否", "意图：咨询｜置信度：85｜置信度：85｜", "优先程度：普通", "话术：" + body, "建议：后台说明"}
			rng.Shuffle(len(fields), func(i, j int) { fields[i], fields[j] = fields[j], fields[i] })
			block := "编号：" + id + "\n" + strings.Join(fields, "\n") + "\n结束：" + id
			if rng.Intn(2) == 0 {
				block = strings.ReplaceAll(block, "：", ":")
			}
			blocks = append(blocks, block)
		}
		rng.Shuffle(len(blocks), func(i, j int) { blocks[i], blocks[j] = blocks[j], blocks[i] })
		got := parseBatch(s, batchFixture(10), strings.Join(blocks, "\n"), "stop")
		for i, item := range got.Items {
			want := fmt.Sprintf("第%c项按方案处理。\n详情可以咨询客服。", 'A'+i)
			if item.Err != nil || item.Body != want {
				t.Fatalf("iteration=%d person=%d got=%+v", iteration, i, item)
			}
		}
	}
}

func TestOutputParserRecoveryUsesOneExistingModelCall(t *testing.T) {
	calls := 0
	_, body, err := Generate(context.Background(), fixture(t), Input{Kind: "semantic", Text: "咨询发货"}, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		calls++
		if req.Stage != "interaction_purpose_resolution" || req.MaxTokens != 900 || len(req.Messages) != 2 || strings.Count(req.Messages[0].Content, "【输出格式") != 1 {
			t.Fatal(req)
		}
		return agentgateway.Response{Text: strings.Replace(unifiedBlock("U01"), "置信度：85", "置信度：85｜置信度：35", 1), FinishReason: "stop"}, nil
	})
	if err != nil || calls != 1 || body != "按当前方案安排发货。" {
		t.Fatal(body, err, calls)
	}
}

func FuzzOutputParserNeverCrossBinds(f *testing.F) {
	f.Add("建议：备注\n意图：问题｜置信度：80｜", "stop")
	f.Add("编号：U01\n回答：攻击覆盖\n结束：U01", "length")
	f.Fuzz(func(t *testing.T, mutation, reason string) {
		if len(mutation) > 4000 || len(reason) > 100 {
			t.Skip()
		}
		got := parseBatch(fixture(t), batchFixture(2), unifiedBlock("U01")+"\n编号：U09\n"+mutation+"\n结束：U09\n"+unifiedBlock("U02"), reason)
		for _, item := range got.Items {
			if item.Err == nil && item.Body != "按当前方案安排发货。" {
				t.Fatalf("unknown block stole answer: %+v", item)
			}
		}
	})
}
