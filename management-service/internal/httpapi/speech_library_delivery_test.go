package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	lib "livecompanion/management/internal/speechlibrary"
	"testing"
)

func TestSpeechLibraryRepairReceiptDoesNotInvalidateAdoptedVoice(t *testing.T) {
	s, state, seg, memory := libraryFixture(t)
	_, contract, _, err := agentgateway.TextPromptContract(state.TextModel.Model)
	if err != nil {
		t.Fatal(err)
	}
	// A repaired body was adopted with an empty request receipt. The normal
	// producer has moved on before the voice worker archives this exact body.
	state.TextPromptSHA = ""
	before, err := s.libraryScope(state, seg)
	if err != nil {
		t.Fatal(err)
	}
	seg.Selected.LibraryTextSHA = before.TextContext
	state.TextPromptSHA = contract
	state.Phase = "closing"
	after, err := s.libraryScope(state, seg)
	if err != nil || after.TextContext != before.TextContext {
		t.Fatal("next request invalidated adopted text", err)
	}
	timeline := []model.LiveAgentPlanTimelineSegment{{SegmentID: "test-adopted-1", Index: 1, StartMS: 0, EndMS: 1800, Text: seg.Selected.OutputText, SafeCut: true}}
	voice, _, err := s.archiveLibraryVoice(context.Background(), state, seg, []byte("fixture-wave"), timeline, 1800)
	if err != nil || voice == nil || voice.Status != "ready" || memory.reserves != 1 {
		t.Fatal("complete approved audio was discarded", err)
	}
	// The reverse transition (normal -> repair) is also harmless.
	state.TextPromptSHA = ""
	if next, e := s.libraryScope(state, seg); e != nil || next.TextContext != before.TextContext {
		t.Fatal("repair invalidated a normal body", e)
	}
	// Actual fact changes remain a hard stop, rather than a temporary-cache bypass.
	state.Facts += "\n商品适用条件已修订"
	if _, _, e := s.archiveLibraryVoice(context.Background(), state, seg, nil, timeline, 1800); !errors.Is(e, lib.ErrStale) {
		t.Fatal("changed facts accepted old audio", e)
	}
}
