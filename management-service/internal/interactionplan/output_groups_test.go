package interactionplan

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/planprompt"
)

func fiveGroup(id, body string, clarify bool, framing bool) string {
	content := "意图：咨询当前问题\n类型：提问\n歧义：否\n澄清句：\n话术：" + body
	if clarify {
		content = "意图：确认当前问题\n类型：提问\n歧义：是\n话术：\n澄清句：" + body
	}
	if id != "" {
		content = "编号：" + id + "\n" + content
	}
	if framing {
		content += "\n结束：" + id
	}
	return content
}

func TestFiveFieldSingleUsesUniqueSpokenValue(t *testing.T) {
	for _, tc := range []struct {
		name, raw, body, spoken string
		clarify                 bool
	}{
		{"answer", fiveGroup("", "按保存方案发货。", false, false), "按保存方案发货。", "话术", false},
		{"clarification", fiveGroup("", "您问的是发货时间，还是配送地区？", true, false), "您问的是发货时间，还是配送地区？", "澄清句", true},
		{"only_body", "话术：按保存方案发货。", "按保存方案发货。", "话术", false},
		{"missing_metadata", "意图：发货咨询\n话术：按保存方案发货。", "按保存方案发货。", "话术", false},
		{"invalid_metadata", "意图：发货咨询\n类型：未知类别\n歧义：不知道\n澄清句：\n话术：按保存方案发货。", "按保存方案发货。", "话术", false},
		{"wrong_ambiguity_body", "意图：发货咨询\n歧义：是\n澄清句：\n话术：按保存方案发货。", "按保存方案发货。", "话术", false},
		{"wrong_ambiguity_clarify", "意图：确认需求\n歧义：否\n话术：\n澄清句：您问的是发货时间，还是配送地区？", "您问的是发货时间，还是配送地区？", "澄清句", true},
		{"multiline", "意图：发货咨询\n类型：提问\n歧义：否\n澄清句：\n话术：\n按保存方案发货。\n详情可以咨询客服。", "按保存方案发货。\n详情可以咨询客服。", "话术", false},
		{"clarify_placeholder", "意图：确认需求\n澄清句：\n澄清句：您问的是发货时间，还是配送地区？\n话术：", "您问的是发货时间，还是配送地区？", "澄清句", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(1), tc.raw, "stop")
			item := got.Items[0]
			if item.Err != nil || item.Body != tc.body || item.Intent.NeedClarify != tc.clarify || item.Parse.SpokenField != tc.spoken || got.Groups != 1 {
				t.Fatal(got)
			}
			if tc.name == "answer" || tc.name == "clarification" {
				if len(item.Parse.Warnings) != 0 {
					t.Fatal("normal five-field output generated legacy warnings", item.Parse)
				}
			}
		})
	}
}

