package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/contentpools"
)

func TestPoolContentHistoricalEmptyBenefitDoesNotBlockValidRows(t *testing.T) {
	// Actual job 20 returned these blank rows when old shipping benefits lacked
	// current evidence. The old all-table decoder failed the entire update.
	raw := `{"tasks":[{"id":"old-price","selling_point":"规格与价格","angle":"960g整只日常价92.6元","source_ids":[1,2]},{"id":"old-shipping","selling_point":"包邮福利","angle":"","source_ids":[]},{"selling_point":"包邮福利","angle":"","source_ids":[]},{"selling_point":"口味选择","angle":"麻辣与五香可选","source_ids":[5]}]}`
	rows, notes := decodePoolContent(raw)
	if len(rows) != 2 || len(notes) != 2 {
		t.Fatal(rows, notes)
	}
	p := contentpools.Pool{Key: "product:6", Products: []int64{6}, Facts: []contentpools.Fact{{ID: "trusted-origin", Text: "当前确认材料"}}}
	got := bindPoolContent(p, rows[0], nil)
	if got.ID == "old-price" || len(got.Sources) != 1 || got.Sources[0] != "trusted-origin" {
		t.Fatal("model metadata trusted", got)
	}
	rawContent, _ := json.Marshal(poolMaterial(p, []contentpools.Product{{ID: 6, Name: "礼品装", Link: "2"}}))
	for _, key := range []string{"trusted-origin", "source_ids", "product_ids", "\"id\""} {
		if strings.Contains(string(rawContent), key) {
			t.Fatal("internal references leaked", string(rawContent))
		}
	}
}
func TestPoolMalformedAndZeroContentStayOutsideExecutablePool(t *testing.T) {
	for _, raw := range []string{`{"tasks":[null,{"selling_point":"甲","angle":5},{"selling_point":"合格","angle":"有效内容"}]}`, `{"tasks":[{"selling_point":"合格","angle":"有效内容"},"bad"]}`} {
		rows, notes := decodePoolContent(raw)
		if len(rows) != 1 || len(notes) == 0 {
			t.Fatal(rows, notes)
		}
	}
	rows, notes := decodePoolContent(`{"tasks":[]}`)
	if len(rows) != 0 || len(notes) != 0 {
		t.Fatal(rows, notes)
	}
	if (contentpools.Set{Notices: []contentpools.Notice{{Kind: "excluded"}}}).HasUnfinishedContent() {
		t.Fatal("content rejection made retry mandatory")
	}
	if !(contentpools.Set{Notices: []contentpools.Notice{{Retryable: true}}}).HasUnfinishedContent() {
		t.Fatal("incomplete review lost retry")
	}
}

func TestPoolP0InstructionIsSeparatedFromFactMaterial(t *testing.T) {
	packet := map[string]any{"facts": []string{"客观事实"}, "product": map[string]string{"name": "本商品"}}
	messages := poolMessages("当前内容任务", "完整有效P0", packet)
	if len(messages) != 2 || messages[0].Role != "system" || messages[0].Content != "完整有效P0\n\n当前内容任务" || messages[1].Role != "user" || strings.Contains(messages[1].Content, "P0") {
		t.Fatal(messages)
	}
}
