package httpapi

import (
	"context"
	"errors"
	"log"

	"livecompanion/management/internal/answerflow"
	"livecompanion/management/internal/danmakutest"
	appdb "livecompanion/management/internal/db"
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
		config.AttributeAliases = danmakuAttributeAliases(catalog, out)
	}
	ranker := rerank.NewFromEnv()
	if ranker.Enabled() {
		config.Rerank = ranker.Rerank
	}
	return out, config, ctx.Err()
}

func danmakuAttributeAliases(catalog appdb.FactAttributeCatalog, lib danmakuLibrary) map[string]string {
	names := map[int64]string{}
	for _, record := range lib.Records {
		claim := record.Claim
		key := appdb.NormalizeAttributeKey(claim.Attribute)
		id := catalog.ByName[key]
		if id == 0 {
			id = catalog.Aliases[key]
		}
		name := danmakutest.AttributeName(claim.Topic, lib.Links, lib.Stores)
		if id == 0 {
			key = appdb.NormalizeAttributeKey(name)
			id = catalog.ByName[key]
			if id == 0 {
				id = catalog.Aliases[key]
			}
		}
		if id > 0 && name != "" {
			previous, exists := names[id]
			if exists && previous != name {
				names[id] = ""
			} else if !exists {
				names[id] = name
			}
		}
	}
	aliases := map[string]string{}
	for alias, id := range catalog.Aliases {
		if name := names[id]; name != "" {
			aliases[alias] = name
		}
	}
	for alias, id := range catalog.ByName {
		if name := names[id]; name != "" {
			aliases[alias] = name
		}
	}
	return aliases
}
