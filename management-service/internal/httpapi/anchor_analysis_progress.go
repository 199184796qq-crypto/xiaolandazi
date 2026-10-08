package httpapi

import (
	"context"
	"net/http"
)

type analysisProgressKey struct{}
type analysisReporter func(int, string)

func anchorAnalysisStream(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request) {
	stream := openAnchorPreviewStream(w, r)
	if stream == nil {
		return w, r
	}
	report := analysisReporter(func(percent int, message string) {
		stream.send("progress", map[string]any{"percent": percent, "message": message})
	})
	return stream.resultWriter(), r.WithContext(context.WithValue(r.Context(), analysisProgressKey{}, report))
}
func reportAnchorAnalysis(ctx context.Context, percent int, message string) {
	if report, ok := ctx.Value(analysisProgressKey{}).(analysisReporter); ok {
		report(percent, message)
	}
}

type fusionAnalysisKey struct{}
