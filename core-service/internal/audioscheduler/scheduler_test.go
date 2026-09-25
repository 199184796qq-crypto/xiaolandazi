package audioscheduler

import (
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time      { return c.now }
func (c *fakeClock) Add(d time.Duration) { c.now = c.now.Add(d) }

func testSegment(id string, role SegmentRole, kind SourceKind, duration int64) Segment {
	source := SourceRef{Kind: kind, URI: "file:///" + id + ".wav"}
	if kind == SourceTTSStream {
		source = SourceRef{Kind: kind, StreamID: "stream-" + id}
	}
	return Segment{
		ID: id, Role: role, Source: source, DurationMS: duration,
		Mainline: MainlineCursor{
			PlanID: "plan-1", TrackID: "track-1", UnitID: id, SegmentID: id,
		},
	}
}

func prepareReady(t *testing.T, registry *Registry, room int64, slot SlotID, segment Segment) SlotSnapshot {
	t.Helper()
	snapshot, err := registry.Prepare(room, slot, segment)
	if err != nil {
		t.Fatal(err)
	}
	prepared := snapshot.Slots[0]
	if slot == SlotB {
		prepared = snapshot.Slots[1]
	}
	snapshot, _, err = registry.MarkReady(room, slot, segment.ID, prepared.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if slot == SlotA {
		return snapshot.Slots[0]
	}
	return snapshot.Slots[1]
}

func TestDoubleBufferSingleOutputAndSafeSwitch(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)}
	registry := newRegistry(clock.Now)

	main := testSegment("main-1", RoleMainline, SourceLocalFile, 10_000)
	a := prepareReady(t, registry, 1001, SlotA, main)
	if _, err := registry.Start(1001, SlotA, main.ID, a.Generation); err != nil {
		t.Fatal(err)
	}

	interaction := testSegment("answer-1", RoleInteraction, SourceTTSStream, 2_500)
	b := prepareReady(t, registry, 1001, SlotB, interaction)
	resume := MainlineCursor{PlanID: "plan-1", TrackID: "track-1", UnitID: "unit-next", SegmentID: "main-2"}
	if _, result, err := registry.ArmSwitch(1001, SwitchRequest{
		ID: "sw-1", TargetSlot: SlotB,
		Stop:   StopPoint{SegmentID: main.ID, OffsetMS: 4_000, SafePointID: "sp-4s"},
		Resume: resume,
	}); err != nil || result.Switched {
		t.Fatalf("arm switch result=%#v err=%v", result, err)
	}

	clock.Add(3 * time.Second)
	if _, result, err := registry.Observe(1001, PlaybackObservation{
		SegmentID: main.ID, Generation: a.Generation,
		Status: PlaybackProgress, ProgressMS: 3_000,
	}); err != nil || result.Switched {
		t.Fatalf("early switch result=%#v err=%v", result, err)
	}

	clock.Add(time.Second)
	snapshot, result, err := registry.Observe(1001, PlaybackObservation{
		SegmentID: main.ID, Generation: a.Generation,
		Status: PlaybackProgress, ProgressMS: 4_050,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Switched || snapshot.ActiveSlot != SlotB || result.ToSlot != SlotB {
		t.Fatalf("switch failed: snapshot=%#v result=%#v", snapshot, result)
	}
	if snapshot.Slots[0].State != SlotSuspended || snapshot.Slots[1].State != SlotPlaying {
		t.Fatalf("unexpected slot states: %#v", snapshot.Slots)
	}
	if snapshot.Output.Segment == nil || snapshot.Output.Segment.ID != interaction.ID || snapshot.Output.Generation != b.Generation {
		t.Fatalf("unexpected output: %#v", snapshot.Output)
	}
	if snapshot.SuspendedMainline == nil || snapshot.SuspendedMainline.SegmentID != "main-2" {
		t.Fatalf("resume cursor lost: %#v", snapshot.SuspendedMainline)
	}
	if _, err := registry.Start(1001, SlotA, main.ID, a.Generation); err == nil {
		t.Fatal("second simultaneous output unexpectedly allowed")
	}
}

func TestInteractionReturnsToRepreparedMainline(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)}
	registry := newRegistry(clock.Now)

	main1 := testSegment("main-1", RoleMainline, SourceLocalFile, 6_000)
	a1 := prepareReady(t, registry, 8, SlotA, main1)
	if _, err := registry.Start(8, SlotA, main1.ID, a1.Generation); err != nil {
		t.Fatal(err)
	}

	answer := testSegment("tts-answer", RoleInteraction, SourceTTSStream, 1_400)
	b := prepareReady(t, registry, 8, SlotB, answer)
	if _, _, err := registry.ArmSwitch(8, SwitchRequest{
		ID: "to-answer", TargetSlot: SlotB,
		Stop:   StopPoint{SegmentID: main1.ID, OffsetMS: 2_000},
		Resume: MainlineCursor{PlanID: "p", TrackID: "t", UnitID: "u2", SegmentID: "main-2"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, result, err := registry.Observe(8, PlaybackObservation{
		SegmentID: main1.ID, Generation: a1.Generation,
		Status: PlaybackProgress, ProgressMS: 2_050,
	}); err != nil || !result.Switched {
		t.Fatalf("switch to interaction result=%#v err=%v", result, err)
	}

	main2 := testSegment("main-2", RoleMainline, SourceGeneratedFile, 8_000)
	main2.Mainline = MainlineCursor{PlanID: "p", TrackID: "t", UnitID: "u2", SegmentID: "main-2"}
	prepared, slot, err := registry.PrepareResume(8, main2)
	if err != nil {
		t.Fatal(err)
	}
	if slot != SlotA {
		t.Fatalf("resume prepared in slot %s want A", slot)
	}
	a2 := prepared.Slots[0]
	if _, _, err := registry.MarkReady(8, SlotA, main2.ID, a2.Generation); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.ArmSwitch(8, SwitchRequest{
		ID: "back-main", TargetSlot: SlotA,
		Stop: StopPoint{SegmentID: answer.ID, OffsetMS: 1_400},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, result, err := registry.CompleteActive(8, answer.ID, b.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Switched || snapshot.ActiveSlot != SlotA {
		t.Fatalf("did not return to mainline: snapshot=%#v result=%#v", snapshot, result)
	}
	if snapshot.Output.Segment == nil || snapshot.Output.Segment.ID != main2.ID || snapshot.Output.Generation != a2.Generation {
		t.Fatalf("wrong resumed output: %#v", snapshot.Output)
	}
}

func TestPrepareResumeRejectsWrongSemanticCursor(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)}
	registry := newRegistry(clock.Now)

	main := testSegment("main-1", RoleMainline, SourceLocalFile, 6_000)
	a := prepareReady(t, registry, 18, SlotA, main)
	if _, err := registry.Start(18, SlotA, main.ID, a.Generation); err != nil {
		t.Fatal(err)
	}
	answer := testSegment("answer", RoleInteraction, SourceTTSStream, 1_000)
	prepareReady(t, registry, 18, SlotB, answer)
	if _, _, err := registry.ArmSwitch(18, SwitchRequest{
		ID: "to-answer", TargetSlot: SlotB,
		Stop:   StopPoint{SegmentID: main.ID, OffsetMS: 1_000},
		Resume: MainlineCursor{PlanID: "plan-expected", TrackID: "track-2", UnitID: "unit-9", SegmentID: "resume-9"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, result, err := registry.Observe(18, PlaybackObservation{
		SegmentID: main.ID, Generation: a.Generation,
		Status: PlaybackProgress, ProgressMS: 1_100,
	}); err != nil || !result.Switched {
		t.Fatalf("switch result=%#v err=%v", result, err)
	}

	wrong := testSegment("wrong", RoleMainline, SourceGeneratedFile, 2_000)
	if _, _, err := registry.PrepareResume(18, wrong); err == nil {
		t.Fatal("wrong semantic resume segment unexpectedly accepted")
	}
}

func TestLateJoinSnapshotUsesRoomClock(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)}
	registry := newRegistry(clock.Now)

	main := testSegment("main", RoleMainline, SourceCachedAudio, 20_000)
	a := prepareReady(t, registry, 99, SlotA, main)
	if _, err := registry.Start(99, SlotA, main.ID, a.Generation); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Observe(99, PlaybackObservation{
		SegmentID: main.ID, Generation: a.Generation,
		Status: PlaybackProgress, ProgressMS: 3_200,
	}); err != nil {
		t.Fatal(err)
	}
	clock.Add(850 * time.Millisecond)
	snapshot, ok := registry.Snapshot(99)
	if !ok {
		t.Fatal("missing room snapshot")
	}
	if snapshot.Output.ProgressMS != 4_050 {
		t.Fatalf("late join progress=%d want=4050", snapshot.Output.ProgressMS)
	}
	if snapshot.Output.Segment == nil || snapshot.Output.Segment.ID != main.ID {
		t.Fatalf("late join has wrong segment: %#v", snapshot.Output)
	}
}

func TestStaleGenerationCannotOverwriteReusedSlot(t *testing.T) {
	clock := &fakeClock{now: time.Now().UTC()}
	registry := newRegistry(clock.Now)

	first := testSegment("first", RoleMainline, SourceLocalFile, 1_000)
	s1, err := registry.Prepare(5, SlotA, first)
	if err != nil {
		t.Fatal(err)
	}
	gen1 := s1.Slots[0].Generation

	second := testSegment("second", RoleMainline, SourceLocalFile, 1_000)
	s2, err := registry.Prepare(5, SlotA, second)
	if err != nil {
		t.Fatal(err)
	}
	gen2 := s2.Slots[0].Generation
	if gen2 <= gen1 {
		t.Fatalf("generation did not advance: %d -> %d", gen1, gen2)
	}
	if _, _, err := registry.MarkReady(5, SlotA, first.ID, gen1); err == nil {
		t.Fatal("stale readiness unexpectedly accepted")
	}
	ready, _, err := registry.MarkReady(5, SlotA, second.ID, gen2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Start(5, SlotA, second.ID, ready.Slots[0].Generation); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Observe(5, PlaybackObservation{
		SegmentID: first.ID, Generation: gen1,
		Status: PlaybackProgress, ProgressMS: 500,
	}); err == nil {
		t.Fatal("stale playback unexpectedly accepted")
	}
}

func TestRoomsAreIndependent(t *testing.T) {
	registry := NewRegistry()
	a := prepareReady(t, registry, 1, SlotA, testSegment("r1", RoleMainline, SourceLocalFile, 1_000))
	b := prepareReady(t, registry, 2, SlotA, testSegment("r2", RoleMainline, SourceLocalFile, 1_000))
	if _, err := registry.Start(1, SlotA, "r1", a.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Start(2, SlotA, "r2", b.Generation); err != nil {
		t.Fatal(err)
	}
	s1, _ := registry.Snapshot(1)
	s2, _ := registry.Snapshot(2)
	if s1.Output.Segment == nil || s2.Output.Segment == nil ||
		s1.Output.Segment.ID == s2.Output.Segment.ID || s1.RoomID == s2.RoomID {
		t.Fatalf("rooms leaked state: %#v %#v", s1, s2)
	}
}
