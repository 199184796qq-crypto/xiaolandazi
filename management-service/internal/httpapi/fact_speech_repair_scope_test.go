package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestSecondaryRepairUsesOnlyCurrentSegmentNotWholeStory(t *testing.T) {
	state := cycleTestState(t)
	state.BaselineJSON = textTestBaseline()
	task := anchorcycle.Task{ProductMode: "lecture", LecturePurpose: "完整故事大目的", Angle: "全部故事角度", LectureFacts: "资料对象：原料A\n与商品的已确认关系：商品使用原料A\n丘陵水田散养\n肉质口感紧实\n汤肉鲜甜", LecturePlanJSON: `{"purpose":"说明产地和散养环境","angle":"只讲生活环境","quoted_facts":["丘陵水田散养"],"avoid":"不展开口感或吃法","question":""}`}
	state.Lectures = &anchorcycle.LectureRuntime{Active: &task}
	messages, e := factSpeechRepairMessages(&state, map[string]any{"text": "炖汤好喝", "issues": []map[string]string{{"reason": "新增成品用法"}}})
	if e != nil {
		t.Fatal(e)
	}
	var packet map[string]any
	if e = json.Unmarshal([]byte(messages[len(messages)-1].Content), &packet); e != nil {
		t.Fatal(e)
	}
	facts, _ := packet["confirmed_facts"].(string)
	if packet["requested_purpose"] != "说明产地和散养环境" || packet["requested_angle"] != "只讲生活环境" || !strings.Contains(facts, "商品使用原料A") || !strings.Contains(facts, "丘陵水田散养") || strings.Contains(facts, "汤肉鲜甜") || strings.Contains(facts, "肉质口感") {
		t.Fatal("repair expanded back to full story", packet)
	}
}
