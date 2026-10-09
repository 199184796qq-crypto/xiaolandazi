package decisionexecutor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type interactionReviewStore interface {
	RecordInteractionLearningReview(context.Context, model.InteractionLearningObservation) error
}

func (w *Worker) shouldAssessInteraction(session model.LiveRuntimeSession, item decisionItem) bool {
	if _, ok := w.store.(interactionReviewStore); !ok {
		return false
	}
	if session.ID <= 0 || session.ExternalID == "" || item.ManualOrigin != "" || item.PreviewInstruction != "" || strings.EqualFold(item.ExecutionMode, "verbatim") {
		return false
	}
	return len(item.SampleQuestions) > 0 && (item.MissionKind == "reply_question" || item.MissionKind == "reply_chat" || item.Scene == "product_question" || item.Scene == "complaint")
}

func apparentUncertainty(text string) bool {
	for _, p := range []string{"没拿到准话", "没有准确数", "暂时无法确认", "需要确认一下", "具体规则还不清楚", "不清楚您是问", "我得先确认", "我得确认", "还需要核实"} {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

func (w *Worker) recordInteractionReview(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, text string, review *model.InteractionLearningReview) {
	sink, ok := w.store.(interactionReviewStore)
	if !ok || session.ID <= 0 || session.ExternalID == "" || item.ManualOrigin != "" || item.PreviewInstruction != "" {
		return
	}
	if review == nil && apparentUncertainty(text) {
		review = &model.InteractionLearningReview{Kind: "missing_fact", Meaning: primaryQuestion(item), Context: strings.TrimSpace(item.OriginMainline + "\n" + item.CurrentMainline), Reason: "实际回答仍表示信息需要确认，请补充相关事实或纠正问题含义", NeededFacts: []string{primaryQuestion(item)}}
	}
	if review == nil {
		return
	}
	copy := *review
	copy.ObservedContext = strings.TrimSpace("事件发生时正在讲：" + item.OriginMainline + "\n生成前正在讲：" + item.CurrentMainline)
	if copy.Context == "" {
		copy.Context = "当前直播商品与提问场景：" + primaryQuestion(item)
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		log.Printf("interaction review plan lookup tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
		return
	}
	copy.PlanID = plan.ID
	actor := int64(0)
	if session.StartedByUserID != nil {
		actor = *session.StartedByUserID
	}
	saveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := sink.RecordInteractionLearningReview(saveCtx, model.InteractionLearningObservation{TenantID: session.TenantID, RoomID: session.RoomID, ActorUserID: actor, Question: strings.Join(item.SampleQuestions, "\n"), Reply: text, SourceType: "live_uncertain", SourceRef: fmt.Sprintf("runtime:%s:%s", session.ExternalID, item.ID), Review: copy}); err != nil {
		log.Printf("interaction review save tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
	}
}
