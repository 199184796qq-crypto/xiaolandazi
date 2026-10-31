package interactionplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func batchFixture(n int) []BatchItem {
	items := make([]BatchItem, n)
	for i := range items {
		in := Input{Kind: "semantic", Text: fmt.Sprintf("第%d个人的问题", i+1), CurrentMainline: "公共讲解", AllowClarify: true}
		for j := 0; j < 5; j++ {
			in.Context = append(in.Context, ContextMessage{Role: "user", Text: fmt.Sprintf("第%d个人的上下文%d", i+1, j)})
		}
		items[i] = BatchItem{ID: fmt.Sprintf("U%02d", i+1), Input: in}
	}
	return items
}
func blockText(id, body string) string {
	return fmt.Sprintf("编号：%s\n意图：咨询问题\n类型：提问\n优先程度：普通\n歧义：否\n回答：%s\n结束：%s\n", id, body, id)
}

func TestBatchOneThroughTenUsesOneCallAndIndependentContexts(t *testing.T) {
	for n := 1; n <= 10; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			items := batchFixture(n)
			calls := 0
			s := fixture(t)
			got, err := GenerateBatch(context.Background(), s, items, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
				calls++
				if len(req.Messages) != 2 || strings.Count(req.Messages[0].Content, s.Prompt) != 1 || req.Stage != "interaction_purpose_resolution" || req.MaxTokens != 900*n {
					t.Fatalf("wrong shared prompt/budget: %+v", req)
				}
				if strings.Count(req.Messages[0].Content, s.P0) != 1 {
					t.Fatal("batch must carry one complete frozen P0")
				}
				var payload struct {
					Items    []BatchItem `json:"items"`
					Mainline string      `json:"current_mainline"`
				}
				if err := json.Unmarshal([]byte(req.Messages[1].Content), &payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.Items) != n || payload.Mainline != "公共讲解" || strings.Count(req.Messages[1].Content, "公共讲解") != 1 {
					t.Fatal(payload)
				}
				for i, item := range payload.Items {
					if item.ID != items[i].ID || len(item.Context) != 5 {
						t.Fatal(item)
					}
					for _, m := range item.Context {
						if !strings.HasPrefix(m.Text, fmt.Sprintf("第%d个人的", i+1)) {
							t.Fatal("cross-user context", item)
						}
					}
				}
				text := ""
				// Deliberately reverse the model return order.
				for i := n - 1; i >= 0; i-- {
					text += blockText(items[i].ID, fmt.Sprintf("问题%c按当前方案说明。", 'A'+i))
				}
				return agentgateway.Response{Text: text}, nil
			})
			if err != nil || calls != 1 || len(got.Items) != n {
				t.Fatal(got, err, calls)
			}
			for i, item := range got.Items {
				if item.Err != nil || item.ID != items[i].ID || item.Body != fmt.Sprintf("问题%c按当前方案说明。", 'A'+i) {
					t.Fatal("wrong person/result", item)
				}
			}
		})
	}
}

func TestBatchPartialMalformedNeverOverwritesOrMisbinds(t *testing.T) {
	s := fixture(t)
	items := batchFixture(3)
	for _, tc := range []struct {
		name, raw string
		good      []bool
		issue     bool
	}{
		{"missing", blockText("U03", "三。") + blockText("U01", "一。"), []bool{true, false, true}, false},
		{"duplicate", blockText("U01", "一。") + blockText("U01", "伪造的覆盖。") + blockText("U02", "二。"), []bool{false, true, false}, true},
		{"unknown", blockText("U09", "未知。") + blockText("U01", "一。"), []bool{true, false, false}, true},
		{"duplicate_field", "编号：U01\n意图：咨询\n回答：一。\n回答：不能覆盖。\n结束：U01\n" + blockText("U03", "三。"), []bool{false, false, true}, false},
		{"truncated", blockText("U01", "一。") + "编号：U02\n意图：咨询\n回答：未结束。", []bool{true, false, false}, false},
		{"flat_repeated", "意图：一\n回答：一。\n意图：二\n回答：二。", []bool{false, false, false}, false},
		{"wrong_end", "编号：U01\n意图：咨询\n回答：一。\n结束：U02\n" + blockText("U03", "三。"), []bool{false, false, true}, true},
		{"missing_type", "编号：U01\n意图：咨询\n歧义：否\n回答：一。\n结束：U01\n" + blockText("U03", "三。"), []bool{false, false, true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBatch(s, items, tc.raw)
			for i, want := range tc.good {
				if (got.Items[i].Err == nil) != want {
					t.Fatal(i, got)
				}
			}
			if tc.issue && len(got.Issues) == 0 {
				t.Fatal("missing format issue")
			}
		})
	}
}

