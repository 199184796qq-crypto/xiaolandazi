package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/ttsgateway"
)

// interactionDigest 是对任意可 JSON 序列化值求 sha256，用于交互指纹（缓存键、准备键）。
func interactionDigest(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// synthesizeInteractionAudio 提交一次互动 TTS 合成。
//
// 旧链路的「问答缓存（faq_cache）」已随「意图分析→规划→回答」一起下线，
// 正文加保护前缀后进入TTS；跨事件声音缓存按完整实际发送文字匹配，
// 并把实际递交文本打到日志，方便排查 cosyvoice 对前缀/开头的处理。
func (w *Worker) synthesizeInteractionAudio(ctx context.Context, item *decisionItem, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	return w.synthesizeInteractionAudioStage(ctx, item, "interaction_tts", request)
}

func (w *Worker) synthesizeInteractionAudioStage(ctx context.Context, item *decisionItem, stage string, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	request.Text = prefixedInteractionText(request.Text)
	deadline, err := w.welcomeTTSPurchaseDeadline(*item, request.Text)
	if err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	if runes := []rune(request.Text); len(runes) > 0 {
		n := min(80, len(runes))
		log.Printf("interaction tts text decision=%s mission=%s text=%q", item.ID, item.MissionKind, string(runes[:n]))
	}
	scope, _ := ctx.Value(workerPaidScopeKey{}).(workerPaidScope)
	started := time.Now()
	response, err := w.synthesizePaidAudio(ctx, scope.session, stage, item.ID, request)
	w.observeTTPreparation(time.Since(started))
	if err != nil {
		return response, err
	}
	if strings.TrimSpace(response.AudioURL) == "" {
		return response, nil
	}
	return response, nil
}
