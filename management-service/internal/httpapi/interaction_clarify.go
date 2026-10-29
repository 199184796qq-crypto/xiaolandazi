package httpapi

import (
	"fmt"
	"strings"
	"time"
)

type pendingClarification struct {
	TenantID int64
	RoomID   int64
	UserID   string
	Original string
	Clarify  string
	Options  []string
	Expires  time.Time
}

func clarificationKey(tenant, room int64, user string) string {
	return fmt.Sprintf("%d:%d:%s", tenant, room, strings.TrimSpace(user))
}

func (s *Server) saveClarification(p pendingClarification) {
	if s == nil || strings.TrimSpace(p.UserID) == "" {
		return
	}
	if p.Expires.IsZero() {
		p.Expires = time.Now().Add(45 * time.Second)
	}
	s.clarifyMu.Lock()
	defer s.clarifyMu.Unlock()
	if s.clarifyPending == nil {
		s.clarifyPending = map[string]pendingClarification{}
	}
	s.clarifyPending[clarificationKey(p.TenantID, p.RoomID, p.UserID)] = p
}

func (s *Server) takeClarification(tenant, room int64, user string) (pendingClarification, bool) {
	if s == nil || strings.TrimSpace(user) == "" {
		return pendingClarification{}, false
	}
	key := clarificationKey(tenant, room, user)
	s.clarifyMu.Lock()
	defer s.clarifyMu.Unlock()
	p, ok := s.clarifyPending[key]
	if !ok {
		return pendingClarification{}, false
	}
	delete(s.clarifyPending, key)
	if time.Now().After(p.Expires) {
		return pendingClarification{}, false
	}
	return p, true
}

func matchClarificationOption(text string, options []string) string {
	text = normalizeClarifyText(text)
	if text == "" {
		return ""
	}
	best, bestLen := "", 0
	for _, option := range options {
		key := normalizeClarifyText(option)
		if key == "" {
			continue
		}
		if strings.Contains(text, key) && len([]rune(key)) > bestLen {
			best, bestLen = option, len([]rune(key))
		}
	}
	return best
}