func TestSingleCompatibilityUsesBatchAndRejectsRepeatedFlatFields(t *testing.T) {
	s := fixture(t)
	for _, raw := range []string{"意图：咨询\n歧义：否\n回答：正常说明。", blockText("U01", "正常说明。")} {
		calls := 0
		_, body, err := Generate(context.Background(), s, Input{Kind: "semantic", Text: "问题"}, func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
			calls++
			if !strings.Contains(req.Messages[1].Content, `"items"`) {
				t.Fatal("single bypasses batch")
			}
			return agentgateway.Response{Text: raw}, nil
		})
		if err != nil || body != "正常说明。" || calls != 1 {
			t.Fatal(body, err, calls)
		}
	}
	got := parseBatch(s, batchFixture(1), "意图：甲\n回答：甲。\n意图：乙\n回答：乙。")
	if got.Items[0].Err == nil {
		t.Fatal("single allowed silent overwrite")
	}
}

func TestBatchClassifiesChatSpamClarificationAndModelPriorityHint(t *testing.T) {
	s := fixture(t)
	items := batchFixture(4)
	raw := "编号：U01\n意图：寒暄\n类型：闲聊\n歧义：否\n回答：晚上好。\n结束：U01\n" +
		"编号：U02\n意图：无意义重复\n类型：刷屏\n歧义：否\n回答：\n结束：U02\n" +
		"编号：U03\n意图：地点或订单有歧义\n类型：提问\n歧义：是\n澄清句：您说的是新建区，还是新建订单？\n结束：U03\n" +
		"编号：U04\n意图：商品损坏\n类型：投诉\n优先程度：紧急\n优先原因：需要尽快处理售后损坏问题\n歧义：否\n回答：可以联系客服核实处理。\n结束：U04\n"
	got := parseBatch(s, items, raw)
	for _, v := range got.Items {
		if v.Err != nil {
			t.Fatal(v)
		}
	}
	if got.Items[0].Intent.Question || got.Items[1].Intent.Relevant || got.Items[1].Body != "" || !got.Items[2].Intent.NeedClarify || got.Items[3].Intent.Priority != "紧急" {
		t.Fatal(got)
	}
	items[2].AllowClarify = false
	got = parseBatch(s, items, raw)
	if got.Items[2].Err == nil || got.Items[3].Err != nil {
		t.Fatal("repeated clarification leaked", got)
	}
}

func TestBatchRejectsBoundsAndContextBeforeModel(t *testing.T) {
	s := fixture(t)
	cases := [][]BatchItem{nil, batchFixture(11), batchFixture(2), batchFixture(1), batchFixture(1)}
	cases[2][1].ID = "U01"
	cases[3][0].Context = append(cases[3][0].Context, ContextMessage{Role: "user", Text: "第六条"})
	cases[4][0].Context[0].Role = "system"
	for _, items := range cases {
		calls := 0
		_, err := GenerateBatch(context.Background(), s, items, func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
			calls++
			return agentgateway.Response{}, nil
		})
		if err == nil || calls != 0 {
			t.Fatal("invalid batch called model", err, calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GenerateBatch(ctx, s, batchFixture(1), nil); err == nil {
		t.Fatal("cancelled generation")
	}
	_, err := GenerateBatch(context.Background(), s, batchFixture(2), func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		return agentgateway.Response{}, errors.New("provider unavailable")
	})
	if err == nil {
		t.Fatal("provider failure hidden")
	}
}

func TestBatchLateModelResultAndPlanFormatReplacement(t *testing.T) {
	s := fixture(t)
	s.Prompt += "\n【输出格式要求】\n旧格式不能覆盖本次协议\n"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := GenerateBatch(ctx, s, batchFixture(1), func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		if strings.Contains(req.Messages[0].Content, "旧格式不能覆盖") {
			t.Fatal("old format remains")
		}
		cancel()
		return agentgateway.Response{Text: blockText("U01", "正常。")}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBatchReplacesFormatSectionsButKeepsBusinessInstructions(t *testing.T) {
	input := "【事实】\n次日上午发货。\n【输出格式要求】\n旧JSON规则\n【风格】\n自然说话。\n【输出格式】\n旧单人字段\n【其他约束】\n不要承诺到货时间。"
	got := stripFormatSection(input)
	for _, want := range []string{"次日上午发货。", "自然说话。", "不要承诺到货时间。"} {
		if !strings.Contains(got, want) {
			t.Fatal("lost business rule", got)
		}
	}
	if strings.Contains(got, "旧") {
		t.Fatal("old formats remain", got)
	}
	inline := "文中提到【输出格式要求】只是举例。"
	if stripFormatSection(inline) != inline {
		t.Fatal("stripped inline business text")
	}
}
