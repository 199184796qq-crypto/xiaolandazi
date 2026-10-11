package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestLectureSegmentPlanBoundsEvidenceAndRejectsMissing(t *testing.T) {
	facts := "资料对象：钢材\n与商品的已确认关系：杯身用304\n钢材牌号为304\n产地背景为地区甲"
	raw := `{"purpose":"解释材质","angle":"杯身部位与牌号","quoted_facts":["钢材牌号为304"],"avoid":"不推导保温时长","question":""}`
	if _, err := parseLectureSegmentPlan(raw, facts); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, "钢材牌号为304", "食品接触级", 1), strings.Replace(raw, `"purpose":`, `"fact_id":7,"purpose":`, 1), `{"question":"请补充时长","purpose":"","angle":"","quoted_facts":[],"avoid":""}`} {
		if _, err := parseLectureSegmentPlan(bad, facts); err == nil {
			t.Fatal("ungrounded plan accepted", bad)
		}
	}
	task := anchorcycle.Task{ProductMode: "lecture", LectureFacts: facts, LecturePurpose: "目的", Angle: "材质", LecturePlanJSON: raw}
	state := anchorcycle.State{Lectures: &anchorcycle.LectureRuntime{Active: &task}, Config: anchorcycle.Config{MaxChars: 220}}
	if err := installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	messages, err := renderLectureTaskMessages(&state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(messages[1].Content, "钢材牌号为304") || strings.Contains(messages[1].Content, "产地背景为地区甲") {
		t.Fatal("unrelated angle evidence sent to speech model")
	}
	if task.LectureFacts != facts {
		t.Fatal("full review/provenance lost")
	}
}
