package main

import (
	"context"
	"flag"
	"log"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/questioncluster"
)

func main() {
	tenantID := flag.Int64("tenant", 0, "tenant id")
	roomID := flag.Int64("room", 0, "room id")
	allSmall := flag.Bool("all-small", false, "include all current-session small Q buckets")
	flag.Parse()

	if *tenantID <= 0 || *roomID <= 0 {
		log.Fatal("tenant and room must be positive")
	}

	cfg := config.Load()
	store, err := appdb.Open(cfg)
	if err != nil {
		log.Fatalf("open management database: %v", err)
	}
	defer store.Close()

	core := coreclient.New(cfg.CoreBaseURL, cfg.CoreToken)
	worker := questioncluster.New(store, core, agentgateway.NewFromEnv())

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := worker.RunRoomOnce(ctx, *tenantID, *roomID, *allSmall); err != nil {
		log.Fatalf("run semantic clustering: %v", err)
	}
	log.Printf("semantic clustering completed tenant=%d room=%d all_small=%t", *tenantID, *roomID, *allSmall)
}
