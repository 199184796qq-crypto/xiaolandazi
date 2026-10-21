package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestFactLecturePreviewAcrossProductsAndRooms(t *testing.T) {
	cases := []struct{ product, subject, detail string }{
		{"熟食", "土麻鸭", "土麻鸭产自四川南充"},
		{"饼干", "燕麦", "燕麦是谷物原料"},
		{"保温杯", "杯身钢材", "杯身使用304不锈钢"},
		{"茶叶", "产地背景", "产区位于丘陵地带"},
	}
	for i, tc := range cases {
		for _, room := range []int64{101, 202} {
			t.Run(tc.product+"/"+string(rune('A'+room%2)), func(t *testing.T) {
				f := model.LiveAgentPlanFact{ID: 987654, TenantID: 14, PlanID: int64(i + 1), VersionNo: 3, Value: tc.product + "关联" + tc.subject, SourceType: "story_subject"}
				lecture := lecturetask.Task{ID: 654321, Revision: 2, Subject: tc.subject, RootFact: &lecturetask.Fact{ID: f.ID, Version: f.VersionNo, Text: f.Value}, Title: tc.subject + "介绍", Purpose: "解释当前商品关联对象的来源", Angles: []lecturetask.Angle{{Title: "来源", Goal: "讲清资料对象"}}, Facts: []lecturetask.Fact{{Storage: "lecture", ID: 1, Name: "背景", Text: tc.detail}}, Valid: true}
				other := lecture
				other.ID++
				other.RootFact = &lecturetask.Fact{ID: 5, Version: 3, Text: "其它商品"}
				other.Facts = []lecturetask.Fact{{Name: "其它来源", Text: "不得串用的对象资料"}}
				selected, err := selectedFactLecture(lecturetask.Library{Tasks: []lecturetask.Task{other, lecture}}, f)
				if err != nil || selected == nil {
					t.Fatal(err)
				}
				task := factLecturePreviewTask(*selected, factPreviewTask(f, nil).FactScope)
				state, err := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{MaxChars: 220})
				if err != nil {
					t.Fatal(err)
				}
				state.RoomID, state.TenantID, state.PlanID, state.Surface = room, f.TenantID, f.PlanID, factPreviewSurface
				state.Lectures = &anchorcycle.LectureRuntime{Active: &task}
				state.BaselineJSON = textTestBaseline()
				if err = installP0Mother(&state); err != nil {
					t.Fatal(err)
				}
				messages, err := renderCycleTextMessages(&state)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(messages)
				for _, want := range []string{tc.detail, tc.subject, lecture.Purpose, "current_rules"} {
					if !strings.Contains(string(raw), want) {
						t.Fatal("missing lecture content", want)
					}
				}
				for _, bad := range []string{"不得串用的对象资料", "987654", "654321", "root_fact", "fact_id", "tenant_id", "plan_id"} {
					if strings.Contains(string(raw), bad) {
						t.Fatal("scope leak", bad)
					}
				}
			})
		}
	}
}

func TestFactLecturePreviewRejectsUnboundStaleAndAmbiguous(t *testing.T) {
	f := model.LiveAgentPlanFact{ID: 9, VersionNo: 2, Value: "关联材料", SourceType: "story_subject"}
	root := &lecturetask.Fact{ID: 9, Version: 2, Text: f.Value}
	valid := lecturetask.Task{Subject: "材料", RootFact: root, Facts: []lecturetask.Fact{{Text: "材料细节"}}, Angles: []lecturetask.Angle{{Title: "来源"}}, Valid: true}
	for _, lib := range []lecturetask.Library{{}, {Tasks: []lecturetask.Task{{RootFact: root, Valid: false}}}, {Tasks: []lecturetask.Task{valid, valid}}} {
		if _, err := selectedFactLecture(lib, f); err == nil {
			t.Fatal("unsafe fallback to isolated mainline fact")
		}
	}
	f.SourceType = "manual_edit"
	if selected, err := selectedFactLecture(lecturetask.Library{}, f); err != nil || selected != nil {
		t.Fatal("ordinary fact regressed")
	}
	f.VersionNo = 3
	if _, err := selectedFactLecture(lecturetask.Library{Tasks: []lecturetask.Task{valid}}, f); err == nil {
		t.Fatal("stale root accepted")
	}
	f.VersionNo = 2
	invalid := valid
	invalid.Valid = false
	if selected, err := selectedFactLecture(lecturetask.Library{Tasks: []lecturetask.Task{invalid, valid}}, f); err != nil || selected == nil {
		t.Fatal("old invalid task shadows current valid object", err)
	}
}
