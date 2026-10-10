package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"livecompanion/management/internal/contentpools"
	"net/http"
	"strings"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
)

// A request-local selected plan: testing never changes the room binding or runtime.
// Embed the complete store so the production worker retains its optional readers.
type planSpeechTestStore struct {
	*appdb.Store
	tenantID, roomID int64
	plan             model.LiveAgentPlan
}

func (s *planSpeechTestStore) GetLiveAgentPlanForRoom(_ context.Context, tenantID, roomID int64) (model.LiveAgentPlan, error) {
	if tenantID != s.tenantID || roomID != s.roomID || s.plan.TenantID != tenantID || s.plan.ID <= 0 {
		return model.LiveAgentPlan{}, fmt.Errorf("方案测试范围不一致")
	}
	return s.plan, nil
}

// Plan-editor tests read the selected plan's current assets. A running room's
// frozen published version must not replace those assets or reject another plan.
// This request-local context has no voice or runtime snapshot and is never saved.
func (s *planSpeechTestStore) GetPublishedLiveAgentPlanVersionForRoom(ctx context.Context, tenantID, roomID int64) (model.LiveAgentPlanVersion, error) {
	plan, err := s.GetLiveAgentPlanForRoom(ctx, tenantID, roomID)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	return model.LiveAgentPlanVersion{PlanID: plan.ID}, nil
}

// Supply the complete current editor facts, including product cards and valid
// benefits. Never fall back to a different room's published scheme or prepare
// new assets as a side effect of a test.
func (s *planSpeechTestStore) CurrentInteractionFacts(ctx context.Context, tenantID, roomID, planID int64) (string, error) {
	plan, err := s.GetLiveAgentPlanForRoom(ctx, tenantID, roomID)
	if err != nil || plan.ID != planID {
		return "", fmt.Errorf("方案测试事实范围不一致")
	}
	facts, err := s.ListLiveAgentPlanFacts(ctx, tenantID, planID)
	if err != nil {
		return "", err
	}
	benefits, err := s.ListLiveAgentPlanBenefits(ctx, tenantID, planID)
	if err != nil {
		return "", err
	}
	links, err := s.ListLiveAgentPlanProductLinks(ctx, tenantID, planID)
	if err != nil {
		return "", err
	}
	scopes, err := s.ListBenefitScopes(ctx, tenantID, planID)
	if err != nil {
		return "", err
	}
	benefits = applyContentBenefitScopes(benefits, links, scopes)
	source, err := (&Server{store: s.Store}).captureContentSources(ctx, tenantID, planID)
	if err != nil {
		return "", err
	}
	set, err := s.Store.LatestContentPoolSet(ctx, tenantID, planID)
	if err != nil {
		return "", err
	}
	if set.ID > 0 && set.SourceSHA == source.SHA() {
		return set.SafeFacts, nil
	}
	return renderAnchorStyleWorkbenchFacts(plan, facts, benefits, links) + contentpools.RenderCorrections(source), nil
}

type planAnswerAdoptionInput struct {
	PlanID   int64  `json:"plan_id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func (s *planSpeechTestStore) CurrentInteractionLectureFacts(ctx context.Context, tenant, room, plan int64, question string) (string, error) {
	if _, err := s.GetLiveAgentPlanForRoom(ctx, tenant, room); err != nil || plan != s.plan.ID {
		return "", fmt.Errorf("方案测试资料范围不一致")
	}
	lib, err := s.LectureQuestionLibrary(ctx, tenant, plan)
	if err != nil {
		return "", err
	}
	return lecturetask.QuestionEvidence(lib, question), nil
}

func validatePlanAnswerAdoption(input planAnswerAdoptionInput) error {
	if input.PlanID <= 0 || strings.TrimSpace(input.Question) == "" || strings.TrimSpace(input.Answer) == "" {
		return fmt.Errorf("请选择方案并提供测试问题和已确认回答")
	}
	if utf8.RuneCountInString(input.Question) > 1000 || utf8.RuneCountInString(input.Answer) > 4000 {
		return fmt.Errorf("问题或回答过长")
	}
	return nil
}

// The button is explicit human confirmation. Preserve the answer verbatim,
// instead of calling another model to decide what the user meant to adopt.
func (s *Server) adoptPlanTestAnswer(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	var input planAnswerAdoptionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, 400, "采纳回答格式错误")
		return
	}
	if err := validatePlanAnswerAdoption(input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, input.PlanID)
	if err != nil || plan.Status != "active" {
		writeError(w, 404, "当前方案不存在或不可编辑")
		return
	}
	question, answer := strings.TrimSpace(input.Question), strings.TrimSpace(input.Answer)
	target := []rune(question)
	if len(target) > 255 {
		target = target[:255]
	}
	item, err := s.store.CreateAgentLearningSession(r.Context(), actor.UserID, tenantID, roomID, model.CreateAgentLearningSessionInput{
		PlanID: plan.ID, SourceType: "plan_answer_confirmation", SourceRef: "manual:plan-speech-test", Question: question, OriginalReply: answer, Target: string(target),
	})
	if err != nil {
		writeError(w, 500, "创建方案测试采纳记录失败")
		return
	}
	_, err = s.store.AppendAgentLearningTurn(r.Context(), item, "用户明确确认这次测试回答，采纳到互动策略", model.AgentLearningResult{
		MemoryType: "semantic", Target: string(target), MemoryKey: fmt.Sprintf("plan:%d:test-answer:%x", plan.ID, sha256.Sum256([]byte(question))),
		ResultText: answer, Structured: map[string]any{"review_plan_id": plan.ID, "semantic_mode": "response_strategy", "question": question, "confirmed_answer": answer, "source": "human_confirmed"},
	})
	if err != nil {
		writeError(w, 500, "保存方案测试采纳内容失败")
		return
	}
	adopted, err := s.store.AdoptAgentLearningSession(r.Context(), tenantID, roomID, item.ID, actor.UserID)
	if err != nil {
		writeError(w, 500, "采纳到互动策略失败")
		return
	}
	if _, err := s.hotReloadCoreAgentRoom(r.Context(), tenantID, roomID, "interaction"); err != nil {
		writeError(w, 502, "互动策略已保存，但同步失败："+err.Error())
		return
	}
	writeJSON(w, 200, adopted)
}
