package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestLectureReviewDoesNotInventPassOrQuotes(t *testing.T) {
	for _, raw := range []string{`{}`, `{"passed":true}`, `{"passed":true,"issues":[{"quote":"原料","rule":"规则","reason":"理由"}]}`, `{"passed":false,"issues":[]}`, `{"passed":false,"issues":[{"quote":"不在原文","rule":"规则","reason":"理由"}]}`, `{"passed":true,"issues":[],"id":"model-id"}`} {
		if _, err := parseLectureReview(raw, "原料是土麻鸭"); err == nil {
			t.Fatal(raw)
		}
	}
	if _, err := parseLectureReview(`{"passed":true,"issues":[]}`, "原料是土麻鸭"); err != nil {
		t.Fatal(err)
	}
}
func TestLectureIsNotOrdinaryFactPromptOrUnrelatedAnswerData(t *testing.T) {
	facts := []model.LiveAgentPlanFact{{Status: "active", Key: "快递", Value: "京东", Category: "other"}, {Status: "active", Key: "故事", Value: "丘陵水田与地方故事", SourceType: "story_lecture", Category: "other"}}
	text := renderAnchorStyleWorkbenchFacts(model.LiveAgentPlan{Name: "当前方案"}, facts, nil, nil)
	if strings.Contains(text, "丘陵") || !strings.Contains(text, "京东") {
		t.Fatal(text)
	}
	lib := lecturetask.Library{Tasks: []lecturetask.Task{{Valid: true, QuestionTerms: []string{"产地", "哪里产的"}, Facts: []lecturetask.Fact{{Name: "产地", Text: "四川南充"}}}}}
	if lecturetask.QuestionEvidence(lib, "发什么快递") != "" || !strings.Contains(lecturetask.QuestionEvidence(lib, "哪里的产地"), "四川南充") {
		t.Fatal("unrelated story retrieval")
	}
}
func TestLecturePacketContainsSelectedTaskWithoutLibraryOrIDs(t *testing.T) {
	s := anchorcycle.State{Config: anchorcycle.Config{MaxChars: 220}, Facts: "无关业务全量事实", Lectures: &anchorcycle.LectureRuntime{Active: &anchorcycle.Task{LectureTopic: 998, ProductMode: "lecture", SellingPoint: "选中主题", Angle: "选中角度", LecturePurpose: "目的", LectureFacts: "所需事实"}}}
	if err := installP0Mother(&s); err != nil {
		t.Fatal(err)
	}
	messages, err := renderLectureTaskMessages(&s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(messages)
	for _, bad := range []string{"无关业务全量事实", "998", "lecture_topic", "task_id", "library"} {
		if strings.Contains(string(raw), bad) {
			t.Fatal("context polluted", bad)
		}
	}
	if !strings.Contains(string(raw), "所需事实") {
		t.Fatal(string(raw))
	}
}

func TestSuppressedLectureDoesNotProduceAnAudibleReturnReference(t *testing.T) {
	s := cycleTestState(t)
	s.BaselineJSON = textTestBaseline()
	s.Accepted = []anchorcycle.Segment{{Task: anchorcycle.Task{ProductMode: "lecture", SellingPoint: "未播出的故事"}, Selected: anchorcycle.Candidate{Text: "未播出的故事正文"}, LectureSkipped: true}}
	messages, err := renderCycleTextMessages(&s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(messages[1].Content, "未播出的故事") || strings.Contains(messages[1].Content, "刚结束的讲解") {
		t.Fatal("referred to suppressed story", messages)
	}
}
