package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	"strings"
	"testing"
)

func TestClarificationNeverRepairsDraft(t *testing.T) {
	set := taskFactPoolFixture()
	c := contentpools.Correction{Content: contentpools.CorrectionContent{Question: "哪个范围？", Fact: "未采用草稿"}}
	if _, err := (&Server{}).repairCorrectionSet(context.Background(), set, c, nil); err == nil {
		t.Fatal("clarification applied a draft")
	}
}

func TestCorrectionBindingKeepsProgramScopeAndRejectsStaleOrUnknownTargets(t *testing.T) {
	set := taskFactPoolFixture()
	set.ID = 7
	c, facts, e := bindContentCorrection(set, contentCorrectionInput{ExpectedSetID: 7, PoolKey: "product:7", TaskID: "storage-job", SelectedText: "伪造外部内容", Products: []int64{999}, Feedback: "不抽真空是3天，不是6天，其他不变，以后按这个说"})
	if e != nil || c.Original != "按包装解释保存条件" || len(c.Products) != 1 || c.Products[0] != 7 || len(facts) != 1 || !strings.Contains(facts[0], "不抽真空3天") {
		t.Fatal(c, facts, e)
	}
	for _, in := range []contentCorrectionInput{{ExpectedSetID: 6, TaskID: "storage-job", Feedback: "取消"}, {ExpectedSetID: 7, PoolKey: "product:7", TaskID: "other-tenant", Feedback: "取消"}, {ExpectedSetID: 7, SelectedText: "外部文本", Products: []int64{999}, Feedback: "不能这么说"}} {
		if _, _, e := bindContentCorrection(set, in); e == nil {
			t.Fatal("unscoped correction accepted", in)
		}
	}
}
func TestCorrectionRendererRetainsP0AndRuleWithoutModelIDs(t *testing.T) {
	set := taskFactPoolFixture()
	set.Sources.Corrections = []contentpools.Correction{{ID: "private-correction", Products: []int64{7}, Content: contentpools.CorrectionContent{Fact: "额定功率800W", Forbidden: "不能承诺所有场景完全安全"}}}
	set.SourceSHA = set.Sources.SHA()
	tasks, _, e := contentPoolRuntime(set, anchorcycle.ProductPolicy{})
	if e != nil {
		t.Fatal(e)
	}
	s := cycleTestState(t)
	s.Queue = tasks
	s.Cursor = 0
	s.TaskIndex = 0
	s.BaselineJSON = textTestBaseline()
	key := s.AnglePlanKey()
	tasks[0].FactScope.Corrections[0]["表达限制"] = "不建议机洗"
	if s.AnglePlanKey() == key {
		t.Fatal("old direction pool survived a rule change")
	}
	messages, e := renderCycleTextMessages(&s)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(messages)
	if !strings.Contains(string(raw), "用户已确认的长期纠正规则") || !strings.Contains(string(raw), "有效P0") || strings.Contains(string(raw), "private-correction") {
		t.Fatal("rule/P0 missing or internal identity leaked", string(raw))
	}
	set.Pools[0].Tasks[0].Disabled = true
	queue, _, e := contentPoolRuntime(set, anchorcycle.ProductPolicy{})
	if e != nil || len(queue) != 1 || queue[0].ID != "delivery-job" {
		t.Fatal("disabled task selected", queue, e)
	}
	p0, _, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = correctionMessages("test", "", map[string]any{}); e == nil {
		t.Fatal("missing P0 sent")
	}
	if _, e = correctionMessages("test", p0, map[string]any{"单项内容": "校对护理方式"}); e != nil {
		t.Fatal(e)
	}
}
