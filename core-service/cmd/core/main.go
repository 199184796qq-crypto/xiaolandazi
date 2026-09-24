package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/collector/douyin"
	"livecompanion/core/internal/config"
	"livecompanion/core/internal/coordination"
	appdb "livecompanion/core/internal/db"
	eventstore "livecompanion/core/internal/events"
	"livecompanion/core/internal/httpapi"
	"livecompanion/core/internal/logging"
	"livecompanion/core/internal/media"
	"livecompanion/core/internal/rediscache"
	roomstore "livecompanion/core/internal/room"
)

func main() {
	cfg := config.Load()
	if cfg.ShardCount <= 0 || cfg.ShardIndex < 0 || cfg.ShardIndex >= cfg.ShardCount {
		log.Fatalf("invalid core shard config index=%d count=%d", cfg.ShardIndex, cfg.ShardCount)
	}

	closeLog, err := logging.Configure("core", cfg.LogFile)
	if err != nil {
		log.Fatalf("configure logging: %v", err)
	}
	defer closeLog()

	appCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	log.Printf(
		"starting core-service env=%s addr=%s log_file=%q",
		cfg.Env,
		cfg.Addr,
		cfg.LogFile,
	)

	database, err := appdb.Open(cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	startupCtx, startupCancel := context.WithTimeout(appCtx, 20*time.Second)
	defer startupCancel()

	if err := appdb.Migrate(startupCtx, database); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	redisClient, err := rediscache.Open(cfg)
	if err != nil {
		log.Fatalf("open redis: %v", err)
	}
	defer redisClient.Close()

	var roomLeases *coordination.RoomLeases
	if cfg.RoomLeaseEnabled {
		roomLeases, err = coordination.NewRoomLeases(
			redisClient,
			cfg.NodeID,
			time.Duration(cfg.RoomLeaseTTLSeconds)*time.Second,
			time.Duration(cfg.RoomLeaseRenewSeconds)*time.Second,
		)
		if err != nil {
			log.Fatalf("create room lease coordinator: %v", err)
		}
	}

	rooms := roomstore.NewStore(database)
	events := eventstore.NewStore(redisClient, cfg.EventCacheLimit)
	hub := eventstore.NewHub()

	browser := douyin.NewBrowserPool(
		cfg.CollectorWorkers,
		cfg.CollectorRoomsPerWorker,
		cfg.BrowserPath,
		cfg.BrowserHeadless,
	)
	defer browser.Close()
	collectorRegistry, err := collector.NewRegistry(
		douyin.NewFactory(browser),
	)
	if err != nil {
		log.Fatalf("create collector provider registry: %v", err)
	}
	for _, provider := range collectorRegistry.ProviderDescriptors() {
		log.Printf(
			"collector provider id=%s platform=%s modes=%v priority=%d capabilities=%v",
			provider.ID,
			provider.Platform,
			provider.Modes,
			provider.Priority,
			provider.Capabilities,
		)
	}
	log.Printf(
		"collector pool workers=%d rooms_per_worker=%d capacity=%d shard=%d/%d node=%s leases=%t",
		cfg.CollectorWorkers,
		cfg.CollectorRoomsPerWorker,
		browser.Capacity(),
		cfg.ShardIndex,
		cfg.ShardCount,
		cfg.NodeID,
		roomLeases != nil,
	)

	collectorManager := collector.NewManager(
		rooms,
		events,
		hub,
		collectorRegistry,
		cfg.ShardIndex,
		cfg.ShardCount,
		cfg.PublicEventLogEnabled,
		roomLeases,
		time.Duration(cfg.RoomFailoverDelaySeconds)*time.Second,
	)
	defer collectorManager.Close()

	mediaManager, err := media.NewManager(
		cfg.FFmpegPath,
		cfg.MediaCacheRoot,
		collectorManager,
		cfg.MediaMaxSessions,
	)
	if err != nil {
		log.Fatalf("create media manager: %v", err)
	}
	defer mediaManager.Close()

	if err := collectorManager.Resume(startupCtx); err != nil {
		log.Fatalf("resume collectors: %v", err)
	}
	go collectorManager.RunSyncLoop(5 * time.Second)

	api := httpapi.New(
		rooms,
		events,
		hub,
		collectorManager,
		mediaManager,
		cfg.Env,
		cfg.InternalToken,
	)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("core-service listening on %s", cfg.Addr)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-appCtx.Done():
		log.Printf("core-service shutdown requested")
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("core-service stopped unexpectedly: %v", err)
		}
	}
	stop()

	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("core-service graceful shutdown: %v", err)
	}
}
