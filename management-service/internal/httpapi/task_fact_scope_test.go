package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
)

func taskFactPoolFixture() contentpools.Set {
	source := contentpools.Sources{Products: []contentpools.Product{{ID: 7, Link: "2", Name: "半只装"}}, Rows: []contentpools.SourceRow{{ID: "original-storage"}, {ID: "original-delivery"}}}
	return contentpools.Set{Sources: source, SourceSHA: source.SHA(), SafeFacts: "完整原始资料", Pools: []contentpools.Pool{{Key: "product:7", Owner: 7, Products: []int64{7}, Facts: []contentpools.Fact{
		{ID: "storage", Text: "未开封常温：抽真空6天，不抽真空3天", Sources: []string{"original-storage"}, Products: []int64{7}},
		{ID: "delivery", Text: "使用京东物流；下午6点前一般当天发出，之后次日上午发出", Sources: []string{"original-delivery"}, Products: []int64{7}},
	}, Tasks: []contentpools.Task{
		{ID: "storage-job", Point: "保存", Angle: "按包装解释保存条件", Sources: []string{"storage"}},
		{ID: "delivery-job", Point: "物流", Angle: "回答使用什么物流", Sources: []string{"delivery"}},
	}}}}
}

func TestContentPoolRuntimeRetainsPerTaskFactsForSingleProduct(t *testing.T) {
	set := taskFactPoolFixture()
	tasks, focus, err := contentPoolRuntime(set, anchorcycle.ProductPolicy{})
	if err != nil || focus != nil || len(tasks) != 2 {
		t.Fatal(tasks, focus, err)
	}
	// Binding remains useful without a multiple-product scheduler.
	s := cycleTestState(t)
	s.Queue, s.Cursor, s.TaskIndex = tasks, 0, 0
	s.Facts = "全量事实含物流和售后"
	if facts := s.GenerationFacts(); strings.Contains(facts, "物流") || !strings.Contains(facts, "不抽真空3天") {
		t.Fatal(facts)
	}
	if !strings.Contains(s.GenerationFacts(), "商品名称=半只装") {
		t.Fatal("minimal item identity absent")
	}
	before := s.AnglePlanKey()
	s.Facts = "修改不相关物流资料"
	if s.AnglePlanKey() != before {
		t.Fatal("unrelated full facts invalidated the task direction pool")
	}
	s.Cursor, s.TaskIndex = 1, 1
	if facts := s.GenerationFacts(); !strings.Contains(facts, "京东物流") || strings.Contains(facts, "抽真空6天") {
		t.Fatal(facts)
	}
	// The complete saved facts and their original references remain intact.
	if len(set.Pools[0].Facts) != 2 || set.Pools[0].Tasks[0].Sources[0] != "storage" {
		t.Fatal("saved source modified")
	}
}

func TestDirectWriterSendsOnlyBoundTaskFactsWithoutRelationKeys(t *testing.T) {
	tasks, _, err := contentPoolRuntime(taskFactPoolFixture(), anchorcycle.ProductPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	s := cycleTestState(t)
	s.Queue, s.Cursor, s.TaskIndex = tasks, 0, 0
	s.BaselineJSON = textTestBaseline()
	s.TextModel = &agentgateway.SpeechModel{ID: "flash", Model: "qwen3.7-flash"}
	s.Facts = "全量资料：京东物流，支持退货"
	s.Accepted = []anchorcycle.Segment{{Task: tasks[1], Selected: anchorcycle.Candidate{AngleHook: &anchorcycle.AngleHook{Action: "旧物流方向不能混入保存任务"}, Text: "旧物流正文"}}}
	for i := 0; i < 2; i++ {
		s.Cursor, s.TaskIndex = i, i
		messages, err := renderCycleTextMessages(&s)
		if err != nil {
			t.Fatal(err)
		}
		// Parse the material block; P0 may legitimately mention logistics in rules.
		at := strings.Index(messages[0].Content, `{"主播表达标尺"`)
		if at < 0 {
			t.Fatal("material block absent")
		}
		var material map[string]any
		if err := json.Unmarshal([]byte(messages[0].Content[at:]), &material); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(material["本场有效事实"])
		if (i == 0 && (strings.Contains(string(raw), "物流") || !strings.Contains(string(raw), "不抽真空3天"))) || (i == 1 && (!strings.Contains(string(raw), "京东物流") || strings.Contains(string(raw), "抽真空6天"))) {
			t.Fatal(string(raw))
		}
		for _, key := range []string{"fact_scope", "fact_ids", "storage-job", "delivery-job", "original-storage"} {
			if strings.Contains(messages[1].Content, key) {
				t.Fatal("internal relation leaked", key, messages[1].Content)
			}
		}
		if i == 0 && strings.Contains(messages[1].Content, "旧物流") {
			t.Fatal("unrelated task memory leaked", messages[1].Content)
		}
	}
}