func TestFiveFieldFramingAndOrderFallback(t *testing.T) {
	a, b := "甲项按保存方案发货。", "乙项按商品卡说明。"
	for _, tc := range []struct {
		name, raw   string
		good        []bool
		assignments []string
	}{
		{"complete_pair_reversed", fiveGroup("U02", b, false, true) + "\n" + fiveGroup("U01", a, false, true), []bool{true, true}, []string{"item_id_pair", "item_id_pair"}},
		{"missing_ends_reversed", fiveGroup("U02", b, false, false) + "\n" + fiveGroup("U01", a, false, false), []bool{true, true}, []string{"item_id_content", "item_id_content"}},
		{"bare_ids", "U01\n" + fiveGroup("", a, false, false) + "\nU02\n" + fiveGroup("", b, false, false), []bool{true, true}, []string{"item_id_content", "item_id_content"}},
		{"missing_colons", "编号 U01\n" + fiveGroup("", a, false, false) + "\n结束U01\n编号U02\n" + fiveGroup("", b, false, false) + "\n结束 U02", []bool{true, true}, []string{"item_id_pair", "item_id_pair"}},
		{"noncanonical_ids", "U1\n" + fiveGroup("", a, false, false) + "\n结束U1\nU2\n" + fiveGroup("", b, false, false) + "\n结束U2", []bool{true, true}, []string{"group_order", "group_order"}},
		{"no_ids", fiveGroup("", a, false, false) + "\n" + fiveGroup("", b, false, false), []bool{true, true}, []string{"group_order", "group_order"}},
		{"bad_ids", fiveGroup("BAD_A", a, false, true) + "\n" + fiveGroup("BAD_B", b, false, true), []bool{true, true}, []string{"group_order", "group_order"}},
		{"duplicate_ids", fiveGroup("U01", a, false, true) + "\n" + fiveGroup("U01", b, false, true), []bool{true, true}, []string{"group_order", "group_order"}},
		{"one_missing_id", fiveGroup("U01", a, false, true) + "\n" + fiveGroup("", b, false, false), []bool{true, true}, []string{"item_id_pair", "group_order"}},
		{"wrong_end", strings.Replace(fiveGroup("U01", a, false, true), "结束：U01", "结束：U09", 1) + "\n" + fiveGroup("U02", b, false, true), []bool{true, true}, []string{"group_order", "item_id_pair"}},
		{"only_end_ids_reversed", fiveGroup("", b, false, false) + "\n结束：U02\n" + fiveGroup("", a, false, false) + "\n结束：U01", []bool{true, true}, []string{"end_id", "end_id"}},
		{"valid_id_conflicts_with_position", fiveGroup("U02", b, false, true) + "\n" + fiveGroup("", a, false, false), []bool{false, true}, []string{"", "item_id_pair"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(2), tc.raw, "stop")
			if got.Groups != 2 {
				t.Fatal(got)
			}
			for i, item := range got.Items {
				if (item.Err == nil) != tc.good[i] {
					t.Fatal(got)
				}
				if !tc.good[i] {
					if item.Body != "" {
						t.Fatal("unassigned answer became playable", item)
					}
					continue
				}
				want := a
				if i == 1 {
					want = b
				}
				if item.Body != want || item.Parse.Assignment != tc.assignments[i] || item.Parse.GroupCount != 2 {
					t.Fatal(item)
				}
				if item.Parse.Assignment == "group_order" && !hasParseWarning(item.Parse, "identity_recovered_by_group_order") {
					t.Fatal("fallback not observable", item.Parse)
				}
			}
		})
	}
}

func TestFiveFieldIncompleteCountsNeverShiftPeople(t *testing.T) {
	// Three inputs, but only two unlabelled groups: their positions cannot
	// distinguish a missing first/middle/last person, even with valid text.
	unlabelled := fiveGroup("", "甲项回复。", false, false) + "\n" + fiveGroup("", "丙项回复。", false, false)
	cases := []string{
		unlabelled,
		fiveGroup("U01", "甲项回复。", false, true) + "\n" + fiveGroup("", "丙项回复。", false, false),
		fiveGroup("U01", "甲项回复。", false, true) + "\n编号：BAD\n意图：只有分析，没有正文\n" + fiveGroup("U03", "丙项回复。", false, true),
	}
	for n, raw := range cases {
		got := parseBatch(fixture(t), batchFixture(3), raw, "stop")
		if got.Items[1].Err == nil || got.Items[1].Body != "" || got.UnmappedGroups == 0 {
			t.Fatal(n, got)
		}
		if n == 0 {
			for _, item := range got.Items {
				if item.Err == nil {
					t.Fatal(got)
				}
			}
		}
		if n > 0 && got.Items[0].Err != nil {
			t.Fatal("lost complete framed predecessor", got)
		}
	}
	raw := fiveGroup("", "一。", false, false) + "\n" + fiveGroup("", "二。", false, false) + "\n" + fiveGroup("", "多余。", false, false)
	got := parseBatch(fixture(t), batchFixture(2), raw, "stop")
	if got.UnmappedGroups != 3 || got.Items[0].Err == nil || got.Items[1].Err == nil {
		t.Fatal(got)
	}
}

