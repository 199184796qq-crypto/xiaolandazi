package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"

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
// 这里只保留合成本身：正文加保护前缀（TTS 前缀只属于请求，不进入缓存键），
// 并把实际递交文本打到日志，方便排查 cosyvoice 对前缀/开头的处理。
func (w *Worker) synthesizeInteractionAudio(ctx context.Context, item *decisionItem, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	request.Text = prefixedInteractionText(request.Text)
	if runes := []rune(request.Text); len(runes) > 0 {
		n := min(80, len(runes))
		log.Printf("interaction tts text decision=%s mission=%s text=%q", item.ID, item.MissionKind, string(runes[:n]))
	}
	response, err := w.tts.SynthesizeURL(ctx, request)
	if err != nil {
		return response, err
	}
	if strings.TrimSpace(response.AudioURL) == "" {
		return response, nil
	}
	return response, nil
}
