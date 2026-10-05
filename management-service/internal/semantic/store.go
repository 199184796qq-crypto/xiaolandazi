package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ContentTypeQuestionCluster = "question_cluster"
	ContentTypeCorrection      = "correction"
	ContentTypeReferenceAnswer = "reference_answer"
	ContentTypeMaterialChunk   = "material_chunk"
	ContentTypeRecentSpeech    = "recent_speech"
	ContentTypeStyleOverlay    = "style_overlay_memory"
	MaxSyncDocuments           = 500
)

var ErrDocumentNotFound = errors.New("semantic document not found")

type Document struct {
	TenantID      int64
	RoomID        int64
	PlanID        int64
	ContentType   string
	SourceID      string
	SourceVersion int64
	Status        string
	Text          string
	ExpiresAt     *time.Time
}

type StoredDocument struct {
	ID int64
	Document
	TextHash   string
	Model      string
	Dimensions int
	Vector     []float32
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Lookup struct {
	TenantID      int64
	RoomID        int64
	PlanID        int64
	ContentType   string
	SourceID      string
	SourceVersion int64
	Model         string
	TextHash      string
}

type Query struct {
	TenantID       int64
	RoomID         int64
	PlanID         int64
	ContentType    string
	Model          string
	Since          time.Time
	Limit          int
	CandidateLimit int
	MinScore       float64
}

type Repository interface {
	GetSemanticDocument(context.Context, Lookup) (StoredDocument, error)
	UpsertSemanticDocument(context.Context, StoredDocument) error
	ListSemanticDocuments(context.Context, Query) ([]StoredDocument, error)
}

// SyncScope identifies a complete derived index. After all current source
// documents are resolved, documents no longer present in Keep are superseded.
// This prevents deleted material chunks and old source versions from remaining
// eligible for retrieval.
type SyncScope struct {
	TenantID    int64
	RoomID      int64
	PlanID      int64
	ContentType string
}

type SourceRevision struct {
	SourceID      string
	SourceVersion int64
	Model         string
}

type SyncRepository interface {
	PruneSemanticDocuments(context.Context, SyncScope, []SourceRevision) (int64, error)
}

type Match struct {
	Document StoredDocument
	Score    float64
}

type ContentOperationStats struct {
	ResolveDocuments  uint64
	CacheHits         uint64
	EmbeddedDocuments uint64
	IndexWrites       uint64
	Searches          uint64
	SearchCandidates  uint64
	SearchMatches     uint64
	Retrievals        uint64
	RetrievalHits     uint64
	Failures          uint64
}

type OperationStats struct {
	ByContent map[string]ContentOperationStats
}

type Service struct {
	embedder Embedder
	repo     Repository
	statsMu  sync.Mutex
	stats    map[string]ContentOperationStats
}

func NewService(embedder Embedder, repo Repository) *Service {
	return &Service{embedder: embedder, repo: repo, stats: make(map[string]ContentOperationStats)}
}

func (s *Service) Enabled() bool {
	return s != nil && s.embedder != nil && s.embedder.Enabled() && s.repo != nil
}

func (s *Service) Model() string {
	if s == nil || s.embedder == nil {
		return ""
	}
	return s.embedder.Model()
}

// Stats proxies provider metrics so Service can be wired directly into the
// existing health and Prometheus surfaces.
func (s *Service) Stats() Stats {
	if s == nil || s.embedder == nil {
		return Stats{}
	}
	if provider, ok := s.embedder.(interface{ Stats() Stats }); ok {
		return provider.Stats()
	}
	return Stats{}
}

func (s *Service) OperationStats() OperationStats {
	result := OperationStats{ByContent: make(map[string]ContentOperationStats)}
	if s == nil {
		return result
	}
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	for key, value := range s.stats {
		result.ByContent[key] = value
	}
	return result
}

func (s *Service) record(contentType string, update func(*ContentOperationStats)) {
	if s == nil || update == nil {
		return
	}
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		contentType = "unknown"
	}
	s.statsMu.Lock()
	item := s.stats[contentType]
	update(&item)
	s.stats[contentType] = item
	s.statsMu.Unlock()
}

// RecordRetrieval counts business level recall decisions. SearchMatches only
// counts raw vector matches; a caller may still reject them by scene threshold.
func (s *Service) RecordRetrieval(contentType string, hit bool) {
	s.record(contentType, func(stats *ContentOperationStats) {
		stats.Retrievals++
		if hit {
			stats.RetrievalHits++
		}
	})
}

func (s *Service) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	if len(texts) == 0 {
		return nil, nil
	}
	return s.embedder.Embed(ctx, texts)
}

