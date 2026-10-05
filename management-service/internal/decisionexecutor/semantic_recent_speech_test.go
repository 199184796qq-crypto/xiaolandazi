package decisionexecutor

import (
	"context"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
	"livecompanion/management/internal/speechmission"
)

type playbackTestEmbedder struct{}

func (playbackTestEmbedder) Enabled() bool { return true }
func (playbackTestEmbedder) Model() string { return "test-model" }
func (playbackTestEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for index := range vectors {
		vectors[index] = []float32{1, 0}
	}
	return vectors, nil
}

type playbackTestRepository struct {
	items []semantic.StoredDocument
}

func (r *playbackTestRepository) GetSemanticDocument(context.Context, semantic.Lookup) (semantic.StoredDocument, error) {
	return semantic.StoredDocument{}, semantic.ErrDocumentNotFound
}
func (r *playbackTestRepository) UpsertSemanticDocument(_ context.Context, item semantic.StoredDocument) error {
	r.items = append(r.items, item)
	return nil
}
func (r *playbackTestRepository) ListSemanticDocuments(_ context.Context, _ semantic.Query) ([]semantic.StoredDocument, error) {
	return append([]semantic.StoredDocument(nil), r.items...), nil
}

func TestRecentSpeechIndexesOnlyAfterCoreConfirmsPlayback(t *testing.T) {
	repo := &playbackTestRepository{}
	service := semantic.NewService(playbackTestEmbedder{}, repo)
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
	worker := New(readyVoiceStore(), &fakeCore{runtimeRaw: `{"interrupt":{"status":"failed","decision_id":"failed","mission_id":"failed"}}`}, &fakeAgent{}, &fakeTTS{})
	worker.SetSemanticService(service)
	worker.now = func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
	worker.missions.Ensure(speechmission.Mission{
		ID: "failed", DecisionID: "failed", TenantID: 7, RoomID: 11, RuntimeSessionID: 9,
		State: speechmission.StateDispatched, GeneratedText: "这段音频没有播出",
	})
	worker.reconcileRoomMission(context.Background(), session)
	if len(repo.items) != 0 {
		t.Fatalf("failed playback indexed %d items", len(repo.items))
	}

	worker.core = &fakeCore{runtimeRaw: `{"interrupt":{"status":"completed","decision_id":"played","mission_id":"played"}}`}
	worker.missions.Ensure(speechmission.Mission{
		ID: "played", DecisionID: "played", TenantID: 7, RoomID: 11, RuntimeSessionID: 9,
		State: speechmission.StateDispatched, GeneratedText: "这段音频已完整播出",
	})
	worker.reconcileRoomMission(context.Background(), session)
	if len(repo.items) != 1 || repo.items[0].Text != "这段音频已完整播出" {
		t.Fatalf("completed playback index=%v", repo.items)
	}
	worker.reconcileRoomMission(context.Background(), session)
	if len(repo.items) != 1 {
		t.Fatalf("completed playback indexed more than once: %d", len(repo.items))
	}
}
