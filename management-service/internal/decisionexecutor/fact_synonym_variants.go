package decisionexecutor

import (
	"context"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionpolicy"
)

// rewriteAnswerText 对已生成的互动回答正文做同义改写：换「使用方式/动作」措辞、锁死数字与称呼，
// 打破高频短语（如"叮1分钟"）复读。失败返回空字符串，调用方降级用原文。
func (w *Worker) rewriteAnswerText(ctx context.Context, text string) string {
	if w == nil || w.agent == nil || strings.TrimSpace(text) == "" {
		return ""
	}
	mother, err := interactionpolicy.Read("fact-speech-rewrite.md")
	if err != nil {
		return ""
	}
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resp, err := w.agent.Complete(callCtx, agentgateway.Request{
		Stage:          "interaction_answer_rewrite",
		Messages:       []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: text}},
		ResponseFormat: agentgateway.ResponseText,
		MaxTokens:      800,
		EnableThinking: false,
	})
	if err != nil {
		return ""
	}
	if strings.TrimSpace(resp.Text) == "" {
		return ""
	}
	return strings.TrimSpace(resp.Text)
}
