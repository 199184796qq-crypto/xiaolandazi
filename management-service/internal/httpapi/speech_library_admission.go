package httpapi

import (
	"errors"

	"livecompanion/management/internal/anchorcycle"
	lib "livecompanion/management/internal/speechlibrary"
)

func libraryScopedTasks(state *anchorcycle.State) bool {
	if len(state.Queue) == 0 {
		return false
	}
	for _, t := range state.Queue {
		if t.FactScope == nil {
			return false
		}
	}
	return true
}

func (s *Server) libraryAdmittedEntry(state *anchorcycle.State, seg anchorcycle.Segment, sc lib.Scope, id string) (lib.Entry, error) {
	c := seg.Selected
	accepted := false
	for _, a := range state.Accepted {
		if c.ID != "" && a.Selected.ID == c.ID && lib.SpokenText(a.Selected) == lib.SpokenText(c) {
			accepted = true
			break
		}
	}
	if !accepted || c.LibraryTextSHA != sc.TextContext || state.CheckApprovedText(c, cycleVoiceText(c)) != nil {
		return lib.Entry{}, errors.New("本段尚未采纳或文字/事实指纹已变化")
	}
	mode := lib.GenerationAccepted
	if !state.Config.SkipReview {
		mode = lib.ReviewPassed
		// Voice production may lag behind the scheduler. Validate the frozen
		// adopted task rather than borrowing the next product's facts.
		frozen := *state
		frozen.Queue, frozen.Cursor, frozen.TaskIndex = []anchorcycle.Task{seg.Task}, 0, 0
		frozen.Phase, frozen.Config.Rolling = "body", false
		frozen.PriceHook, frozen.Lectures = nil, nil
		if state.ProductFocus != nil {
			focus := *state.ProductFocus
			focus.CurrentLink = seg.Task.FocusLink
			frozen.ProductFocus = &focus
		}
		context, err := s.libraryReviewContext(&frozen, c)
		if err != nil {
			return lib.Entry{}, err
		}
		frozen.FinalContentReviewRequired, frozen.FinalContentReviewContextSHA = true, context
		if !frozen.FinalContentCandidateApproved(c) {
			return lib.Entry{}, errors.New("本段没有当前审核模式的有效回执")
		}
	}
	e := lib.Entry{ID: id, Candidate: c, Admission: lib.NewAdmission(sc, c, mode)}
	return e, e.ValidateAdmission(sc, mode)
}