func HashText(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func EncodeVector(vector []float32) []byte {
	if len(vector) == 0 {
		return nil
	}
	data := make([]byte, len(vector)*4)
	for index, value := range vector {
		binary.LittleEndian.PutUint32(data[index*4:], math.Float32bits(value))
	}
	return data
}

func DecodeVector(data []byte, dimensions int) ([]float32, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("semantic vector bytes=%d not divisible by 4", len(data))
	}
	count := len(data) / 4
	if dimensions > 0 && count != dimensions {
		return nil, fmt.Errorf("semantic vector dimensions=%d want=%d", count, dimensions)
	}
	vector := make([]float32, count)
	for index := range vector {
		vector[index] = math.Float32frombits(binary.LittleEndian.Uint32(data[index*4:]))
	}
	return vector, nil
}

func normalizeDocument(document Document) (Document, error) {
	document.ContentType = strings.TrimSpace(document.ContentType)
	document.SourceID = strings.TrimSpace(document.SourceID)
	document.Status = strings.TrimSpace(document.Status)
	document.Text = strings.TrimSpace(document.Text)
	if document.Status == "" {
		document.Status = "active"
	}
	if document.TenantID <= 0 {
		return Document{}, errors.New("semantic tenant_id is required")
	}
	if document.ContentType == "" {
		return Document{}, errors.New("semantic content_type is required")
	}
	if document.SourceID == "" {
		return Document{}, errors.New("semantic source_id is required")
	}
	if document.Text == "" {
		return Document{}, errors.New("semantic text is required")
	}
	return document, nil
}

