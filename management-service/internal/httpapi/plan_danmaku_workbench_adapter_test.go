package httpapi

import (
	"context"
	"livecompanion/management/internal/answerflow"
)

func runDanmakuTest(ctx context.Context, input danmakuInput, lib danmakuLibrary, e danmakuEmbedder, complete danmakuComplete) (answerflow.Result, error) {
	return newDanmakuAnswerService(lib, e, complete).Analyze(ctx, answerflow.Request{Scope: answerflow.Scope{Tenant: 1, Plan: 1}, Input: input})
}
