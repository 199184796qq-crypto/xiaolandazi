package httpapi

import (
	"context"
	"errors"
	"log"

	"livecompanion/management/internal/answerflow"
	"livecompanion/management/internal/rerank"
)

func (s *Server) loadDanmakuRetrieval(ctx context.Context, tenant, plan int64) (danmakuLibrary, answerflow.RetrievalConfig, error) {
	snapshot, err := s.store.PlanAnswerSnapshot(ctx, tenant, plan)
	if err != nil {
		return danmakuLibrary{}, answerflow.RetrievalConfig{}, err
	}
	out := danmakuLibraryFromSnapshot(snapshot)
	out.Validate = func(ctx context.Context) error {
		current, err := s.store.PlanAnswerSnapshot(ctx, tenant, plan)
		if err != nil {
			return errors.New("读取回答资料状态失败，本次不能确认依据")
		}
		if current.Revision != snapshot.Revision {
			return errors.New("资料、对象、语境词或问答已变化，请重新试答")
		}
		return nil
	}
	config := answerflow.RetrievalConfig{GlobalName: snapshot.Objects.Directory.GlobalDisplayName,
		AttributeAliases: map[string]string{}, Concepts: map[string]answerflow.AttributeConcept{}}
	vectors, vectorErr := s.store.PlanAnswerAttributeVectors(ctx, tenant, plan)
	if vectorErr != nil {
		log.Printf("danmaku test attribute vectors unavailable tenant=%d plan=%d", tenant, plan)
	}
	for name, item := range vectors {
		config.Concepts[name] = answerflow.AttributeConcept{Hash: item.Hash, Model: item.Model, Vector: item.Vector}
	}

	catalog, err := s.store.LoadFactAttributeCatalog(ctx)
	if err != nil {
		log.Printf("danmaku test attribute aliases unavailable tenant=%d plan=%d", tenant, plan)
	} else {
		config.AttributeAliases = catalog.CanonicalNames()
	}
	ranker := rerank.NewFromEnv()
	if ranker.Enabled() {
		config.Rerank = ranker.Rerank
	}
	return out, config, ctx.Err()
}
