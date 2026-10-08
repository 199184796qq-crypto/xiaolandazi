package decisionexecutor

import (
	"context"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"strings"
	"time"
	"unicode/utf8"
)

const interactionLengthTolerance = 360

func (w *Worker) tolerantInteractionLength(ctx context.Context, s model.LiveRuntimeSession, item decisionItem, text string) (string, error) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= interactionLengthTolerance {
		return text, nil
	}
	raw, err := interactionpolicy.Read("interaction-length-repair.md")
	if err != nil {
		return "", err
	}
	repairCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	contextText, err := w.answerPrompt(repairCtx, s, item)
	if err != nil {
		return "", err
	}
	response, err := w.agent.Complete(repairCtx, agentgateway.Request{Stage: "speech_generation", Messages: []agentgateway.Message{{Role: "system", Content: string(raw)}, {Role: "user", Content: contextText + "\n待精简的完整回答：\n" + text}}, MaxTokens: 650, EnableThinking: false, Timeout: 16 * time.Second})
	if err != nil {
		return "", err
	}
	shorter, err := w.finalizeSpeechText(repairCtx, s, item, response.Text)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(shorter) == "" || utf8.RuneCountInString(shorter) > interactionLengthTolerance {
		return "", fmt.Errorf("回答精简后仍超过360字容差，撤销本次生成")
	}
	return strings.TrimSpace(shorter), nil
}
