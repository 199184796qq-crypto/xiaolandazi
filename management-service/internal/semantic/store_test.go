package semantic

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type fakeRepository struct {
	items     map[string]StoredDocument
	lastQuery Query
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{items: map[string]StoredDocument{}}
}

func repoKey(lookup Lookup) string {
	return fmt.Sprintf("%d|%d|%d|%s|%s|%d|%s", lookup.TenantID, lookup.RoomID, lookup.PlanID,
		lookup.ContentType, lookup.SourceID, lookup.SourceVersion, lookup.Model)
}

func (r *fakeRepository) GetSemanticDocument(_ context.Context, lookup Lookup) (StoredDocument, error) {
	item, ok := r.items[repoKey(lookup)]
	if !ok || item.Status == "superseded" || item.TextHash != lookup.TextHash || item.SourceVersion != lookup.SourceVersion || item.TenantID != lookup.TenantID {
		return StoredDocument{}, ErrDocumentNotFound
	}
	return item, nil
}

func (r *fakeRepository) UpsertSemanticDocument(_ context.Context, item StoredDocument) error {
	r.items[repoKey(Lookup{TenantID: item.TenantID, RoomID: item.RoomID, PlanID: item.PlanID,
		ContentType: item.ContentType, SourceID: item.SourceID, SourceVersion: item.SourceVersion, Model: item.Model})] = item
	return nil
}

