package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/contentpools"
	"strings"
	"testing"
)

func TestPoolContentLayoutDoesNotOwnIdentityOrBlockContent(t *testing.T) {
	prose := "讲清960g整只日常价92.6元，麻辣与五香可选。"
	for _, raw := range []string{prose, "- " + prose, `{"text":"` + prose + `","id":"forged","source_ids":["foreign"],"product_ids":[999]}`, `{"tasks":[{"selling_point":"任意标题","angle":"` + prose + `","id":"forged"}]}`, `["` + prose + `"]`, `{"任意外壳":"` + prose + `","product_ids":[999]}`, `{"任意外壳":"` + prose + `"`} {
		text := poolReplyContent(raw)
		if !strings.Contains(text, prose) || strings.Contains(text, "forged") {
			t.Fatal(raw, text)
		}
		p := contentpools.Pool{Key: "product:6", Products: []int64{6}, Facts: []contentpools.Fact{{ID: "trusted-origin", Text: "当前确认材料"}}}
		got := bindPoolContent(p, poolContent{Point: "程序选定标题", Angle: text}, nil)
		if got.ID == "forged" || got.Point != "程序选定标题" || len(got.Sources) != 1 || got.Sources[0] != "trusted-origin" {
			t.Fatal(got)
		}
	}
	// A different or broken envelope remains content to review, never a schema veto.
	raw := "其它结构的自然文字"
	if poolReplyContent(raw) != raw {
		t.Fatal("content was discarded for layout")
	}
}
func TestPoolVerdictReadsMeaningWithoutRequiringSchema(t *testing.T) {
	for _, raw := range []string{"通过。事实和条件一致。", "**审核结论：可以保留**\n没有新增用途。", "这条内容正确，可以入池。", "没有问题，可以保留。", "没有发现问题。", "不存在内容错误。", "未违反P0，内容符合事实。", "审核说明\n\n结论：通过。", `{"passed":true}`} {
		j := readPoolJudgment(raw, false)
		if !j.known || !j.passed {
			t.Fatal(raw, j)
		}
	}
	for _, raw := range []string{"有问题：遗漏了退款的未使用条件。", "不通过。无依据保温12小时。", "通过不了，内容不符合事实。", "并非没有问题，遗漏条件。", `{"passed":false}`} {
		j := readPoolJudgment(raw, false)
		if !j.known || j.passed || j.reason == "" {
			t.Fatal(raw, j)
		}
	}
	for _, raw := range []string{"需要更多资料，无法判断。", "不确定是否通过。", "能否通过？", "the passage is a description.", "这是一段讨论，还没有判断结论。"} {
		if readPoolJudgment(raw, false).known {
			t.Fatal("unfinished judgment approved", raw)
		}
	}
	for _, raw := range []string{"这条资料适用于本商品。", "可以用于本商品，因为是同品牌背景。"} {
		j := readPoolJudgment(raw, true)
		if !j.known || !j.passed {
			t.Fatal(raw, j)
		}
	}
	for _, raw := range []string{"不适用，钢材不是饼干原料。", "不可用于本商品，主体不同。"} {
		j := readPoolJudgment(raw, true)
		if !j.known || j.passed {
			t.Fatal(raw, j)
		}
	}
}
func TestPoolUnitsUseProgramProvenanceNotTextOrPosition(t *testing.T) {
	p := contentpools.Pool{Facts: []contentpools.Fact{{ID: "a", Text: "规格", Sources: []string{"card"}}, {ID: "b", Text: "价格", Sources: []string{"card"}}, {ID: "c", Text: "同名资料", Sources: []string{"other"}}}}
	source := contentpools.Sources{Rows: []contentpools.SourceRow{{ID: "card", Label: "商品卡"}, {ID: "other", Fields: map[string]string{"名称": "售后条件"}}}}
	units := poolContentUnits(p, source)
	if len(units) != 2 || len(units[0].facts) != 2 || units[0].title != "商品卡" || units[1].title != "售后条件" {
		t.Fatal(units)
	}
}
func TestPoolP0InstructionIsSeparatedFromFactMaterial(t *testing.T) {
	p := contentpools.Pool{Facts: []contentpools.Fact{{ID: "trusted-origin", Text: "客观事实"}}, Products: []int64{6}}
	packet := poolMaterial(p, []contentpools.Product{{ID: 6, Name: "本商品", Link: "2"}})
	data, _ := json.Marshal(packet)
	for _, key := range []string{"trusted-origin", "source_ids", "product_ids", `"id"`} {
		if strings.Contains(string(data), key) {
			t.Fatal(string(data))
		}
	}
	messages := poolMessages("当前内容任务", "完整有效P0", packet)
	if len(messages) != 2 || messages[0].Content != "完整有效P0\n\n当前内容任务" || strings.Contains(messages[1].Content, "P0") {
		t.Fatal(messages)
	}
	if (contentpools.Set{Notices: []contentpools.Notice{{Kind: "excluded"}}}).HasUnfinishedContent() {
		t.Fatal("content rejection made retry mandatory")
	}
}

func TestPoolUnfinishedSiblingNeverVetoesApprovedDraftSave(t *testing.T) {
	source := contentpools.Sources{Products: []contentpools.Product{{ID: 1, Name: "当前商品", Link: "1"}}}
	set := contentpools.Set{ID: 10, SourceSHA: source.SHA(), Notices: []contentpools.Notice{{Kind: "unavailable", Retryable: true}}}
	if !set.HasUnfinishedContent() || contentPoolDraftState(set, source, 0) != "ready" || contentPoolDraftState(set, source, 10) != "synced" {
		t.Fatal("optional retry blocks valid save", set)
	}
	changed := source
	changed.Products = []contentpools.Product{{ID: 1, Name: "已经换品", Link: "1"}}
	if contentPoolDraftState(set, changed, 10) != "needs_update" {
		t.Fatal("stale material approved for changed product")
	}
	if contentPoolDraftState(contentpools.Set{}, source, 0) != "needs_update" {
		t.Fatal("missing draft approved")
	}
}
