package httpapi

import (
	"errors"
	"log"
	"net/http"
	"runtime/debug"

	"livecompanion/core/internal/coreaudio"
)

type audioDeliveryError struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	Stage     string `json:"stage"`
	Retryable bool   `json:"retryable"`
}

func writeAudioAppendFailure(w http.ResponseWriter, err error) {
	detail := audioDeliveryError{Error: "本段声音未通过播放服务校验，可继续使用其它已就绪内容。", Code: "audio_unit_not_accepted", Stage: "声音单元校验", Retryable: false}
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, coreaudio.ErrProgramQueueFull):
		status = http.StatusServiceUnavailable
		detail = audioDeliveryError{Error: "播放队列暂时已满，保留本段声音等待追加。", Code: "audio_queue_full", Stage: "播放队列", Retryable: true}
	case errors.Is(err, coreaudio.ErrProgramPositionInvalid):
		status = http.StatusServiceUnavailable
		detail = audioDeliveryError{Error: "播放返回位置暂时无法确认，正在核对原音轨。", Code: "resume_position_invalid", Stage: "播放位置恢复", Retryable: true}
	case errors.Is(err, coreaudio.ErrProgramRefreshConflict):
		status = http.StatusConflict
		detail = audioDeliveryError{Error: "播放节目状态已变化，正在核对本场音频。", Code: "audio_program_state_changed", Stage: "播放状态校验", Retryable: true}
	}
	writeJSON(w, status, detail)
}

// Keep an unexpected request-local panic from becoming an ambiguous EOF. The
// caller retains its unit ID and reconciles/retries; this boundary never ends
// the live session, manufactures success, or returns a stack/secret to a client.
func recoverRoomAudioDelivery(w http.ResponseWriter) {
	if recovered := recover(); recovered != nil {
		log.Printf("audio delivery request recovered panic_type=%T\n%s", recovered, debug.Stack())
		writeJSON(w, http.StatusInternalServerError, audioDeliveryError{
			Error: "播放服务处理音频时发生内部异常，正在自动恢复。", Code: "audio_delivery_internal_error", Stage: "播放服务", Retryable: true,
		})
	}
}
