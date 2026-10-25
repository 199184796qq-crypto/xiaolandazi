package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"time"
	"unicode/utf8"
)

// Supply selection is distinct from provider requests and Core playback. A
// source sentence may produce several units; counting those as source updates
// would inflate both the refresh rate and the apparent TTS bill.
type recordingSupplyStats struct {
	TenantID          int64  `json:"tenant_id"`
	RoomID            int64  `json:"room_id"`
	SessionID         int64  `json:"session_id"`
	PlanID            int64  `json:"plan_id"`
	Round             int    `json:"round"`
	TabID             string `json:"tab_id"`
	DesiredPercent    int    `json:"desired_percent"`
	EffectivePercent  int    `json:"effective_percent"`
	BudgetPaused      bool   `json:"budget_paused"`
	SourceSentences   int    `json:"source_sentences"`
	SourceCharacters  int    `json:"source_characters"`
	PlannedUpdates    int    `json:"planned_updates"`
	PlannedCharacters int    `json:"planned_characters"`
	Visited           int    `json:"visited"`
	Generated         int    `json:"generated_sentences"`
	Updated           int    `json:"updated_sentences"`
	UpdatedCharacters int    `json:"updated_source_characters"`
	Reused            int    `json:"reused_sentences"`
	PersistentReused  int    `json:"persistent_reused_sentences"`
	Unavailable       int    `json:"unavailable_sentences"`
	RefreshFailed     int    `json:"refresh_failed_sentences"`
	BudgetDeclined    int    `json:"budget_declined_sentences"`
	ReadyUnits        int    `json:"ready_units"`
	Complete          bool   `json:"complete"`
	StartedAt         string `json:"started_at"`
	FinishedAt        string `json:"finished_at"`
}

func newRecordingSupplyStats(tenant, room, session, plan int64, round int, tab string, states []recordingSentenceState, selected []bool, desired, effective int) *recordingSupplyStats {
	s := &recordingSupplyStats{TenantID: tenant, RoomID: room, SessionID: session, PlanID: plan,
		Round: round, TabID: tab, DesiredPercent: desired, EffectivePercent: effective,
		BudgetPaused: desired > 0 && effective == 0, SourceSentences: len(states), StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for i, state := range states {
		n := utf8.RuneCountInString(state.Source)
		s.SourceCharacters += n
		if i < len(selected) && selected[i] {
			s.PlannedUpdates++
			s.PlannedCharacters += n
		}
	}
	return s
}

func (s *recordingSupplyStats) observe(source string, refresh bool, result recordingReuseResult, err error) {
	s.Visited++
	if errors.Is(err, errLiveCostOptionalDeclined) || errors.Is(result.GenerationErr, errLiveCostOptionalDeclined) {
		s.BudgetDeclined++
	}
	if err != nil {
		s.Unavailable++
		if refresh {
			s.RefreshFailed++
		}
		return
	}
	s.ReadyUnits += len(result.Units)
	if result.Generated {
		s.Generated++
		if refresh {
			s.Updated++
			s.UpdatedCharacters += utf8.RuneCountInString(source)
		}
	} else {
		s.Reused++
		if result.CacheHit {
			s.PersistentReused++
		}
		if refresh {
			s.RefreshFailed++
		}
	}
}

func (s *recordingSupplyStats) write(complete bool) {
	s.Complete = complete && s.Visited == s.SourceSentences
	s.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(s)
	// Contains IDs/counters only, never source text, credentials or audio URLs.
	log.Printf("recording round supply %s", raw)
}
