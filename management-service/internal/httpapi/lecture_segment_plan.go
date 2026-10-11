package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"strings"
	"unicode/utf8"
)

type lectureSegmentPlan struct {
	Purpose  string   `json:"purpose"`
	Angle    string   `json:"angle"`
	Quotes   []string `json:"quoted_facts"`
	Avoid    string   `json:"avoid"`
	Question *string  `json:"question"`
}

var errLecturePlanning = errors.New("二级资料讲解规划未完成")

func parseLectureSegmentPlan(raw, facts string) (lectureSegmentPlan, error) {
	var p lectureSegmentPlan
	if decodeLectureContent(raw, &p) != nil || p.Question == nil {
		return p, errors.New("二级资料讲解规划未完成，请重试")
	}
	if strings.TrimSpace(*p.Question) != "" {
		return p, errors.New("讲解资料不足：" + *p.Question)
	}
	if p.Purpose == "" || p.Angle == "" || len(p.Quotes) == 0 || len(p.Quotes) > 12 {
		return p, errors.New("二级资料讲解规划未完成，请重试")
	}
	for _, q := range p.Quotes {
		if strings.TrimSpace(q) == "" || utf8.RuneCountInString(q) > 1600 || !strings.Contains(facts, q) {
			return p, errors.New("讲解规划引用不属于当前二级资料，未生成正文")
		}
	}
	// A few extra valid quotations are a noncritical count deviation. Preserve
	// the raw exchange and cap only the selected view, without inventing evidence.
	if len(p.Quotes) > 5 {
		p.Quotes = p.Quotes[:5]
	}
	if utf8.RuneCountInString(p.Purpose) > 600 || utf8.RuneCountInString(p.Angle) > 600 || utf8.RuneCountInString(p.Avoid) > 1500 {
		return p, errors.New("讲解规划超过内容范围")
	}
	return p, nil
}
func (s *Server) ensureLectureSegmentPlan(ctx context.Context, state *anchorcycle.State) error {
	task := state.Task()
	if task.ProductMode != "lecture" {
		return nil
	}
	if state.Lectures == nil || state.Lectures.Active == nil {
		return errors.New("缺少当前讲解任务")
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_SEGMENT_PLAN_PATH", "configs/prompts/anchor-style/lecture-segment-plan.md", "二级事实讲解规划母版")
	if err != nil {
		return err
	}
	constraintJSON, _ := json.Marshal(state.EffectiveCorrections())
	sha := derivedHash(mother + task.LectureFacts + task.LecturePurpose + task.Angle + state.P0PolicyMD + string(constraintJSON))
	if task.LecturePlanSHA == sha && task.LecturePlanJSON != "" {
		return nil
	}
	data := map[string]any{"requested_purpose": task.LecturePurpose, "requested_angle": task.Angle, "facts": task.LectureFacts, "current_rules": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)}
	data["user_confirmed_corrections"] = state.EffectiveCorrections()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	for attempt := 0; attempt < 2; attempt++ {
		packet, _ := json.Marshal(data)
		result, _, callErr := s.completeAnchorStyleWorkbench(ctx, "", "lecture_segment_plan", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(packet)}}, 2400, false)
		if callErr != nil {
			return errors.New("二级资料讲解规划模型未完成")
		}
		plan, parseErr := parseLectureSegmentPlan(result.Text, task.LectureFacts)
		if parseErr == nil {
			normalized, _ := json.Marshal(plan)
			state.Lectures.Active.LecturePlanJSON, state.Lectures.Active.LecturePlanSHA = string(normalized), sha
			return nil
		}
		if attempt == 1 || strings.Contains(parseErr.Error(), "讲解资料不足") {
			return parseErr
		}
		data["previous_format_error"] = map[string]string{"returned_content": result.Text, "error": parseErr.Error()}
	}
	return errLecturePlanning
}