func (r *fakeRepository) ListSemanticDocuments(_ context.Context, query Query) ([]StoredDocument, error) {
	r.lastQuery = query
	items := make([]StoredDocument, 0)
	for _, item := range r.items {
		if item.TenantID != query.TenantID || item.ContentType != query.ContentType || item.Status == "superseded" {
			continue
		}
		if query.Model != "" && item.Model != query.Model {
			continue
		}
		if query.RoomID > 0 && item.RoomID != query.RoomID {
			continue
		}
		if query.PlanID > 0 && item.PlanID != query.PlanID {
			continue
		}
		if !query.Since.IsZero() && item.CreatedAt.Before(query.Since) {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *fakeRepository) PruneSemanticDocuments(_ context.Context, scope SyncScope, keep []SourceRevision) (int64, error) {
	wanted := make(map[SourceRevision]bool, len(keep))
	for _, item := range keep {
		wanted[item] = true
	}
	var count int64
	for key, item := range r.items {
		if item.TenantID != scope.TenantID || item.RoomID != scope.RoomID || item.PlanID != scope.PlanID ||
			item.ContentType != scope.ContentType || item.Status == "superseded" {
			continue
		}
		if wanted[SourceRevision{SourceID: item.SourceID, SourceVersion: item.SourceVersion, Model: item.Model}] {
			continue
		}
		item.Status = "superseded"
		r.items[key] = item
		count++
	}
	return count, nil
}

type countingEmbedder struct {
	calls int
	err   error
}

func (e *countingEmbedder) Enabled() bool { return true }
func (e *countingEmbedder) Model() string { return "test-model" }
func (e *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(texts))
	for index, text := range texts {
		if text == "价格" {
			out[index] = []float32{1, 0}
		} else {
			out[index] = []float32{0, 1}
		}
	}
	return out, nil
}

func TestResolvePersistsAndReusesVector(t *testing.T) {
	repo := newFakeRepository()
	embedder := &countingEmbedder{}
	service := NewService(embedder, repo)
	doc := Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeCorrection, SourceID: "memory:1", SourceVersion: 2, Text: "价格"}

	first, err := service.Resolve(context.Background(), []Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Resolve(context.Background(), []Document{doc})
	if err != nil {
		t.Fatal(err)
	}
	if embedder.calls != 1 {
		t.Fatalf("embedding calls=%d want=1", embedder.calls)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cached vectors differ: first=%v second=%v", first, second)
	}
}

func TestResolveDoesNotReuseAcrossTenant(t *testing.T) {
	repo := newFakeRepository()
	embedder := &countingEmbedder{}
	service := NewService(embedder, repo)
	_, _ = service.Resolve(context.Background(), []Document{{TenantID: 7, ContentType: ContentTypeCorrection, SourceID: "same", Text: "价格"}})
	_, _ = service.Resolve(context.Background(), []Document{{TenantID: 8, ContentType: ContentTypeCorrection, SourceID: "same", Text: "价格"}})
	if embedder.calls != 2 {
		t.Fatalf("cross-tenant cache reuse detected, calls=%d", embedder.calls)
	}
}

func TestResolveDoesNotReuseAcrossRoomsInSameTenant(t *testing.T) {
	repo := newFakeRepository()
	embedder := &countingEmbedder{}
	service := NewService(embedder, repo)
	for _, roomID := range []int64{11, 12} {
		if _, err := service.Resolve(context.Background(), []Document{{TenantID: 7, RoomID: roomID,
			ContentType: ContentTypeQuestionCluster, SourceID: "topic:same", Text: "价格"}}); err != nil {
			t.Fatal(err)
		}
	}
	if embedder.calls != 2 {
		t.Fatalf("same-tenant cross-room cache reuse detected, calls=%d", embedder.calls)
	}
}

func TestSyncSupersedesOldChunksWithoutTouchingOtherPlan(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(&countingEmbedder{}, repo)
	ctx := context.Background()
	firstScope := SyncScope{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk}
	otherScope := SyncScope{TenantID: 7, PlanID: 9, ContentType: ContentTypeMaterialChunk}
	first := []Document{
		{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk, SourceID: "script:1:chunk:0", SourceVersion: 1, Text: "价格"},
		{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk, SourceID: "script:1:chunk:1", SourceVersion: 1, Text: "物流"},
	}
	if _, err := service.Sync(ctx, firstScope, first); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(ctx, otherScope, []Document{{TenantID: 7, PlanID: 9, ContentType: ContentTypeMaterialChunk,
		SourceID: "script:2:chunk:0", SourceVersion: 1, Text: "物流"}}); err != nil {
		t.Fatal(err)
	}
	current := []Document{{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk,
		SourceID: "script:1:chunk:0", SourceVersion: 2, Text: "价格"}}
	pruned, err := service.Sync(ctx, firstScope, current)
	if err != nil || pruned != 2 {
		t.Fatalf("pruned=%d err=%v, want 2 old chunk revisions", pruned, err)
	}
	firstRows, _ := repo.ListSemanticDocuments(ctx, Query{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk})
	otherRows, _ := repo.ListSemanticDocuments(ctx, Query{TenantID: 7, PlanID: 9, ContentType: ContentTypeMaterialChunk})
	if len(firstRows) != 1 || firstRows[0].SourceVersion != 2 || len(otherRows) != 1 {
		t.Fatalf("active rows first=%v other=%v", firstRows, otherRows)
	}
}

func TestSyncDoesNotPruneWhenEmbeddingFails(t *testing.T) {
	repo := newFakeRepository()
	embedder := &countingEmbedder{}
	service := NewService(embedder, repo)
	ctx := context.Background()
	scope := SyncScope{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk}
	old := Document{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk,
		SourceID: "script:1:chunk:0", SourceVersion: 1, Text: "价格"}
	if _, err := service.Sync(ctx, scope, []Document{old}); err != nil {
		t.Fatal(err)
	}
	embedder.err = errors.New("provider unavailable")
	current := old
	current.SourceVersion = 2
	if _, err := service.Sync(ctx, scope, []Document{current}); err == nil {
		t.Fatal("expected provider failure")
	}
	rows, err := repo.ListSemanticDocuments(ctx, Query{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk})
	if err != nil || len(rows) != 1 || rows[0].SourceVersion != 1 {
		t.Fatalf("old index must remain after provider failure: rows=%v err=%v", rows, err)
	}
}

func TestSyncRejectsOversizedScopeBeforeEmbedding(t *testing.T) {
	embedder := &countingEmbedder{}
	service := NewService(embedder, newFakeRepository())
	documents := make([]Document, MaxSyncDocuments+1)
	for index := range documents {
		documents[index] = Document{TenantID: 7, PlanID: 8, ContentType: ContentTypeReferenceAnswer,
			SourceID: fmt.Sprintf("reference:%d", index), Text: "价格"}
	}
	_, err := service.Sync(context.Background(), SyncScope{TenantID: 7, PlanID: 8,
		ContentType: ContentTypeReferenceAnswer}, documents)
	if err == nil || embedder.calls != 0 {
		t.Fatalf("oversized scope should fail before embedding: err=%v calls=%d", err, embedder.calls)
	}
}

func TestReindexBypassesHashCache(t *testing.T) {
	repo := newFakeRepository()
	embedder := &countingEmbedder{}
	service := NewService(embedder, repo)
	document := Document{TenantID: 7, PlanID: 8, ContentType: ContentTypeMaterialChunk,
		SourceID: "script:1:chunk:0", SourceVersion: 1, Text: "价格"}
	if err := service.Index(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	if err := service.Reindex(context.Background(), []Document{document}); err != nil {
		t.Fatal(err)
	}
	if embedder.calls != 2 {
		t.Fatalf("force reindex embedding calls=%d want=2", embedder.calls)
	}
}

func TestSearchRanksBeforeApplyingResultLimitAndKeepsLatestVersion(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(&countingEmbedder{}, repo)
	now := time.Now().UTC()
	for _, item := range []StoredDocument{
		{Document: Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, SourceID: "older", SourceVersion: 1, Text: "价格"}, Model: "test-model", Vector: []float32{1, 0}, UpdatedAt: now.Add(-time.Minute)},
		{Document: Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, SourceID: "newer", SourceVersion: 1, Text: "物流"}, Model: "test-model", Vector: []float32{0, 1}, UpdatedAt: now},
		{Document: Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, SourceID: "older", SourceVersion: 2, Text: "价格"}, Model: "test-model", Vector: []float32{1, 0}, UpdatedAt: now},
	} {
		repo.UpsertSemanticDocument(context.Background(), item)
	}
	matches, err := service.Search(context.Background(), "价格", Query{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, Limit: 1})
	if err != nil || len(matches) != 1 || matches[0].Document.SourceID != "older" || matches[0].Document.SourceVersion != 2 {
		t.Fatalf("matches=%v err=%v", matches, err)
	}
}