func (s *Service) Resolve(ctx context.Context, documents []Document) ([][]float32, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	if len(documents) == 0 {
		return nil, nil
	}
	for _, document := range documents {
		s.record(document.ContentType, func(stats *ContentOperationStats) { stats.ResolveDocuments++ })
	}

	normalized := make([]Document, len(documents))
	hashes := make([]string, len(documents))
	result := make([][]float32, len(documents))
	missingIndexes := make([]int, 0, len(documents))
	missingTexts := make([]string, 0, len(documents))

	for index, document := range documents {
		var err error
		normalized[index], err = normalizeDocument(document)
		if err != nil {
			return nil, err
		}
		hashes[index] = HashText(normalized[index].Text)
		cached, err := s.repo.GetSemanticDocument(ctx, Lookup{
			TenantID:      normalized[index].TenantID,
			RoomID:        normalized[index].RoomID,
			PlanID:        normalized[index].PlanID,
			ContentType:   normalized[index].ContentType,
			SourceID:      normalized[index].SourceID,
			SourceVersion: normalized[index].SourceVersion,
			Model:         s.embedder.Model(),
			TextHash:      hashes[index],
		})
		if err == nil && len(cached.Vector) > 0 {
			result[index] = cached.Vector
			s.record(normalized[index].ContentType, func(stats *ContentOperationStats) { stats.CacheHits++ })
			continue
		}
		if err != nil && !errors.Is(err, ErrDocumentNotFound) {
			s.record(normalized[index].ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
			return nil, err
		}
		missingIndexes = append(missingIndexes, index)
		missingTexts = append(missingTexts, normalized[index].Text)
	}

	if len(missingTexts) == 0 {
		return result, nil
	}
	vectors, err := s.embedder.Embed(ctx, missingTexts)
	if err != nil {
		for _, index := range missingIndexes {
			s.record(normalized[index].ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
		}
		return nil, err
	}
	if len(vectors) != len(missingTexts) {
		return nil, fmt.Errorf("semantic embedding count=%d want=%d", len(vectors), len(missingTexts))
	}
	now := time.Now().UTC()
	for offset, vector := range vectors {
		index := missingIndexes[offset]
		result[index] = vector
		s.record(normalized[index].ContentType, func(stats *ContentOperationStats) { stats.EmbeddedDocuments++ })
		stored := StoredDocument{
			Document:   normalized[index],
			TextHash:   hashes[index],
			Model:      s.embedder.Model(),
			Dimensions: len(vector),
			Vector:     vector,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.repo.UpsertSemanticDocument(ctx, stored); err != nil {
			s.record(normalized[index].ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
			return nil, err
		}
		s.record(normalized[index].ContentType, func(stats *ContentOperationStats) { stats.IndexWrites++ })
	}
	return result, nil
}

func (s *Service) Index(ctx context.Context, document Document) error {
	_, err := s.Resolve(ctx, []Document{document})
	return err
}

// Sync resolves every current document in a scope and then marks every stale
// source/version/model in that same scope as superseded. Resolve happens first,
// so a provider failure never removes the last usable index.
func (s *Service) Sync(ctx context.Context, scope SyncScope, documents []Document) (int64, error) {
	if !s.Enabled() {
		return 0, ErrDisabled
	}
	if scope.TenantID <= 0 || strings.TrimSpace(scope.ContentType) == "" {
		return 0, errors.New("semantic sync scope is incomplete")
	}
	if len(documents) > MaxSyncDocuments {
		return 0, fmt.Errorf("semantic sync documents=%d exceeds %d", len(documents), MaxSyncDocuments)
	}
	for index := range documents {
		document := &documents[index]
		if document.TenantID != scope.TenantID || strings.TrimSpace(document.ContentType) != strings.TrimSpace(scope.ContentType) {
			return 0, errors.New("semantic sync document is outside scope")
		}
		if document.RoomID != scope.RoomID {
			return 0, errors.New("semantic sync document room is outside scope")
		}
		if document.PlanID != scope.PlanID {
			return 0, errors.New("semantic sync document plan is outside scope")
		}
	}
	if len(documents) > 0 {
		if _, err := s.Resolve(ctx, documents); err != nil {
			return 0, err
		}
	}
	repo, ok := s.repo.(SyncRepository)
	if !ok {
		return 0, errors.New("semantic repository does not support source synchronization")
	}
	keep := make([]SourceRevision, 0, len(documents))
	for _, document := range documents {
		keep = append(keep, SourceRevision{
			SourceID:      strings.TrimSpace(document.SourceID),
			SourceVersion: document.SourceVersion,
			Model:         s.embedder.Model(),
		})
	}
	count, err := repo.PruneSemanticDocuments(ctx, scope, keep)
	if err != nil {
		s.record(scope.ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
	}
	return count, err
}

// Reindex intentionally bypasses the text-hash cache. It is used by the
// maintenance command when changing models or repairing persisted vectors.
func (s *Service) Reindex(ctx context.Context, documents []Document) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	if len(documents) == 0 {
		return nil
	}
	normalized := make([]Document, len(documents))
	texts := make([]string, len(documents))
	for index, document := range documents {
		var err error
		normalized[index], err = normalizeDocument(document)
		if err != nil {
			return err
		}
		texts[index] = normalized[index].Text
		s.record(normalized[index].ContentType, func(stats *ContentOperationStats) { stats.ResolveDocuments++ })
	}
	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		for _, document := range normalized {
			s.record(document.ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
		}
		return err
	}
	if len(vectors) != len(normalized) {
		return fmt.Errorf("semantic reindex embedding count=%d want=%d", len(vectors), len(normalized))
	}
	now := time.Now().UTC()
	for index, document := range normalized {
		if err := s.repo.UpsertSemanticDocument(ctx, StoredDocument{
			Document: document, TextHash: HashText(document.Text), Model: s.embedder.Model(),
			Dimensions: len(vectors[index]), Vector: vectors[index], CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			s.record(document.ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
			return err
		}
		s.record(document.ContentType, func(stats *ContentOperationStats) {
			stats.EmbeddedDocuments++
			stats.IndexWrites++
		})
	}
	return nil
}

func (s *Service) Search(ctx context.Context, queryText string, query Query) ([]Match, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	queryText = strings.TrimSpace(queryText)
	if queryText == "" || query.TenantID <= 0 || strings.TrimSpace(query.ContentType) == "" {
		return nil, nil
	}
	// The repository must filter by the active embedding model before applying
	// CandidateLimit. Filtering only after the database LIMIT can starve the
	// active model when rows from a newer/older model are more recent, which is
	// especially likely during a model migration or rollback.
	query.Model = s.embedder.Model()
	s.record(query.ContentType, func(stats *ContentOperationStats) { stats.Searches++ })
	documents, err := s.repo.ListSemanticDocuments(ctx, query)
	if err != nil {
		s.record(query.ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
		return nil, err
	}
	if len(documents) == 0 {
		return nil, nil
	}
	s.record(query.ContentType, func(stats *ContentOperationStats) { stats.SearchCandidates += uint64(len(documents)) })
	queryVector, err := s.embedder.Embed(ctx, []string{queryText})
	if err != nil {
		s.record(query.ContentType, func(stats *ContentOperationStats) { stats.Failures++ })
		return nil, err
	}
	if len(queryVector) != 1 {
		return nil, fmt.Errorf("semantic query embedding count=%d want=1", len(queryVector))
	}
	// Existing deployments may contain old active rows created before source
	// synchronization was introduced. Deduplicate defensively and prefer the
	// highest source version before ranking.
	latest := make(map[string]StoredDocument, len(documents))
	for _, document := range documents {
		if document.Model != s.embedder.Model() || len(document.Vector) == 0 {
			continue
		}
		key := fmt.Sprintf("%d:%d:%s:%s", document.RoomID, document.PlanID, document.ContentType, document.SourceID)
		current, exists := latest[key]
		if !exists || document.SourceVersion > current.SourceVersion ||
			(document.SourceVersion == current.SourceVersion && document.UpdatedAt.After(current.UpdatedAt)) {
			latest[key] = document
		}
	}
	matches := make([]Match, 0, len(latest))
	for _, document := range latest {
		score := CosineSimilarity(queryVector[0], document.Vector)
		if query.MinScore > 0 && score < query.MinScore {
			continue
		}
		matches = append(matches, Match{
			Document: document,
			Score:    score,
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].Document.UpdatedAt.After(matches[j].Document.UpdatedAt)
		}
		return matches[i].Score > matches[j].Score
	})
	if query.Limit > 0 && len(matches) > query.Limit {
		matches = matches[:query.Limit]
	}
	s.record(query.ContentType, func(stats *ContentOperationStats) { stats.SearchMatches += uint64(len(matches)) })
	return matches, nil
}
