package anchorcycle

import (
	"encoding/json"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/lecturetask"
	"testing"
)

func TestLectureAdoptionPreservesOrdinaryCursorAndFailureCooldown(t *testing.T) {
	if !lectureEnabled {
		t.Skip("主线讲解任务已暂缓（lectureEnabled=false）")
	}
	s := lectureFixture(t)
	s.Lectures.PlayedMS, s.Lectures.CompletedCycles = 720000, 6
	before, cursor, cycle, body := s.Task(), s.Cursor, s.CycleNumber, s.BodyCount
	if !s.BeginLecture() {
		t.Fatal("missing slot")
	}
	text := "土麻鸭是这个原料故事的重点。"
	c := Candidate{ID: "demo", Text: text, Score: anchorstylescore.Result{OverallScore: 100}, LectureAudit: &LectureAudit{Passed: true, TextSHA: ProductDigest(text), ScopeSHA: ProductDigest(s.Task().LectureFacts)}}
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if s.Cursor != cursor || s.CycleNumber != cycle || s.BodyCount != body || s.Task() != before || s.Lectures.Uses[1].Count != 0 {
		t.Fatal("lecture consumed normal wheel or counted as played")
	}
	s.DropLectureTrack("demo")
	if !s.Accepted[0].LectureSkipped || s.Lectures.ReservedTopic != 0 || s.BeginLecture() {
		t.Fatal("failed optional voice did not release/defer")
	}
	s.Lectures.PlayedMS += 300000
	if !s.BeginLecture() {
		t.Fatal("transient failure disabled topic for whole run")
	}
}

func lectureFixture(t *testing.T) State {
	t.Helper()
	s, err := New([]Task{{SellingPoint: "规格", Angle: "选择方法"}}, Config{Rolling: true, MaxChars: 250, BodySegments: 2})
	if err != nil {
		t.Fatal(err)
	}
	s.Surface = "formal_live"
	s.Phase = "opening"
	s.CycleNumber = 7
	s.Lectures = NewLectures(lecturetask.Library{Policy: lecturetask.DefaultPolicy(), Tasks: []lecturetask.Task{{ID: 1, Revision: 1, Enabled: true, Valid: true, Title: "原料", Purpose: "原料选择", Angles: []lecturetask.Angle{{Title: "产地", Goal: "说明来源"}, {Title: "口感", Goal: "说明口感"}}, Facts: []lecturetask.Fact{{Name: "原料", Text: "土麻鸭"}}}}})
	return s
}
func TestLectureRequiresActualPlaybackAndOneReservation(t *testing.T) {
	if !lectureEnabled {
		t.Skip("主线讲解任务已暂缓（lectureEnabled=false）")
	}
	s := lectureFixture(t)
	if s.BeginLecture() {
		t.Fatal("generated cycles counted as played")
	}
	s.Lectures.CompletedCycles = 6
	s.Lectures.PlayedMS = 720000
	if !s.BeginLecture() {
		t.Fatal("eligible lecture not selected")
	}
	if s.BeginLecture() {
		t.Fatal("second reservation")
	}
	raw, _ := json.Marshal(s)
	var copy State
	if err := json.Unmarshal(raw, &copy); err != nil || copy.Task() != s.Task() {
		t.Fatal("unstable reserved task", err)
	}
	s.Lectures.Active = nil
	s.Phase = "opening"
	if s.BeginLecture() {
		t.Fatal("prefetch filled multiple lecture slots")
	}
}
func TestLectureReceiptIdempotentAndPartialDoesNotComplete(t *testing.T) {
	s := lectureFixture(t)
	r := s.Lectures
	r.ReservedTopic = 1
	r.ReservedCandidate = "L"
	r.Tracks["L"] = LectureTrack{Topic: 1, Angle: 0}
	r.Tracks["C"] = LectureTrack{Closing: true}
	s.ObservePlayback([]PlaybackReceipt{{Sequence: 1, TrackID: "L", PlayedMS: 10000}, {Sequence: 2, TrackID: "L", PlayedMS: 10000, Terminal: true}}, 720000)
	if r.LectureMS != 10000 || r.Uses[1].Count != 0 || r.ReservedTopic != 0 || r.Uses[1].LastMS == 0 {
		t.Fatal(r)
	}
	s.ObservePlayback([]PlaybackReceipt{{Sequence: 2, TrackID: "L", PlayedMS: 10000, Terminal: true}, {Sequence: 3, TrackID: "C", PlayedMS: 40000, Complete: true, Terminal: true}}, 760000)
	s.ObservePlayback([]PlaybackReceipt{{Sequence: 3, TrackID: "C", PlayedMS: 40000, Complete: true}}, 760000)
	if r.CompletedCycles != 1 || r.LectureMS != 10000 {
		t.Fatal("duplicate receipt advanced progress", r)
	}
}
func TestLectureBudgetParentCooldownAndUnrelatedScope(t *testing.T) {
	s := lectureFixture(t)
	r := s.Lectures
	r.CompletedCycles = 6
	r.PlayedMS = 720000
	r.LectureMS = 20000
	if s.BeginLecture() {
		t.Fatal("reserved duration escaped budget")
	}
	r.LectureMS = 0
	r.Uses[1] = LectureUse{Count: 1, LastMS: 700000, Angles: map[int]int{0: 1}}
	if s.BeginLecture() {
		t.Fatal("another angle bypassed parent cooldown")
	}
	r.Uses = map[int64]LectureUse{}
	r.Library.Tasks[0].Links = []string{"2"}
	if s.BeginLecture() {
		t.Fatal("foreign product story selected")
	}
}