func TestFiveFieldMixedSpeechAndClarificationCounts(t *testing.T) {
	raw := fiveGroup("U01", "按方案发货。", false, true) + "\n" + fiveGroup("U02", "您问发货时间，还是配送地区？", true, true) + "\n意图：口令参与\n类型：主动互动\n歧义：否\n澄清句：\n话术："
	got := parseBatch(fixture(t), batchFixture(3), raw, "stop")
	if got.Groups != 3 || got.SpeechGroups != 1 || got.ClarificationGroups != 1 || got.UnmappedGroups != 0 || got.Items[2].Parse.Outcome != "no_speech" || got.Items[2].Parse.Assignment != "group_order" {
		t.Fatal(got)
	}
	items := batchFixture(3)
	items[1].AllowClarify = false
	got = parseBatch(fixture(t), items, raw, "stop")
	if got.Items[1].Err == nil || got.ClarificationGroups != 0 || got.Items[0].Err != nil || got.Items[2].Err != nil {
		t.Fatal(got)
	}
}

func TestFiveFieldVoiceConflictsAndRetiredAuxiliaryMetadata(t *testing.T) {
	base := fiveGroup("U01", "按保存方案发货。", false, true)
	for name, raw := range map[string]string{
		"type":      strings.Replace(base, "类型：提问", "类型：提问\n类型：刷屏", 1),
		"ambiguity": strings.Replace(base, "歧义：否", "歧义：否\n歧义：是", 1),
		"relation":  strings.Replace(base, "类型：", "对话关系：new\n对话关系：related\n类型：", 1),
		"continue":  strings.Replace(base, "类型：", "继续对话：是\n继续对话：否\n类型：", 1),
		"unknown":   strings.Replace(base, "结束：", "备注：辅助说明不口播\n结束：", 1),
		"priority":  strings.Replace(base, "类型：提问", "类型：投诉\n优先程度：紧急\n优先原因：模型要求升级", 1),
	} {
		t.Run(name, func(t *testing.T) {
			got := parseBatch(fixture(t), batchFixture(1), raw, "stop")
			item := got.Items[0]
			if item.Err != nil || item.Body != "按保存方案发货。" || item.Intent.Priority != "普通" || item.Intent.PriorityReason != "" {
				t.Fatal(item)
			}
		})
	}
	for _, tc := range []struct {
		raw, want string
		bad       bool
	}{
		{"意图：咨询\n歧义：否\n澄清句：您问哪个地区？\n话术：按方案发货。", "按方案发货。", false},
		{"意图：确认\n歧义：是\n话术：按方案发货。\n澄清句：您问哪个地区？", "您问哪个地区？", false},
		{"意图：不明确\n话术：按方案发货。\n澄清句：您问哪个地区？", "", true},
		{"意图：不明确\n歧义：可能\n话术：按方案发货。\n澄清句：您问哪个地区？", "", true},
		{"意图：咨询\n话术：甲正文。\n回答：乙正文。", "", true},
		{"意图：确认\n话术：\n澄清句：您问哪个地区？\n澄清句：您问哪个订单？", "", true},
		{"意图：咨询\n话术：保证百分百有效。", "", true},
		{"意图：确认\n澄清句：保证百分百有效。", "", true},
		{"意图：咨询\n话术：" + strings.Repeat("长", 361), "", true},
	} {
		got := parseBatch(fixture(t), batchFixture(1), tc.raw, "stop")
		if (got.Items[0].Err != nil) != tc.bad || got.Items[0].Body != tc.want {
			t.Fatal(got)
		}
	}
}

