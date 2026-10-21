package httpapi

import (
	"context"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	lib "livecompanion/management/internal/speechlibrary"
)

func TestSpeechLibrarySkipReviewPublishesThenAdoptsAcrossSessions(t *testing.T) {
	t.Setenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED", "0")
	s, state, seg, m := libraryFixture(t)
	state.Config.SkipReview, state.ReviewModel = true, nil
	seg.Selected.FinalContentAudit = nil
	sc, err := s.libraryScope(state, seg)
	if err != nil {
		t.Fatal(err)
	}
	seg.Selected.LibraryTextSHA = sc.TextContext
	state.Accepted = []anchorcycle.Segment{seg}
	timeline := []model.LiveAgentPlanTimelineSegment{{SegmentID: "fixture-1", Index: 1, StartMS: 0, EndMS: 3400, Text: seg.Selected.OutputText, SafeCut: true}}
	_, audio, err := s.archiveLibraryVoice(context.Background(), state, seg, []byte("fixture-only-wave"), timeline, 3400)
	if err != nil {
		t.Fatal(err)
	}
	s.publishLibraryVoice(context.Background(), state, seg, audio)
	if m.publishes != 1 || m.entry.Admission.Mode != lib.GenerationAccepted || m.entry.Candidate.FinalContentAudit != nil {
		t.Fatal("accepted current-generation text not published honestly", m.entry)
	}
	// A new runtime keeps content, not playback progress. No real model or TTS
	// transport exists here; an accidental generation call fails this test.
	state.Accepted, state.Attempts = nil, nil
	state.RunID = "new-live-runtime"
	state.Delivery.RuntimeSessionID++
	state.LibrarySourceSHA = anchorcycle.ProductDigest(state.Queue)
	s.anchorCycles = &supplyRepository{state: state}
	out, err := s.advanceAnchorCycle(context.Background(), state.TenantID, state.PlanID, state.RoomID, state.RunID, state.Revision)
	if err != nil || len(out.Accepted) != 1 || out.Accepted[0].Selected.ScoreScope != "speech_library_reused" {
		t.Fatal("durable text hit did not reach real adoption", err)
	}
	hit, err := s.libraryVoice(context.Background(), out, out.Accepted[0])
	if err != nil || hit == nil || !hit.CacheHit || hit.Bundle.Text != seg.Selected.OutputText {
		t.Fatal("adopted text did not claim complete audio", err, hit)
	}
}

func TestSpeechLibraryAdmissionRejectsUnadoptedOrChangedMode(t *testing.T) {
	s, state, seg, _ := libraryFixture(t)
	sc, err := s.libraryScope(state, seg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.libraryAdmittedEntry(state, seg, sc, "entry"); err == nil {
		t.Fatal("unadopted draft published")
	}
	state.Accepted = []anchorcycle.Segment{seg}
	seg.Selected.FinalContentAudit = nil
	if _, err = s.libraryAdmittedEntry(state, seg, sc, "entry"); err == nil {
		t.Fatal("review mode accepted missing review")
	}
	state.Config.SkipReview = true
	sc, _ = s.libraryScope(state, seg)
	if _, err = s.libraryAdmittedEntry(state, seg, sc, "entry"); err == nil {
		t.Fatal("old mode fingerprint published")
	}
	seg.Selected.LibraryTextSHA = sc.TextContext
	if _, err = s.libraryAdmittedEntry(state, seg, sc, "entry"); err != nil {
		t.Fatal(err)
	}
	seg.Selected.OutputText += "未采纳的新句子。"
	if _, err = s.libraryAdmittedEntry(state, seg, sc, "entry"); err == nil {
		t.Fatal("changed text borrowed old adoption")
	}
}

func TestSpeechLibraryScopedFactsOnlyInvalidateAffectedSlot(t *testing.T) {
	s, state, seg, _ := libraryFixture(t)
	state.Config.SkipReview = true
	task := seg.Task
	task.FactScope = &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: "one", Products: []string{"1"}, Data: "商品一当前价格20元。"}}}
	other := task
	other.Key, other.SlotKey, other.FocusLink = "two", "two", "2"
	other.FactScope = &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: "two", Products: []string{"2"}, Data: "商品二当前价格30元。"}}}
	state.Queue = []anchorcycle.Task{task, other}
	seg.Task = task
	base, err := s.libraryScope(state, seg)
	if err != nil {
		t.Fatal(err)
	}
	seg2 := seg
	seg2.Task = other
	before2, _ := s.libraryScope(state, seg2)
	state.Queue[1].FactScope.Facts[0].Data = "商品二当前价格35元。"
	state.Facts += "商品二已更新。"
	after, _ := s.libraryScope(state, seg)
	after2, _ := s.libraryScope(state, seg2)
	if base.TextContext != after.TextContext || base.RoomContext != after.RoomContext || before2.TextContext == after2.TextContext {
		t.Fatal("unrelated inventory evicted or changed facts retained")
	}
	side := seg
	side.Selected.FissionSide = "另一有效侧面"
	different, _ := s.libraryScope(state, side)
	if different.Key() == base.Key() || different.RoomContext != base.RoomContext {
		t.Fatal("side rotation retires preceding side")
	}
	state.Config.TTS.Rate = 1.2
	voice, _ := s.libraryScope(state, seg)
	if voice.TextContext != after.TextContext || voice.VoiceContext == after.VoiceContext {
		t.Fatal("rate change did not retain text while replacing voice")
	}
}
