package httpapi

import "testing"

func TestRecordingSupplyCheckpointRoundTrip(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	want := recordingSupplyCheckpoint{
		TenantID: 14, RoomID: 29, SessionID: 515, VersionID: 242,
		ProgramID: "program-1", Generation: 3, Round: 2, TabOrder: []int{1, 0}, TabPos: 1, Index: 7,
		Sequence: 41, BlockSeq: 9, PreviousLast: 0,
	}
	if err := saveRecordingSupplyCheckpoint(want); err != nil {
		t.Fatal(err)
	}
	got, ok := loadRecordingSupplyCheckpoint(14, 29, 515)
	if !ok {
		t.Fatal("checkpoint not loaded")
	}
	if got.VersionID != want.VersionID || got.Round != want.Round || got.TabPos != want.TabPos || got.Index != want.Index || got.BlockSeq != want.BlockSeq || got.ProgramID != want.ProgramID || len(got.TabOrder) != 2 || got.TabOrder[0] != 1 {
		t.Fatalf("checkpoint mismatch: %+v", got)
	}
	if !recordingSupplyCheckpointMatches(got, 242, 2) {
		t.Fatal("valid checkpoint rejected")
	}
	if recordingSupplyCheckpointMatches(got, 243, 2) || recordingSupplyCheckpointMatches(got, 242, 3) {
		t.Fatal("checkpoint accepted wrong version/tab count")
	}
	bad := got
	bad.TabOrder = []int{1, 1}
	if recordingSupplyCheckpointMatches(bad, 242, 2) {
		t.Fatal("checkpoint accepted duplicate tab order")
	}
}

func TestRecordingSupplyCheckpointIsSessionScoped(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	if err := saveRecordingSupplyCheckpoint(recordingSupplyCheckpoint{TenantID: 14, RoomID: 29, SessionID: 515, VersionID: 242}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadRecordingSupplyCheckpoint(14, 29, 516); ok {
		t.Fatal("checkpoint leaked into another session")
	}
}
