package decisionexecutor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
)

const (
	semanticRecentSpeechWindow         = 10 * time.Minute
	semanticRecentSpeechTTL            = 30 * time.Minute
	semanticRecentSpeechCandidateLimit = 40
	// Live Chinese paraphrase samples cluster around 0.60-0.82; unrelated samples stayed below ~0.43.
	semanticRecentSpeechWeakThreshold   = 0.60
	semanticRecentSpeechStrongThreshold = 0.78
)

type recentSpeechSemanticRisk struct {
	Matched bool
	Strong  bool
	Score   float64
	Text    string
}

func (w *Worker) recentSpeechSemanticRisk(
	ctx context.Context,
	session model.LiveRuntimeSession,
	text string,
) (recentSpeechSemanticRisk, error) {
	if w == nil || w.semanticService == nil || !w.semanticService.Enabled() {
		return recentSpeechSemanticRisk{}, nil
	}
	text = strings.TrimSpace(text)
	if text == "" || session.TenantID <= 0 || session.RoomID <= 0 {
		return recentSpeechSemanticRisk{}, nil
	}
	now := time.Now().UTC()
	if w.now != nil {
		now = w.now()
	}
	matches, err := w.semanticService.Search(ctx, text, semantic.Query{
		TenantID:       session.TenantID,
		RoomID:         session.RoomID,
		ContentType:    semantic.ContentTypeRecentSpeech,
		Since:          now.Add(-semanticRecentSpeechWindow),
		Limit:          semanticRecentSpeechCandidateLimit,
		CandidateLimit: 500,
		MinScore:       semanticRecentSpeechWeakThreshold,
	})
	if err != nil {
		return recentSpeechSemanticRisk{}, err
	}
	for _, match := range matches {
		if match.Score < semanticRecentSpeechWeakThreshold {
			continue
		}
		w.semanticService.RecordRetrieval(semantic.ContentTypeRecentSpeech, true)
		return recentSpeechSemanticRisk{
			Matched: true,
			Strong:  match.Score >= semanticRecentSpeechStrongThreshold,
			Score:   match.Score,
			Text:    strings.TrimSpace(match.Document.Text),
		}, nil
	}
	w.semanticService.RecordRetrieval(semantic.ContentTypeRecentSpeech, false)
	return recentSpeechSemanticRisk{}, nil
}

func (w *Worker) indexRecentSpeech(
	ctx context.Context,
	session model.LiveRuntimeSession,
	item decisionItem,
	text string,
) error {
	if w == nil || w.semanticService == nil || !w.semanticService.Enabled() {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" || session.TenantID <= 0 || session.RoomID <= 0 {
		return nil
	}
	now := time.Now().UTC()
	if w.now != nil {
		now = w.now()
	}
	expiresAt := now.Add(semanticRecentSpeechTTL)
	sourceID := strings.TrimSpace(item.ID)
	if sourceID == "" {
		hash := semantic.HashText(text)
		if len(hash) > 16 {
			hash = hash[:16]
		}
		sourceID = "anon-" + hash
	}
	return w.semanticService.Index(ctx, semantic.Document{
		TenantID:      session.TenantID,
		RoomID:        session.RoomID,
		ContentType:   semantic.ContentTypeRecentSpeech,
		SourceID:      fmt.Sprintf("speech:%d:%s", session.ID, sourceID),
		SourceVersion: 1,
		Status:        "active",
		Text:          text,
		ExpiresAt:     &expiresAt,
	})
}
