package interactionplan

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func assertNoAdditionalP0(t *testing.T, s Snapshot, req agentgateway.Request) {
	t.Helper()
	for _, message := range req.Messages {
		for _, forbidden := range []string{s.P0, s.P0Markdown, "【本场有效P0】"} {
			if forbidden != "" && strings.Contains(message.Content, forbidden) {
				t.Fatal("program-side P0 leaked into generation request")
			}
		}
	}
}

func TestGenerationOmitsAdditionalP0AcrossInteractionKinds(t *testing.T) {
	for _, kind := range []string{"welcome", "welcome_named", "welcome_batch", "semantic", "dialogue", "urgent", "engagement", "guide", "time_report"} {
		t.Run(kind, func(t *testing.T) {
			s := fixture(t)
			calls := 0
			_, body, err := Generate(context.Background(), s, Input{Kind: kind, Text: "按当前方案回答", AllowClarify: true}, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
				calls++
				assertNoAdditionalP0(t, s, req)
				if !strings.Contains(req.Messages[0].Content, s.Prompt) {
					t.Fatal("author's frozen plan prompt was changed")
				}
				return agentgateway.Response{Text: "意图：说明发货\n类型：闲聊\n歧义：否\n澄清句：\n话术：一般次日上午发货。"}, nil
			})
			if err != nil || body != "一般次日上午发货。" || calls != 1 {
				t.Fatal(body, err, calls)
			}
		})
	}
}

func TestP0StillRejectsReturnedSpeechAndClarification(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"speech", "意图：询问\n类型：提问\n歧义：否\n澄清句：\n话术：保证百分百有效。"},
		{"clarification", "意图：核对\n类型：提问\n歧义：是\n话术：\n澄清句：您问的是保证百分百有效吗？"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			calls := 0
			_, body, err := Generate(context.Background(), s, Input{Kind: "semantic", Text: "咨询", AllowClarify: true}, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
				calls++
				assertNoAdditionalP0(t, s, req)
				return agentgateway.Response{Text: tc.raw}, nil
			})
			if err == nil || !strings.Contains(err.Error(), "P0") || body != "" || calls != 1 {
				t.Fatal("invalid generated content escaped post-check or was regenerated", body, err, calls)
			}
		})
	}
}

func TestP0RejectsOnlyAffectedBatchItemAfterGeneration(t *testing.T) {
	s := fixture(t)
	calls := 0
	got, err := GenerateBatch(context.Background(), s, batchFixture(2), func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		calls++
		assertNoAdditionalP0(t, s, req)
		return agentgateway.Response{Text: blockText("U01", "保证百分百有效。") + blockText("U02", "一般次日上午发货。")}, nil
	})
	if err != nil || calls != 1 || got.Items[0].Err == nil || got.Items[0].Body != "" || got.Items[1].Err != nil || got.Items[1].Body != "一般次日上午发货。" {
		t.Fatal(got, err, calls)
	}
}

func TestProgramJoinKeepsP0CheckWithoutModelCall(t *testing.T) {
	s := fixture(t)
	for _, tc := range []struct{ address, body string }{
		{"小王", "保证百分百有效。"},
		{"保证百分百有效", "一般次日上午发货。"},
	} {
		joined, err := Join(context.Background(), s, tc.address, tc.body, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
			t.Fatal("program join called a model")
			return agentgateway.Response{}, nil
		})
		if err == nil || !strings.Contains(err.Error(), "P0") || joined != "" {
			t.Fatal("joined speech bypassed post-check", joined, err)
		}
	}
}

func TestRequestBuilderOmitsAdditionalP0AndRetainsPlanContract(t *testing.T) {
	s := fixture(t)
	req, err := request(s, "interaction-v2-address-join.md", "interaction_address_join", map[string]string{"body": "一般次日上午发货。"}, agentgateway.ResponseText)
	if err != nil {
		t.Fatal(err)
	}
	assertNoAdditionalP0(t, s, req)
	if !strings.Contains(req.Messages[0].Content, s.Prompt) || !strings.Contains(req.Messages[0].Content, OutputContract) {
		t.Fatal("request lost frozen plan or output contract")
	}
}
