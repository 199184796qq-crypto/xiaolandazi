package interactionplan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func withoutEnd(raw, id string) string {
	return strings.ReplaceAll(raw, "\n结束："+id, "")
}

func TestBatchMissingEndRecoveredAtNextItemOrNormalEOF(t *testing.T) {
	s := fixture(t)
	for _, reason := range []string{"stop", "end_turn", "completed", "STOP"} {
		got := parseBatch(s, batchFixture(3), withoutEnd(unifiedBlock("U03"), "U03")+"\n"+withoutEnd(unifiedBlock("U01"), "U01")+"\n"+withoutEnd(unifiedBlock("U02"), "U02"), reason)
		for _, item := range got.Items {
			if item.Err != nil || item.Body != "按当前方案安排发货。" || len(item.Intent.Alternatives) != 2 {
				t.Fatalf("recoverable numbered item lost: %+v", got)
			}
		}
	}
}

func TestBatchMissingEndAtUnknownOrTruncatedEOFOnlyDropsTail(t *testing.T) {
	for _, reason := range []string{"", "length", "max_tokens", "incomplete", "content_filter", "unknown"} {
		got := parseBatch(fixture(t), batchFixture(2), withoutEnd(unifiedBlock("U01"), "U01")+"\n"+withoutEnd(unifiedBlock("U02"), "U02"), reason)
		if got.Items[0].Err != nil || got.Items[1].Err == nil || got.Items[1].Body != "" {
			t.Fatalf("%q: must keep bounded predecessor and drop unproven tail: %+v", reason, got)
		}
	}
}

func TestBatchRecoveryCannotRepairInvalidPerson(t *testing.T) {
	valid := withoutEnd(unifiedBlock("U02"), "U02")
	for name, bad := range map[string]string{
		"missing metadata":   strings.Replace(valid, "类型：提问\n", "", 1),
		"missing body":       strings.Replace(valid, "话术：按当前方案安排发货。", "", 1),
		"empty body":         strings.Replace(valid, "话术：按当前方案安排发货。", "话术：", 1),
		"unknown field":      valid + "\n备注：不能播出",
		"duplicate field":    strings.Replace(valid, "类型：提问", "类型：提问\n类型：闲聊", 1),
		"two bodies":         valid + "\n回答：不能覆盖。",
		"wrong end":          valid + "\n结束：U01",
		"invalid ambiguity":  strings.Replace(valid, "歧义：否", "歧义：不知道", 1),
		"invalid confidence": strings.Replace(valid, "置信度：85", "置信度：101", 1),
		"multiline body":     valid + "\n不能把额外一行也当正文。",
	} {
		t.Run(name, func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(3), withoutEnd(unifiedBlock("U01"), "U01")+"\n"+bad+"\n"+withoutEnd(unifiedBlock("U03"), "U03"), "stop")
			if got.Items[0].Err != nil || got.Items[1].Err == nil || got.Items[1].Body != "" || got.Items[2].Err != nil {
				t.Fatalf("one malformed person affected another: %+v", got)
			}
		})
	}
}

func TestBatchDuplicateAndUnknownIDsCannotStealRecoveredBodies(t *testing.T) {
	raw := withoutEnd(unifiedBlock("U01"), "U01") + "\n" + withoutEnd(unifiedBlock("U02"), "U02") + "\n" + withoutEnd(unifiedBlock("U01"), "U01") + "\n" + withoutEnd(unifiedBlock("U09"), "U09") + "\n" + withoutEnd(unifiedBlock("U03"), "U03")
	got := parseBatch(fixture(t), batchFixture(3), raw, "stop")
	if got.Items[0].Err == nil || got.Items[0].Body != "" || got.Items[1].Err != nil || got.Items[2].Err != nil {
		t.Fatal(got)
	}
}

func TestBatchRecoveryStillValidatesClarificationAndP0(t *testing.T) {
	valid := "编号：U01\n意图：确认问题\n类型：提问\n歧义：是\n候选：发货时间、配送地区\n澄清句：您问发货时间，还是配送地区？\n话术："
	got := parseBatch(fixture(t), batchFixture(1), valid, "stop")
	if got.Items[0].Err != nil || got.Items[0].Body != "您问发货时间，还是配送地区？" {
		t.Fatal(got)
	}
	s := fixture(t)
	// Existing validation must still reject an explicitly unsafe spoken body.
	unsafe := strings.Replace(withoutEnd(unifiedBlock("U01"), "U01"), "按当前方案安排发货。", "保证百分百有效。", 1)
	got = parseBatch(s, batchFixture(1), unsafe, "stop")
	if got.Items[0].Err == nil || got.Items[0].Body != "" {
		t.Fatal("recovery bypassed P0", got)
	}
}

// Exact supplier text from the reported request (exchange 51447); only the
// completion reason is supplied by the transport in new requests.
func TestReportedYangDuckReplyMissingEndCanBeRecovered(t *testing.T) {
	raw := "编号：U01\n意图：确认直播间现在可以下单了｜置信度：70｜建议：顺着话头告诉大家链接已开，简要说明可拍规格，不追加催促表达\n类型：下单意向\n优先程度：较高\n优先原因：明确指向现在可以下单，属于购买推进\n歧义：否\n候选：\n澄清句：\n话术要求：顺着“可以下单”回应，确认链接已开，提一句链接上有整只和半只两种规格可选，不下单催促、不报价格以外信息\n话术：链接已经开了，现在能正常拍。链接上有整只装和半只装，你想自己吃就按需要的规格来就行。"
	calls := 0
	intent, body, err := Generate(context.Background(), fixture(t), Input{Kind: "semantic", Text: "现在升级了可以下单了"}, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{Text: raw, FinishReason: "stop"}, nil
	})
	if err != nil || calls != 1 || intent.Type != "下单意向" || body != "链接已经开了，现在能正常拍。链接上有整只装和半只装，你想自己吃就按需要的规格来就行。" {
		t.Fatal(intent, body, err, calls)
	}
}

func TestInvalidReturnedItemIsTerminalButProviderFailureIsNot(t *testing.T) {
	for _, tc := range []struct{ raw, reason string }{
		{"编号：U01\n意图：问题\n话术：残缺内容", "stop"},
		{withoutEnd(unifiedBlock("U01"), "U01"), "length"},
		{withoutEnd(unifiedBlock("U01"), "U01"), ""},
	} {
		_, body, err := Generate(context.Background(), fixture(t), Input{Kind: "semantic", Text: "问题"}, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
			return agentgateway.Response{Text: tc.raw, FinishReason: tc.reason}, nil
		})
		if !errors.Is(err, ErrInvalidResult) || body != "" {
			t.Fatal(body, err)
		}
	}
	transient := fmt.Errorf("transport temporarily unavailable")
	_, _, err := Generate(context.Background(), fixture(t), Input{Kind: "semantic", Text: "问题"}, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		return agentgateway.Response{}, transient
	})
	if !errors.Is(err, transient) || errors.Is(err, ErrInvalidResult) {
		t.Fatal("transport error misclassified as bad content", err)
	}
}
