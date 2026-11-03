// Package voicebatch tracks a live host independently of saved plan defaults.
package voicebatch

import (
	"errors"
	"time"

	"livecompanion/management/internal/model"
)

var ErrSwitching = errors.New("主播声音正在切换，互动稍后继续")

type State struct {
	Tenant          int64                         `json:"tenant_id"`
	Room            int64                         `json:"room_id"`
	Session         int64                         `json:"session_id"`
	Version         int64                         `json:"version_id"`
	Revision        int64                         `json:"revision"`
	Active          model.LiveAgentVoiceIdentity  `json:"active"`
	Requested       *model.LiveAgentVoiceIdentity `json:"requested,omitempty"`
	Pending         *model.LiveAgentVoiceIdentity `json:"pending,omitempty"`
	PendingInstance string                        `json:"pending_instance,omitempty"`
	Batch           uint64                        `json:"batch"`
	ElapsedMS       int64                         `json:"elapsed_ms"`
	ObservedAt      time.Time                     `json:"observed_at"`
	Completed       map[string]bool               `json:"completed,omitempty"`
	FullTargets     []string                      `json:"full_targets,omitempty"`
}

func Same(a, b model.LiveAgentVoiceIdentity) bool {
	return a.Provider == b.Provider && a.Model == b.Model && a.VoiceID == b.VoiceID && a.BindingID == b.BindingID && a.ProfileID == b.ProfileID && a.Source == b.Source
}

// Count only continuously observed running time. Pauses, server outages and
// restart gaps never become free elapsed live time.
func (s *State) Observe(now time.Time) {
	if delta := now.Sub(s.ObservedAt); !s.ObservedAt.IsZero() && delta > 0 && delta <= 5*time.Second && s.Pending == nil {
		s.ElapsedMS += delta.Milliseconds()
	}
	s.ObservedAt = now
}

func (s State) Due() bool {
	if s.Pending != nil {
		return false
	}
	if s.ElapsedMS >= int64((2*time.Hour)/time.Millisecond) {
		return true
	}
	if len(s.FullTargets) == 0 {
		return false
	}
	for _, key := range s.FullTargets {
		if !s.Completed[key] {
			return false
		}
	}
	return true
}

// The new host becomes audible only on an exact pending-instance PCM receipt.
func (s *State) Started(instance string, now time.Time) bool {
	if s.Pending == nil || instance != s.PendingInstance {
		return false
	}
	s.Active = *s.Pending
	if s.Requested != nil && Same(*s.Requested, s.Active) {
		s.Requested = nil
	}
	s.Pending = nil
	s.PendingInstance = ""
	s.Batch++
	s.ElapsedMS = 0
	s.ObservedAt = now
	s.Completed = map[string]bool{}
	s.FullTargets = nil
	return true
}
