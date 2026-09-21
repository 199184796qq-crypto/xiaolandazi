package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/collector/douyin"
	"livecompanion/core/internal/config"
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

	closeLog, err := logging.Configure("core", cfg.LogFile)
	if err != nil {
		log.Fatalf("configure logging: %v", err)
	}
	defer closeLog()

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

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer startupCancel()

	if err := appdb.Migrate(startupCtx, database); err != nil {
		log.Fatalf("migrate database: %v", err)
	}
	redisClient, err := rediscache.Open(cfg)
	if err != nil {
		log.Fatalf("open redis: %v", err)
	}
	defer redisClient.Close()

	rooms := roomstore.NewStore(database)
	events := eventstore.NewStore(redisClient, cfg.EventCacheLimit)
	hub := eventstore.NewHub()

	browser := douyin.NewBrowserManager(cfg.BrowserPath, cfg.BrowserHeadless)
	defer browser.Close()

	collectorManager := collector.NewManager(
		rooms,
		events,
		hub,
		douyin.NewFactory(browser),
	)
	defer collectorManager.Close()

	mediaManager, err := media.NewManager(
		cfg.FFmpegPath,
		cfg.MediaCacheRoot,
		collectorManager,
	)
	if err != nil {
		log.Fatalf("create media manager: %v", err)
	}
	defer mediaManager.Close()

	if err := collectorManager.Resume(startupCtx); err != nil {
		log.Fatalf("resume collectors: %v", err)
	}

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
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
