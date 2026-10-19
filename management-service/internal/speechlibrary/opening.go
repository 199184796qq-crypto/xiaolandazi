package speechlibrary

import "context"

// OpeningHistoryRepository stores approved first-opening text as a room/plan
// asset. It does not resume sessions or supply old opening text for playback.
type OpeningHistoryRepository interface {
	RecentFirstOpenings(context.Context, Scope, int) ([]string, error)
	RememberFirstOpening(context.Context, Scope, string) error
}