func TestFiveFieldKnownCutDropsOnlyUnboundedTail(t *testing.T) {
	for _, reason := range []string{"length", "max_tokens", "incomplete", "content_filter", "unknown"} {
		// First group is bounded by the next intent, not an end marker or ID.
		raw := fiveGroup("", "甲项完整正文。", false, false) + "\n" + fiveGroup("", "乙项疑似截断。", false, false)
		got := parseBatch(fixture(t), batchFixture(2), raw, reason)
		if got.Items[0].Err != nil || got.Items[0].Body != "甲项完整正文。" || got.Items[1].Err == nil || got.Items[1].Body != "" || got.Items[0].Parse.Assignment != "group_order" {
			t.Fatal(reason, got)
		}
	}
	raw := fiveGroup("", "甲项完整正文。", false, false) + "\n" + fiveGroup("", "乙项完整正文。", false, false)
	got := parseBatch(fixture(t), batchFixture(2), raw, "")
	if got.Items[0].Err != nil || got.Items[1].Err != nil || !hasParseWarning(got.Items[1].Parse, "supplier_status_unknown") {
		t.Fatal(got)
	}
}

func TestFiveFieldRandomMissingFramingPreservesInputOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(20261104))
	s := fixture(t)
	for iteration := 0; iteration < 400; iteration++ {
		n := 1 + rng.Intn(10)
		var groups []string
		for person := 1; person <= n; person++ {
			id := fmt.Sprintf("U%02d", person)
			framing := rng.Intn(2) == 0
			// Use one common framing failure for all identities to avoid introducing
			// contradictory *valid* labels. Mixed valid labels are tested explicitly.
			if iteration%2 == 0 {
				id = ""
				framing = false
			} else {
				id = "INVALID"
			}
			body := fmt.Sprintf("第%c项按方案处理。", 'A'+person-1)
			group := fiveGroup(id, body, person%2 == 0, framing)
			if rng.Intn(2) == 0 {
				group = strings.ReplaceAll(group, "：", ":")
			}
			groups = append(groups, group)
		}
		got := parseBatch(s, batchFixture(n), strings.Join(groups, "\n"), "stop")
		if got.Groups != n || got.UnmappedGroups != 0 {
			t.Fatal(iteration, got)
		}
		for i, item := range got.Items {
			if item.Err != nil || item.Body != fmt.Sprintf("第%c项按方案处理。", 'A'+i) || item.Intent.NeedClarify != (i%2 == 1) {
				t.Fatal(iteration, item)
			}
		}
	}
}

func TestFiveFieldActualRequestsKeepSingleCallAndOneContract(t *testing.T) {
	for _, n := range []int{1, 2, 10} {
		calls := 0
		got, err := GenerateBatch(context.Background(), fixture(t), batchFixture(n), func(_ context.Context, req agentgateway.Request) (agentgateway.Response, error) {
			calls++
			system := req.Messages[0].Content
			contract := planprompt.BatchInteractionOutputContract
			if n == 1 {
				contract = planprompt.InteractionOutputContract
			}
			if req.Stage != "interaction_purpose_resolution" || req.MaxTokens != 900*n || len(req.Messages) != 2 || strings.Count(system, "【输出格式") != 1 || !strings.Contains(system, contract) {
				t.Fatal(req)
			}
			for _, retired := range []string{"置信度：", "优先程度：", "优先原因：", "候选：", "话术要求：", "对话关系：", "继续对话："} {
				if strings.Contains(system, retired) {
					t.Fatal("retired model output requested", retired)
				}
			}
			var groups []string
			for person := 1; person <= n; person++ {
				groups = append(groups, fiveGroup("", fmt.Sprintf("第%c项按方案处理。", 'A'+person-1), false, false))
			}
			return agentgateway.Response{Text: strings.Join(groups, "\n"), FinishReason: "stop"}, nil
		})
		if err != nil || calls != 1 || len(got.Items) != n {
			t.Fatal(got, err, calls)
		}
		for _, item := range got.Items {
			if item.Err != nil || item.Body == "" {
				t.Fatal(item)
			}
		}
	}
}

