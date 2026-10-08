package httpapi

import (
	"context"
	"net/http"
)

type cycleProgressKey struct{}
type cycleProgressReporter func(int)

// Opt-in transport only. Formal room producers and old JSON clients are
// unchanged. Authorization must finish before this helper opens the stream.
func cycleProgressStream(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request) {
	if !productionPreviewRequest(r) {
		return w, r
	}
	stream := openAnchorPreviewStream(w, r)
	if stream == nil {
		return w, r
	}
	last := -1
	report := cycleProgressReporter(func(percent int) {
		percent = min(100, max(0, percent))
		if percent <= last {
			return
		}
		last = percent
		// No stage names, prompt text, credentials or upstream responses.
		stream.send("progress", map[string]any{"message": "正在准备声音…", "percent": percent})
	})
	r = r.WithContext(context.WithValue(r.Context(), cycleProgressKey{}, report))
	report(0)
	return stream.resultWriter(), r
}

// Milestones reflect completed work, not elapsed time or guessed duration.
// Detached jobs retain the reporter's context values until their response ends.
func reportCycleProgress(ctx context.Context, percent int) {
	if report, ok := ctx.Value(cycleProgressKey{}).(cycleProgressReporter); ok {
		report(percent)
	}
}
