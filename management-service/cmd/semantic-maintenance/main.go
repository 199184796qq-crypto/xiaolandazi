// semantic-maintenance rebuilds one scoped derived index from authoritative
// business rows. The default invocation is read-only; --apply is required to
// write vectors and deactivate stale source versions.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/semantic"
)

func main() {
	tenantID := flag.Int64("tenant", 0, "tenant ID")
	roomID := flag.Int64("room", 0, "room ID for correction index")
	planID := flag.Int64("plan", 0, "plan ID for material or reference index")
	contentType := flag.String("type", "", "correction, reference_answer, or material_chunk")
	apply := flag.Bool("apply", false, "write the rebuilt index and supersede stale documents")
	force := flag.Bool("force", false, "recompute every vector even when text hash matches")
	flag.Parse()

	if *tenantID <= 0 {
		log.Fatal("--tenant must be positive")
	}
	scope := semantic.SyncScope{TenantID: *tenantID, ContentType: *contentType}
	switch *contentType {
	case semantic.ContentTypeCorrection:
		if *roomID <= 0 || *planID != 0 {
			log.Fatal("correction requires --room and no --plan")
		}
		scope.RoomID = *roomID
	case semantic.ContentTypeReferenceAnswer, semantic.ContentTypeMaterialChunk:
		if *planID <= 0 || *roomID != 0 {
			log.Fatal("reference_answer/material_chunk require --plan and no --room")
		}
		scope.PlanID = *planID
	default:
		log.Fatal("--type must be correction, reference_answer, or material_chunk")
	}
	if *force && !*apply {
		log.Fatal("--force requires --apply")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cfg := config.Load()
	store, err := appdb.Open(cfg)
	if err != nil {
		log.Fatalf("open management database: %v", err)
	}
	defer store.Close()

	var documents []semantic.Document
	switch *contentType {
	case semantic.ContentTypeCorrection:
		memories, err := store.ListActiveAgentMemories(ctx, *tenantID, *roomID)
		if err != nil {
			log.Fatalf("list current corrections: %v", err)
		}
		documents = decisionexecutor.CorrectionDocuments(*tenantID, *roomID, memories)
	case semantic.ContentTypeReferenceAnswer:
		references, err := store.ListLiveAgentPlanScriptReferences(ctx, *tenantID, *planID)
		if err != nil {
			log.Fatalf("list current references: %v", err)
		}
		documents = decisionexecutor.ReferenceDocuments(*tenantID, *planID, references)
	case semantic.ContentTypeMaterialChunk:
		scripts, err := store.ListLiveAgentPlanScripts(ctx, *tenantID, *planID)
		if err != nil {
			log.Fatalf("list current materials: %v", err)
		}
		documents = decisionexecutor.MaterialDocuments(*tenantID, *planID, scripts)
	}
	if len(documents) > semantic.MaxSyncDocuments {
		log.Fatalf("current documents=%d exceed scoped sync limit=%d; split or raise the maintenance limit before applying", len(documents), semantic.MaxSyncDocuments)
	}
	if !*apply {
		fmt.Printf("dry run: tenant=%d room=%d plan=%d type=%s current_documents=%d; pass --apply to rebuild\n",
			scope.TenantID, scope.RoomID, scope.PlanID, scope.ContentType, len(documents))
		return
	}
	service := semantic.NewService(semantic.NewFromEnv(), store)
	if !service.Enabled() {
		log.Fatal("embedding is disabled or DASHSCOPE_API_KEY is not configured")
	}
	if *force {
		if err := service.Reindex(ctx, documents); err != nil {
			log.Fatalf("recompute current vectors: %v", err)
		}
	}
	pruned, err := service.Sync(ctx, scope, documents)
	if err != nil {
		log.Fatalf("sync semantic index: %v", err)
	}
	fmt.Printf("semantic index synced: tenant=%d room=%d plan=%d type=%s current_documents=%d superseded=%d model=%s\n",
		scope.TenantID, scope.RoomID, scope.PlanID, scope.ContentType, len(documents), pruned, service.Model())
}