func TestSearchRanksPersistedDocuments(t *testing.T) {
	repo := newFakeRepository()
	embedder := &countingEmbedder{}
	service := NewService(embedder, repo)
	now := time.Now().UTC()
	repo.items["a"] = StoredDocument{
		Document: Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, SourceID: "a", Text: "价格"},
		Model:    "test-model", Vector: []float32{1, 0}, UpdatedAt: now,
	}
	repo.items["b"] = StoredDocument{
		Document: Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, SourceID: "b", Text: "物流"},
		Model:    "test-model", Vector: []float32{0, 1}, UpdatedAt: now,
	}
	matches, err := service.Search(context.Background(), "价格", Query{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0].Document.SourceID != "a" {
		t.Fatalf("unexpected matches=%v", matches)
	}
}

func TestSearchFiltersModelBeforeRepositoryCandidateLimit(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(&countingEmbedder{}, repo)
	now := time.Now().UTC()
	repo.items["current"] = StoredDocument{
		Document: Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, SourceID: "current", Text: "价格"},
		Model:    "test-model", Vector: []float32{1, 0}, UpdatedAt: now.Add(-time.Hour),
	}
	repo.items["other"] = StoredDocument{
		Document: Document{TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, SourceID: "other", Text: "价格"},
		Model:    "replacement-model", Vector: []float32{1, 0}, UpdatedAt: now,
	}

	matches, err := service.Search(context.Background(), "价格", Query{
		TenantID: 7, RoomID: 11, ContentType: ContentTypeRecentSpeech, CandidateLimit: 1, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastQuery.Model != "test-model" {
		t.Fatalf("repository model filter=%q want=test-model", repo.lastQuery.Model)
	}
	if len(matches) != 1 || matches[0].Document.SourceID != "current" {
		t.Fatalf("current model candidate was starved by another model: %v", matches)
	}
}

func TestDisabledService(t *testing.T) {
	service := NewService(nil, newFakeRepository())
	if service.Enabled() {
		t.Fatal("service must be disabled")
	}
	if _, err := service.Resolve(context.Background(), []Document{{TenantID: 1, ContentType: "x", SourceID: "x", Text: "x"}}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestVectorCodecRoundTrip(t *testing.T) {
	input := []float32{0.25, -0.5, 1}
	decoded, err := DecodeVector(EncodeVector(input), len(input))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, decoded) {
		t.Fatalf("round trip=%v want=%v", decoded, input)
	}
}
