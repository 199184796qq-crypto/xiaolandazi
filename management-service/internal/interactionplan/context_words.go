package interactionplan

import (
	"context"
	"fmt"
	"livecompanion/management/internal/contextword"
)

type wordSource interface {
	PlanContextWords(context.Context, int64, int64) (contextword.Catalog, error)
}
type wordSourceKey struct{}

func WithContextWordSource(ctx context.Context, source any) context.Context {
	if s, ok := source.(wordSource); ok {
		return context.WithValue(ctx, wordSourceKey{}, s)
	}
	return ctx
}
func withMatchedWords(ctx context.Context, s Snapshot, items []BatchItem) ([]BatchItem, error) {
	out := append([]BatchItem(nil), items...)
	needs := false
	for i := range out {
		out[i].ContextWords = nil
		if questionWordKind(out[i].Kind) && out[i].Text != "" {
			needs = true
		}
	}
	source, ok := ctx.Value(wordSourceKey{}).(wordSource)
	if !needs || !ok {
		return out, nil
	}
	catalog, err := source.PlanContextWords(ctx, s.Scope.Tenant, s.Scope.Plan)
	if err != nil {
		return nil, fmt.Errorf("读取当前方案语境词失败: %w", err)
	}
	for i := range out {
		if questionWordKind(out[i].Kind) {
			out[i].ContextWords = contextword.Match(catalog.Items, out[i].Text+"\n"+out[i].Original)
		}
	}
	return out, nil
}
func questionWordKind(kind string) bool {
	switch kind {
	case "", "semantic", "urgent", "dialogue", "reply_question", "manual", "user_request":
		return true
	}
	return false
}
