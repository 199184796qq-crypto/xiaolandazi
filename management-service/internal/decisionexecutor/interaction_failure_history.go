package decisionexecutor

import (
	"context"
	"log"
	"net/url"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type interactionFailure struct {
	DecisionID        string    `json:"decision_id"`
	QuestionText      string    `json:"question_text"`
	Reason            string    `json:"reason"`
	PreparationReason string    `json:"preparation_reason"`
	At                time.Time `json:"at"`
}

type interactionFailureCursor struct {
	sessionID  int64
	decisionID string
}

func (w *Worker) addInteractionFailureCursor(query url.Values, session model.LiveRuntimeSession) {
	if session.ID <= 0 || session.StartedAt.IsZero() {
		return
	}
	query.Set("failure_session_started_at", session.StartedAt.UTC().Format(time.RFC3339Nano))
	w.mu.Lock()
	cursor := w.failureHistory[session.RoomID]
	w.mu.Unlock()
	if cursor.sessionID == session.ID {
		query.Set("failure_after", cursor.decisionID)
	}
}

func (w *Worker) recordInteractionFailures(ctx context.Context, session model.LiveRuntimeSession, failures []interactionFailure) {
	if session.ID <= 0 || session.StartedAt.IsZero() || len(failures) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for _, failure := range failures {
		if failure.DecisionID == "" || failure.At.Before(session.StartedAt) {
			continue
		}
		err := w.store.RecordGeneratedSpeechHistory(ctx, model.GeneratedSpeechHistoryInput{
			TenantID: session.TenantID, RoomID: session.RoomID, RuntimeSessionID: session.ID,
			RuntimeExternalID: session.ExternalID, DecisionID: failure.DecisionID,
			SourceType: "interrupt_failed", QuestionText: failure.QuestionText,
			GeneratedText: "未完成播报 · " + interactionFailureReason(failure),
		})
		if err != nil {
			log.Printf("interaction failure history tenant=%d room=%d decision=%s: %v", session.TenantID, session.RoomID, failure.DecisionID, err)
			return
		}
		w.mu.Lock()
		if w.failureHistory == nil {
			w.failureHistory = make(map[int64]interactionFailureCursor)
		}
		w.failureHistory[session.RoomID] = interactionFailureCursor{sessionID: session.ID, decisionID: failure.DecisionID}
		w.mu.Unlock()
	}
}

func interactionFailureReason(failure interactionFailure) string {
	switch failure.Reason {
	case "deferred_answer_deadline":
		switch strings.TrimSpace(strings.TrimPrefix(failure.PreparationReason, "interaction-scheduler:")) {
		case "audio_duration_budget":
			return "延期窗口内插播时长额度不足"
		case "no_pcm_cut", "acoustic_scan_pending":
			return "延期窗口内没有可用的安全切点"
		case "speech_busy":
			return "延期窗口内上一条互动仍在播放"
		case "mainline_low_buffer", "mainline_unavailable":
			return "延期窗口内主线播放条件未满足"
		case "playback_reschedule":
			return "延期窗口内已合成的语音未等到可插播时机"
		default:
			return "超过互动延期时限"
		}
	case "deferred_answer_unavailable":
		return "延期前生成的答稿或语音已失效"
	case "welcome_tts_time_insufficient", "welcome_names_deadline_missing":
		return "欢迎名单的有效时间不足，无法及时合成语音"
	case "interaction_v2_expired":
		return "互动内容已过期或直播状态已变化"
	case "waiting_deadline", "execution_deadline", "claim_timeout_deadline":
		return "超过互动任务的执行时限"
	case "retry_exhausted":
		return "互动执行失败，已达到重试上限"
	case "source_start_timeout":
		return "语音未能及时开始播放"
	default:
		return "语音播放或互动执行失败"
	}
}
