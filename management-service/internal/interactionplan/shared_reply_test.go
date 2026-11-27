package interactionplan

import (
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func TestSharedReplyUsesExactlyLegacyParser(t *testing.T) {
	for _, raw := range []string{
		"意图：咨询价格\n类型：提问\n歧义：否\n澄清句：\n话术：当前价格80元。",
		"意图：请求晚点发货\n类型：请求\n歧义：是\n话术：\n澄清句：想安排哪一天发？",
		"意图：准备购买\n类型：下单意向\n歧义：否\n澄清句：\n话术：可以先选适合自己的规格。",
	} {
		input := Input{Kind: "semantic", Text: "观众原话", AllowClarify: true}
		response := agentgateway.Response{Text: raw, FinishReason: "stop"}
		want := parseBatch(fixture(t), []BatchItem{{ID: "U01", Input: input}}, raw, "stop").Items[0]
		got, err := ParseReply(fixture(t), input, response)
		if err != nil || got.Body != want.Body || got.Intent.Type != want.Intent.Type || got.Intent.NeedClarify != want.Intent.NeedClarify || got.Parse.RawSHA256 != want.Parse.RawSHA256 {
			t.Fatal(got, want, err)
		}
	}
}
