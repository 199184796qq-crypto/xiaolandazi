package httpapi

import (
	"context"
	"errors"

	"livecompanion/management/internal/sharedfilelock"
)

type scriptManualProgress string

const (
	scriptManualQueued         scriptManualProgress = "queued"
	scriptManualWriting        scriptManualProgress = "writing"
	scriptManualReviewing      scriptManualProgress = "reviewing"
	scriptManualRevising       scriptManualProgress = "revising"
	scriptManualTextReady      scriptManualProgress = "text_ready"
	scriptManualSynthesizing   scriptManualProgress = "synthesizing"
	scriptManualAudioReady     scriptManualProgress = "audio_ready"
	scriptManualAudioAvailable scriptManualProgress = "audio_available"
	scriptManualReviewRejected scriptManualProgress = "review_rejected"
	scriptManualAudioFailed    scriptManualProgress = "audio_failed"
	scriptManualWorkFailed     scriptManualProgress = "generation_failed"
)

var errSpecifiedScriptReviewRejected = errors.New("指定话术未通过红线检查")

type scriptManualProgressKey struct{}

func reportScriptManualProgress(ctx context.Context, progress scriptManualProgress) {
	if report, ok := ctx.Value(scriptManualProgressKey{}).(func(scriptManualProgress)); ok {
		report(progress)
	}
}

// Only a foreground selection waiting for audio displays generation progress.
func setScriptManualProgress(state *scriptManualState, job *scriptManualGeneration, progress scriptManualProgress) {
	job.Progress = progress
	if j := state.Current; job.Waiter != "" && j != nil && j.RequestID == job.Waiter && j.Status == "preparing" {
		j.Progress = progress
		state.Receipts[j.RequestID] = *j
	}
}

func updateScriptManualProgress(ctx context.Context, path string, command scriptManualJump, progress scriptManualProgress) error {
	release, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	state, err := readScriptManualState(path)
	if err != nil {
		return err
	}
	if len(state.Generation) == 0 {
		return nil
	}
	job := &state.Generation[0]
	if job.Command.RequestID != command.RequestID || job.Progress == progress {
		return nil
	}
	setScriptManualProgress(&state, job, progress)
	return writeVolcengineJSON(path, state)
}

func scriptManualGenerationResult(progress scriptManualProgress, cause error) (scriptManualProgress, string) {
	switch {
	case cause == nil && progress == scriptManualSynthesizing:
		return scriptManualAudioReady, ""
	case cause == nil:
		return scriptManualAudioAvailable, ""
	case errors.Is(cause, errSpecifiedScriptReviewRejected):
		return scriptManualReviewRejected, "话术审核未通过，当前播放继续，请修改提示词或重试"
	case progress == scriptManualSynthesizing:
		return scriptManualAudioFailed, "话术已生成，声音合成失败，当前播放继续，请稍后重试"
	default:
		return scriptManualWorkFailed, "目标话术准备失败，当前播放继续，请稍后重试"
	}
}
