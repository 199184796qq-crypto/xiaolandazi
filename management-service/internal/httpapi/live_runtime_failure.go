package httpapi

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/errordetail"
	"livecompanion/management/internal/model"
)

func publicLiveFailure(session int64, err error) anchorcycle.LiveFailure {
	detail := errordetail.Describe(err, "主线运行")
	failure := anchorcycle.LiveFailure{SessionID: session, Code: detail.Code, Stage: detail.Stage, Message: detail.Message, Action: detail.Action, RequestID: detail.RequestID, HTTPStatus: detail.HTTPStatus, OccurredAt: time.Now().UTC()}
	if err == nil {
		return failure
	}
	// Provider errors can contain credentials or signed asset URLs. Return only
	// known business explanations; preserve complete diagnostics in server logs.
	text := err.Error()
	if errors.Is(err, anchorcycle.ErrStale) || strings.Contains(text, anchorcycle.ErrStale.Error()) {
		failure.Code = "runtime_state_changed"
		failure.Stage = "场次状态校验"
		failure.Action = "确认当前场次状态；晚到结果不会应用到新场次。"
		return failure
	}
	for _, marker := range []string{"事实或轮盘已变化", "事实、福利有效期或轮盘已变化", "商品范围与已保存安全事实不一致"} {
		if strings.Contains(text, marker) {
			failure.Code = "plan_sources_changed"
			failure.Stage = "方案事实校验"
			failure.Action = "重新保存方案后再开始。"
			return failure
		}
	}
	if strings.Contains(text, "本场不能继续生成") {
		failure.Code = "generation_blocked"
		failure.Stage = "话术生成校验"
		failure.Action = "根据上述具体原因检查事实依据和本段生成记录。"
	}
	return failure
}

func (s *Server) populateLiveRuntimeFailure(ctx context.Context, tenant, room int64, snapshot *model.LiveRuntimeSnapshot) {
	if snapshot.Session == nil {
		return
	}
	if suppressLiveFailure(snapshot.Session) {
		return
	}
	if repo, ok := s.anchorCycles.(anchorcycle.LiveFailureRepository); ok {
		failure, err := repo.GetLiveFailure(ctx, tenant, room, snapshot.Session.ID)
		if err != nil {
			log.Printf("read live failure tenant=%d room=%d: %v", tenant, room, err)
		}
		if failure != nil {
			applyLiveRuntimeFailure(snapshot, failure)
			return
		}
	}
	if snapshot.AgentStopReason == "system_error" && snapshot.AgentState != "working" {
		failure := publicLiveFailure(snapshot.Session.ID, nil)
		applyLiveRuntimeFailure(snapshot, &failure)
	}
}

func applyLiveRuntimeFailure(snapshot *model.LiveRuntimeSnapshot, failure *anchorcycle.LiveFailure) {
	if snapshot.Session == nil || failure == nil || failure.SessionID != snapshot.Session.ID {
		return
	}
	if suppressLiveFailure(snapshot.Session) {
		return
	}
	snapshot.Failure = &model.LiveRuntimeFailure{SessionID: failure.SessionID, Code: failure.Code, Message: failure.Message, Stage: failure.Stage, Action: failure.Action, RequestID: failure.RequestID, HTTPStatus: failure.HTTPStatus, OccurredAt: failure.OccurredAt}
	snapshot.AgentState = "stopped"
}

func suppressLiveFailure(session *model.LiveRuntimeSession) bool {
	// A saved diagnostic is not a lifecycle transition. In particular, a
	// recovering or manually paused session must never be presented as stopped
	// merely because an earlier request left a failure record behind.
	if session == nil || session.Status != "stopped" {
		return true
	}
	switch session.StopReason {
	case "manual", "manual_stop", "manual_restart", "manual_pause", "live_finished", "device_offline", "device_shutdown", "quota_exhausted", "core_restart", "core_runtime_reset":
		return true
	default:
		return false
	}
}
