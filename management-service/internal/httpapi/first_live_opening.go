package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/interactionpolicy"
	lib "livecompanion/management/internal/speechlibrary"
)

var errFirstOpeningRepeated = errors.New("首次开场与近期本方案开场重复")
var errFirstOpeningPreparation = errors.New("首次开场避重准备未完成")

func firstOpeningNormalized(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
func firstOpeningRepeated(text string, history []string) bool {
	n := firstOpeningNormalized(text)
	if n == "" {
		return false
	}
	for _, previous := range history {
		old := firstOpeningNormalized(previous)
		if n == old || openingNearlyIdentical([]rune(n), []rune(old)) {
			return true
		}
	}
	return false
}

// Reject tiny edits to a long opening (punctuation, a nickname or a few words),
// without treating common product facts as evidence that two different scripts
// are duplicates. This is a narrow edit-distance guard, not semantic approval.
func openingNearlyIdentical(a, b []rune) bool {
	if min(len(a), len(b)) < 60 {
		return false
	}
	limit := max(6, max(len(a), len(b))/50)
	if len(a)-len(b) > limit || len(b)-len(a) > limit {
		return false
	}
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i, x := range a {
		prev := row[0]
		row[0] = i + 1
		for j, y := range b {
			old := row[j+1]
			cost := 0
			if x != y {
				cost = 1
			}
			row[j+1] = min(row[j+1]+1, row[j]+1, prev+cost)
			prev = old
		}
	}
	return row[len(b)] <= limit
}
func initialOpeningSegment(state *anchorcycle.State, segment anchorcycle.Segment) bool {
	if state.Surface != "formal_live" || segment.Phase != "opening" {
		return false
	}
	if segment.OpeningVersion == "完整版" {
		return true
	}
	return segment.OpeningVersion == "" && state.AcceptedOffset == 0 && len(state.Accepted) > 0 && state.Accepted[0].Selected.ID == segment.Selected.ID
}
func (s *Server) openingHistoryRepository() lib.OpeningHistoryRepository {
	if h, ok := s.speechLibrary.(lib.OpeningHistoryRepository); ok {
		return h
	}
	if s.store != nil {
		return s.store
	}
	return nil
}
func (s *Server) loadFirstOpeningHistory(ctx context.Context, state *anchorcycle.State) error {
	if state.FirstOpeningHistoryLoaded {
		return nil
	}
	h := s.openingHistoryRepository()
	if h == nil {
		state.FirstOpeningHistoryLoaded = true
		return nil
	}
	sc, e := s.libraryScope(state, anchorcycle.Segment{Phase: "opening", OpeningVersion: "完整版", Task: state.Task()})
	if e != nil {
		return e
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	history, e := h.RecentFirstOpenings(readCtx, sc, 5)
	if e != nil {
		return fmt.Errorf("读取本方案开场避重历史：%w", e)
	}
	state.FirstOpeningHistory, state.FirstOpeningHistoryLoaded = history, true
	return nil
}

// This wrapper leaves the shared generation implementation and its complete P0
// pipeline intact. Only the first opening carries a bounded user-side directive.
func (s *Server) completeLiveCycleText(ctx context.Context, state *anchorcycle.State) (agentgateway.Response, []agentgateway.Message, error) {
	if !state.FirstLiveOpening() {
		return s.completeCycleText(ctx, state)
	}
	if e := s.loadFirstOpeningHistory(ctx, state); e != nil {
		return agentgateway.Response{}, nil, fmt.Errorf("%w：%v", errFirstOpeningPreparation, e)
	}
	mother, e := interactionpolicy.Read("first-live-opening.md")
	if e != nil {
		return agentgateway.Response{}, nil, fmt.Errorf("%w：%v", errFirstOpeningPreparation, e)
	}
	return generateDistinctFirstOpening(state, string(mother), func() (agentgateway.Response, []agentgateway.Message, error) { return s.completeCycleText(ctx, state) })
}

func (s *Server) initialLegacyOpeningPrompt(ctx context.Context, state *anchorcycle.State, prompt string) (string, error) {
	if !state.FirstLiveOpening() {
		return prompt, nil
	}
	if e := s.loadFirstOpeningHistory(ctx, state); e != nil {
		return "", e
	}
	mother, e := interactionpolicy.Read("first-live-opening.md")
	if e != nil {
		return "", e
	}
	return prompt + "\n\n" + firstOpeningDirective(state, string(mother)), nil
}

func firstOpeningDirective(state *anchorcycle.State, mother string) string {
	var snippets []string
	for _, text := range state.FirstOpeningHistory[:min(3, len(state.FirstOpeningHistory))] {
		runes := []rune(text)
		snippets = append(snippets, string(runes[:min(180, len(runes))]))
	}
	data, _ := json.Marshal(map[string]any{"本场标识仅供程序": state.RunID, "本方案历史开场片段仅供避重": snippets})
	return mother + "\n" + string(data)
}

func generateDistinctFirstOpening(state *anchorcycle.State, mother string, generate func() (agentgateway.Response, []agentgateway.Message, error)) (agentgateway.Response, []agentgateway.Message, error) {
	previousFeedback := state.PendingFeedback
	defer func() { state.PendingFeedback = previousFeedback }()
	state.PendingFeedback = previousFeedback + "\n" + firstOpeningDirective(state, mother)
	var response agentgateway.Response
	var messages []agentgateway.Message
	var e error
	for attempt := 0; attempt < 2; attempt++ {
		response, messages, e = generate()
		if e != nil || !firstOpeningRepeated(response.Text, state.FirstOpeningHistory) {
			return response, messages, e
		}
	}
	return response, messages, errFirstOpeningRepeated
}
func (s *Server) rememberFirstLiveOpening(ctx context.Context, state *anchorcycle.State, candidate anchorcycle.Candidate) error {
	h := s.openingHistoryRepository()
	if h == nil {
		return nil
	}
	sc, e := s.libraryScope(state, anchorcycle.Segment{Phase: "opening", OpeningVersion: "完整版", Task: state.Task(), Selected: candidate})
	if e == nil {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		e = h.RememberFirstOpening(persist, sc, cycleVoiceText(candidate))
	}
	return e
}