func TestCompletePairWinsOverDanglingDuplicateHeader(t *testing.T) {
	// The complete U01 pair arrives second. The earlier malformed U01 header
	// cannot take its body or force an input-order overwrite.
	raw := fiveGroup("U01", "乙项的残缺编号回复。", false, false) + "\n" + fiveGroup("U01", "甲项完整编号回复。", false, true)
	got := parseBatch(fixture(t), batchFixture(2), raw, "stop")
	if got.Items[0].Err != nil || got.Items[0].Body != "甲项完整编号回复。" || got.Items[0].Parse.Assignment != "item_id_pair" || got.Items[1].Err == nil || got.Items[1].Body != "" {
		t.Fatal(got)
	}
	// In input order the intact first pair remains authoritative; the dangling
	// second label may recover the still-unclaimed second task by content order.
	raw = fiveGroup("U01", "甲项完整编号回复。", false, true) + "\n" + fiveGroup("U01", "乙项的残缺编号回复。", false, false)
	got = parseBatch(fixture(t), batchFixture(2), raw, "stop")
	if got.Items[0].Err != nil || got.Items[0].Body != "甲项完整编号回复。" || got.Items[0].Parse.Assignment != "item_id_pair" || got.Items[1].Err != nil || got.Items[1].Body != "乙项的残缺编号回复。" || got.Items[1].Parse.Assignment != "group_order" {
		t.Fatal(got)
	}
}

func TestExplicitEmptySpokenMarkersDoNotBecomeConflictingSpeech(t *testing.T) {
	for _, marker := range []string{"留空", "（留空）", "(留空)", "[留空]", "【留空】"} {
		t.Run(marker, func(t *testing.T) {
			for _, clarify := range []bool{false, true} {
				raw := fiveGroup("U01", "按保存方案说明。", clarify, false)
				if clarify {
					raw += "\n澄清句：" + marker + "\n结束：U01"
				} else {
					raw += "\n话术：" + marker + "\n结束：U01"
				}
				got := parseBatch(fixture(t), batchFixture(1), raw, "stop")
				if got.Items[0].Err != nil || got.Items[0].Body != "按保存方案说明。" || !hasParseWarning(got.Items[0].Parse, "empty_spoken_placeholder") {
					t.Fatal(got)
				}
				if len(got.Items[0].Parse.BodyRanges) != 1 {
					t.Fatal("placeholder must not claim a spoken range", got)
				}
			}
		})
	}
	for _, body := range []string{"无", "请把备注留空。", "不需要留空，按页面填写。"} {
		got := parseBatch(fixture(t), batchFixture(1), fiveGroup("U01", body, false, true), "stop")
		if got.Items[0].Err != nil || got.Items[0].Body != body {
			t.Fatal("ordinary answer was erased", got)
		}
	}
	for _, raw := range []string{"意图：咨询\n话术：留空", "意图：确认需求\n歧义：是\n澄清句：（留空）"} {
		got := parseBatch(fixture(t), batchFixture(1), raw, "stop")
		if got.Items[0].Err == nil || got.Items[0].Body != "" {
			t.Fatal("empty marker became speech", got)
		}
	}
	got := parseBatch(fixture(t), batchFixture(1), "意图：确认需求\n歧义：是\n话术：留空\n澄清句：您问哪个地区？", "stop")
	if got.Items[0].Err != nil || !got.Items[0].Intent.NeedClarify || got.Items[0].Body != "您问哪个地区？" {
		t.Fatal(got)
	}
	got = parseBatch(fixture(t), batchFixture(1), "意图：确认需求\n歧义：是\n澄清句：您问哪个地区？\n澄清句：您问哪个订单？\n话术：留空", "stop")
	if got.Items[0].Err == nil {
		t.Fatal("two genuine clarifications were hidden", got)
	}
}
