package decisionexecutor

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var errTimeReportStale = errors.New("报时话术的原始时间已过期，保留配额重新生成")

type timeReportContextKey struct{}
type timeReportIdentity struct{ Scope, Memory string }
type timeReportDraft struct {
	// Generation metadata travels with reused text, so a TTS retry keeps the
	// same frozen V2 voice/style instead of reverting to legacy defaults.
	Content                 decisionItem
	Text, Scope, Memory     string
	GeneratedAt, ValidUntil time.Time
	AudioUntil              time.Time
	AudioVersion            uint64
}

func timeReportPaidContext(ctx context.Context, at time.Time, audioVersion *uint64) context.Context {
	scope, ok := ctx.Value(workerPaidScopeKey{}).(workerPaidScope)
	if !ok || at.IsZero() {
		return ctx
	}
	scope.key += ":report-generation:" + at.UTC().Format(time.RFC3339Nano)
	if audioVersion != nil {
		scope.key += ":audio:" + strconv.FormatUint(*audioVersion, 10)
	}
	return context.WithValue(ctx, workerPaidScopeKey{}, scope)
}

func isTimeReport(item decisionItem) bool {
	return strings.EqualFold(strings.TrimSpace(item.MissionKind), "time_report")
}
func retryableTimeReport(item decisionItem) bool {
	return isTimeReport(item) && strings.HasPrefix(strings.ToUpper(item.Topic), "INTERACTION:TIME_REPORT:")
}

func (w *Worker) timeReportText(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem) string {
	text, _ := w.timeReportDraftText(ctx, session, item, func(callCtx context.Context) (string, error) {
		return (timeReportStrategy{worker: w}).Respond(callCtx, session, *item), nil
	})
	return text
}

// Both generation routes use the same program-owned clock, validity and paid
// draft cache. A retry never relabels old text with a fresh generation time.
func (w *Worker) timeReportDraftText(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem, generate func(context.Context) (string, error)) (string, error) {
	identity, cachedScope := ctx.Value(timeReportContextKey{}).(timeReportIdentity)
	key := preparationKey(session, *item)
	w.mu.Lock()
	draft, ok := w.timeReportDrafts[key]
	w.mu.Unlock()
	if !cachedScope || !retryableTimeReport(*item) || !ok || !w.now().Before(draft.ValidUntil) || draft.Scope != identity.Scope || draft.Memory != identity.Memory {
		at := w.now().UTC()
		item.TimeReportGeneratedAt = at
		text, err := generate(timeReportPaidContext(ctx, at, nil))
		if err != nil {
			return "", err
		}
		until, valid := timeReportValidity(text, at)
		if !valid {
			text = timeReportFallback(timeOfDay(at.In(beijingZone)))
			until, _ = timeReportValidity(text, at)
		}
		draft = timeReportDraft{Content: *item, Text: text, Scope: identity.Scope, Memory: identity.Memory, GeneratedAt: at, ValidUntil: until}
		if cachedScope && retryableTimeReport(*item) {
			w.mu.Lock()
			if w.timeReportDrafts == nil {
				w.timeReportDrafts = make(map[preparedInteractionKey]timeReportDraft)
			}
			for k, v := range w.timeReportDrafts {
				if !w.now().Before(v.ValidUntil) {
					delete(w.timeReportDrafts, k)
				}
			}
			if len(w.timeReportDrafts) >= maxPreparedInteractions {
				var oldest preparedInteractionKey
				var earliest time.Time
				for k, v := range w.timeReportDrafts {
					if earliest.IsZero() || v.GeneratedAt.Before(earliest) {
						oldest, earliest = k, v.GeneratedAt
					}
				}
				delete(w.timeReportDrafts, oldest)
			}
			w.timeReportDrafts[key] = draft
			w.mu.Unlock()
		}
	}
	if !draft.AudioUntil.IsZero() && !w.now().Before(draft.AudioUntil) {
		// A known expired asset is a new TTS operation. Unknown/failed receipts
		// retain their operation key so retrying cannot double charge it.
		draft.AudioVersion++
		draft.AudioUntil = time.Time{}
		w.mu.Lock()
		w.timeReportDrafts[key] = draft
		w.mu.Unlock()
	}
	item.TimeReportGeneratedAt, item.TimeReportValidUntil = draft.GeneratedAt, draft.ValidUntil
	item.TimeReportAudioVersion = draft.AudioVersion
	item.PurposeKnowledge = append([]byte(nil), draft.Content.PurposeKnowledge...)
	item.EmotionDisabled = draft.Content.EmotionDisabled
	item.SelectedInterrupt, item.SelectedResume = draft.Content.SelectedInterrupt, draft.Content.SelectedResume
	item.BridgeText = draft.Content.BridgeText
	return draft.Text, nil
}

func (w *Worker) rememberTimeReportAudio(session model.LiveRuntimeSession, item decisionItem, until time.Time) {
	if !retryableTimeReport(item) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	key := preparationKey(session, item)
	if draft, ok := w.timeReportDrafts[key]; ok && draft.GeneratedAt.Equal(item.TimeReportGeneratedAt) {
		draft.AudioUntil = until
		w.timeReportDrafts[key] = draft
	}
}

func (w *Worker) checkTimeReport(item decisionItem) error {
	if isTimeReport(item) && (item.TimeReportValidUntil.IsZero() || !w.now().Before(item.TimeReportValidUntil)) {
		return errTimeReportStale
	}
	return nil
}

// Content may outlive a signed audio link. Keep the text draft; renew only
// audio if its URL expires, without paying for a new model answer.
func timeReportAudioExpiry(raw string, validity, now time.Time) time.Time {
	until := validity
	link, err := url.Parse(raw)
	if err == nil {
		if seconds, err := strconv.ParseInt(link.Query().Get("Expires"), 10, 64); err == nil {
			if end := time.Unix(seconds, 0).Add(-5 * time.Second); end.Before(until) {
				until = end
			}
		} else if link.RawQuery != "" {
			if end := now.Add(preparedInteractionTTL); end.Before(until) {
				until = end
			}
		}
	}
	return until
}
