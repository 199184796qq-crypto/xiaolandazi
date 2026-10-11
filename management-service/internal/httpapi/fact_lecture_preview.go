package httpapi

import (
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
)

// Associations come from the tenant/plan-scoped library, never names or keywords.
// A root is a level-one relationship; previewing its attached object explicitly
// selects a lecture, loading only that object's level-two evidence.
func selectedFactLecture(lib lecturetask.Library, f model.LiveAgentPlanFact) (*lecturetask.Task, error) {
	var chosen *lecturetask.Task
	associated := false
	for _, t := range lib.Tasks {
		if t.RootFact == nil || t.RootFact.ID != f.ID {
			continue
		}
		associated = true
		if !t.Valid || t.RootFact.Version != f.VersionNo || t.RootFact.Text != f.Value {
			continue
		}
		if t.Subject == "" || len(t.Facts) == 0 || len(t.Angles) == 0 {
			return nil, errors.New("二级对象资料或讲解角度不足，请先补充")
		}
		if chosen != nil {
			return nil, errors.New("该事实关联多个讲解任务，请到任务讲解板块选择具体任务预览")
		}
		copy := t
		chosen = &copy
	}
	if chosen == nil && associated {
		return nil, errors.New("该对象的一级关联或二级资料已变化，请在二级资料面板核对后保存")
	}
	if chosen == nil && (f.SourceType == "story_subject" || f.SourceType == "story_lecture") {
		return nil, errors.New("该二级资料尚未关联有效讲解任务，请先在任务讲解板块整理关联")
	}
	return chosen, nil
}

func factLecturePreviewTask(t lecturetask.Task, scope *anchorcycle.TaskFactScope) anchorcycle.Task {
	a := t.Angles[0]
	return anchorcycle.Task{SellingPoint: t.Title, Angle: a.Title + "：" + a.Goal, ProductMode: "lecture", LecturePurpose: t.Purpose, LectureFacts: t.Evidence(), LectureTopic: t.ID, FactScope: scope}
}
