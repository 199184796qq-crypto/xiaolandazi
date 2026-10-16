package httpapi

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func publicLiveFailure(session int64, err error) anchorcycle.LiveFailure {
	failure := anchorcycle.LiveFailure{SessionID: session, Code: "formal_mainline_failed", Message: "直播程序运行异常，本场已停止。请联系后台并提供场次编号，以便核对具体原因。", OccurredAt: time.Now().UTC()}
	if err == nil {
		return failure
	}
	// Provider errors can contain credentials or signed asset URLs. Return only
	// known business explanations; preserve complete diagnostics in server logs.
	text := err.Error()
	if errors.Is(err, anchorcycle.ErrStale) || strings.Contains(text, anchorcycle.ErrStale.Error()) {
		failure.Code = "runtime_state_changed"
		failure.Message = "本场运行状态已变化，晚到结果未采纳。请确认场次状态后重新开始。"
		return failure
	}
	for _, marker := range []string{"事实或轮盘已变化", "事实、福利有效期或轮盘已变化", "商品范围与已保存安全事实不一致"} {
		if strings.Contains(text, marker) {
			failure.Code = "plan_sources_changed"
			failure.Message = "事实、福利或卖点轮盘已变化，当前保存方案不再适用。本场已停止，请重新保存方案后开始。"
			return failure
		}
	}
	if strings.Contains(text, "本场不能继续生成") {
		failure.Code = "generation_blocked"
		failure.Message = "本场口播未通过生成校验，已停止生成声音。请检查事实依据及卖点内容后重新开始；如仍失败，请联系后台。"
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
	snapshot.Failure = &model.LiveRuntimeFailure{SessionID: failure.SessionID, Code: failure.Code, Message: failure.Message, OccurredAt: failure.OccurredAt}
	snapshot.AgentState = "stopped"
}

func suppressLiveFailure(session *model.LiveRuntimeSession) bool {
	if session == nil || session.Status != "stopped" {
		return false
	}
	switch session.StopReason {
	case "manual", "manual_stop", "manual_restart", "manual_pause", "live_finished", "device_offline", "device_shutdown", "quota_exhausted", "core_restart", "core_runtime_reset":
		return true
	default:
		return false
	}
}
