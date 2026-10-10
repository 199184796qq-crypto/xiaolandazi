package contentpools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCorrectionContentOnlyAndAmbiguity(t *testing.T) {
	for _, c := range []CorrectionContent{{Summary: "取消这一段", Cancellation: "本次所选内容不要"}, {Summary: "按用户确认的值更新", Fact: "额定功率800W", Forbidden: "不再称为1100W"}, {Summary: "需确认范围", Fact: "未采用草稿", Question: "是仅这段不用，还是以后不讲这个话题？"}} {
		b, _ := json.Marshal(c)
		out, e := ParseCorrection(string(b))
		if e != nil || out != c {
			t.Fatal(out, e)
		}
	}
	for _, bad := range []string{`{"理解说明":"","事实修正":"","表达限制":"","展开指导":"","取消说明":"","澄清问题":""}`, `{"理解说明":"保存","事实修正":"800W","表达限制":"","展开指导":"","取消说明":"","澄清问题":"","fact_id":"model-owned"}`, `{"理解说明":"未知格式"}`} {
		if _, e := ParseCorrection(bad); e == nil {
			t.Fatal("invalid result accepted", bad)
		}
	}
}
func TestCorrectionScopePartitionsCommonPoolWithoutChangingOtherProduct(t *testing.T) {
	source := Sources{Products: []Product{{ID: 1, Link: "1"}, {ID: 2, Link: "2"}}, Rows: []SourceRow{{ID: "origin"}}}
	original := Set{ID: 1, Sources: source, SourceSHA: source.SHA(), SafeFacts: "原始事实", Pools: []Pool{{Key: "common", Products: []int64{1, 2}, Facts: []Fact{{ID: "f", Text: "可机洗", Sources: []string{"origin"}, Products: []int64{1, 2}}}, Tasks: []Task{{ID: "t", Point: "护理", Angle: "解释清洗方法", Sources: []string{"f"}}}}}}
	next, e := PartitionCorrectionPools(original, []int64{1})
	if e != nil || len(next.Pools) != 2 {
		t.Fatal(next, e)
	}
	if e = ValidateSet(next); e != nil {
		t.Fatal(e)
	}
	next.Pools[0].Facts[0].Text = "只能手洗"
	next.Pools[0].Tasks[0].Disabled = true
	if next.Pools[1].Facts[0].Text != "可机洗" || original.Pools[0].Facts[0].Text != "可机洗" || original.Pools[0].Tasks[0].Disabled {
		t.Fatal("mutable or cross-product edit")
	}
	c := Correction{ID: "program-owned", Products: []int64{1}, Content: CorrectionContent{Summary: "纠正护理方式", Fact: "只能手洗", Forbidden: "不建议机洗"}}
	next.Sources.Corrections = []Correction{c}
	if len(CorrectionView(next.Sources, []int64{2})) != 0 {
		t.Fatal("correction leaked to other product")
	}
	wire, _ := json.Marshal(CorrectionView(next.Sources, []int64{1}))
	if strings.Contains(string(wire), "program-owned") || strings.Contains(string(wire), "products") {
		t.Fatal("internal identity on wire", string(wire))
	}
	if len(Changes(original.Sources, next.Sources)) == 0 {
		t.Fatal("correction changes disappeared")
	}
}
