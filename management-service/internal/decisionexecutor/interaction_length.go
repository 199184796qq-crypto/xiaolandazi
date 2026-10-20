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
	withinTolerance := utf8.RuneCountInString(text) <= interactionLengthTolerance
	focusRepair := isBriefViewerQuestion(item) && utf8.RuneCountInString(text) > 120 && withinTolerance
	if withinTolerance && !focusRepair {
		return text, nil
	}
	raw, err := interactionpolicy.Read("interaction-length-repair.md")
	if err != nil {
		return "", err
	}
	if focusRepair {
		focus, err := interactionpolicy.Read("interaction-question-focus.md")
		if err != nil {
			return "", err
		}
		raw = append(raw, append([]byte("\n\n"), focus...)...)
	}
	repairCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	contextText, err := w.answerPrompt(repairCtx, s, item)
	if err != nil {
		return "", err
	}
	stage := "speech_generation"
	if isViewerQuestionAnswer(item) {
		stage = "interaction_agent_answer"
	}
	response, err := w.agent.Complete(repairCtx, agentgateway.Request{Stage: stage, Messages: []agentgateway.Message{{Role: "system", Content: string(raw)}, {Role: "user", Content: contextText + "\n待精简的完整回答：\n" + text}}, MaxTokens: 650, EnableThinking: false, Timeout: 16 * time.Second})
	if err != nil {
		if focusRepair {
			return text, nil
		}
		return "", err
	}
	shorter := strings.TrimSpace(item.nicknameSpeechText(response.Text))
	if shorter == "" || utf8.RuneCountInString(shorter) > interactionLengthTolerance {
		if focusRepair {
			return text, nil
		}
		return "", fmt.Errorf("回答精简后仍超过360字容差，撤销本次生成")
	}
	if focusRepair && utf8.RuneCountInString(shorter) >= utf8.RuneCountInString(text) {
		return text, nil
	}
	return strings.TrimSpace(shorter), nil
}
