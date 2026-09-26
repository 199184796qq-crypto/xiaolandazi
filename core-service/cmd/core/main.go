package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/capture"
	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/collector/douyin"
	"livecompanion/core/internal/config"
	"livecompanion/core/internal/coordination"
	"livecompanion/core/internal/coreaudio"
	appdb "livecompanion/core/internal/db"
	"livecompanion/core/internal/eventarchive"
	eventstore "livecompanion/core/internal/events"
	"livecompanion/core/internal/httpapi"
	"livecompanion/core/internal/logging"
	"livecompanion/core/internal/media"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/paidpipeline"
	"livecompanion/core/internal/questionqueue"
	"livecompanion/core/internal/rediscache"
	roomstore "livecompanion/core/internal/room"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/speechanalysis"
	"livecompanion/core/internal/speechruntime"
	"livecompanion/core/internal/userblock"
)

func hydrateAgentModes(
	ctx context.Context,
	rooms *roomstore.Store,
	events *eventstore.Store,
	registry *agentwork.Registry,
) error {
	roomItems, err := rooms.List(ctx, nil)
	if err != nil {
		return err
	}
	for _, room := range roomItems {
		mode, modeErr := events.GetAgentMode(ctx, room.ID)
		if modeErr != nil {
			log.Printf("hydrate agent mode room=%d: %v", room.ID, modeErr)
			continue
		}
		if _, modeErr = registry.SetMode(room.ID, agentwork.Mode(mode)); modeErr != nil {
			log.Printf("hydrate agent mode room=%d value=%q: %v", room.ID, mode, modeErr)
		}
	}
	return nil
}

func hydrateRoomBrainState(
	ctx context.Context,
	rooms *roomstore.Store,
	events *eventstore.Store,
	brain *roombrain.Manager,
	blocks *userblock.Store,
	recentLimit int,
	importantLimit int,
) error {
	roomItems, err := rooms.List(ctx, nil)
	if err != nil {
		return err
	}
	for _, room := range roomItems {
		stats, ensureErr := events.EnsureSessionState(ctx, room.ID)
		if ensureErr != nil || stats.StartedAt.IsZero() {
			continue
		}
		brain.Ingest(model.RoomEvent{
			TenantID:   room.TenantID,
			RoomID:     room.ID,
			EventType:  "session_start",
			OccurredAt: stats.StartedAt,
		})
		tenantID := room.TenantID
		merged := map[int64]model.RoomEvent{}
		recent, _ := events.ListRecent(ctx, &tenantID, room.ID, recentLimit)
		for _, event := range recent {
			eventType := strings.ToLower(strings.TrimSpace(event.EventType))
			if eventType == "chat" || eventType == "comment" || eventType == "order_signal" {
				merged[event.ID] = event
			}
		}
		importantChats := []model.RoomEvent{}
		existingOrderSignals := map[int64]struct{}{}
		for _, eventType := range []string{"chat", "order_signal"} {
			items, listErr := events.ListImportant(ctx, &tenantID, room.ID, eventType, 0, importantLimit)
			if listErr != nil {
				continue
			}
			for _, event := range items {
				merged[event.ID] = event
				if eventType == "chat" {
					importantChats = append(importantChats, event)
				} else if sourceID := sourceEventID(event.Payload); sourceID > 0 {
					existingOrderSignals[sourceID] = struct{}{}
				}
			}
		}
		for _, chat := range importantChats {
			if !roombrain.IsOrderSignalText(chat.Content) {
				continue
			}
			if _, exists := existingOrderSignals[chat.ID]; exists {
				continue
			}
			payload, _ := json.Marshal(map[string]any{
				"source_event_id": chat.ID,
				"verified_order":  false,
				"source":          "chat_backfill",
			})
			created, createErr := events.Create(ctx, room.TenantID, room.ID, model.CreateEventInput{
				EventType:  "order_signal",
				UserID:     chat.UserID,
				Nickname:   chat.Nickname,
				Content:    chat.Content,
				OccurredAt: chat.OccurredAt,
				Payload:    payload,
			})
			if createErr != nil {
				log.Printf("room=%d backfill order signal chat=%d: %v", room.ID, chat.ID, createErr)
				continue
			}
			merged[created.ID] = created
			existingOrderSignals[chat.ID] = struct{}{}
		}
		ordered := make([]model.RoomEvent, 0, len(merged))
		for _, event := range merged {
			ordered = append(ordered, event)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
		for _, event := range ordered {
			if blocks != nil && blocks.IsBlocked(event.RoomID, event.UserID, event.Nickname) {
				continue
			}
			brain.Ingest(event)
		}
	}
	return nil
}

func sourceEventID(payload json.RawMessage) int64 {
	if len(payload) == 0 {
		return 0
	}
	var value struct {
		SourceEventID int64 `json:"source_event_id"`
	}
	if json.Unmarshal(payload, &value) != nil {
		return 0
	}
	return value.SourceEventID
}

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
	events := eventstore.NewStoreWithImportantLimit(redisClient, cfg.EventCacheLimit, cfg.ImportantEventCacheLimit)
	archiveWriter := eventarchive.NewWithSpool(database, cfg.EventArchiveSpoolDir)
	archiveWriter.Start(appCtx)
	defer archiveWriter.Close()
	events.SetArchiveSink(archiveWriter.Enqueue)
	hub := eventstore.NewHub()
	brain := roombrain.NewManager()
	questions := questionqueue.New()
	agentDecisions := agentdecision.New()
	agentWork := agentwork.New()
	if err := hydrateAgentModes(startupCtx, rooms, events, agentWork); err != nil {
		log.Printf("hydrate agent modes: %v", err)
	}
	speechState := speechruntime.New()
	userBlocks, err := userblock.NewStore(startupCtx, database)
	if err != nil {
		log.Fatalf("load room user blocks: %v", err)
	}
	if err := hydrateRoomBrainState(startupCtx, rooms, events, brain, userBlocks, cfg.EventCacheLimit, cfg.ImportantEventCacheLimit); err != nil {
		log.Printf("hydrate room brain state: %v", err)
	}
	baseEvents := basepipeline.New(brain, questions, userBlocks)
	paidAgents := paidpipeline.New(agentWork, agentDecisions)
	events.SetObserver(func(event model.RoomEvent) {
		signal := baseEvents.Handle(event)
		paidAgents.Handle(event, signal)
		switch strings.ToLower(strings.TrimSpace(event.EventType)) {
		case "session_start", "session_end":
			speechState.Reset(event.RoomID)
		}
	})

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
	type blockedHitLogState struct {
		last       time.Time
		suppressed uint64
	}
	var blockedHitLogMu sync.Mutex
	blockedHitLogs := make(map[int64]map[string]blockedHitLogState)
	collectorManager.SetEventFilter(func(event model.RoomEvent) bool {
		if !userBlocks.IsBlocked(event.RoomID, event.UserID, event.Nickname) {
			return false
		}

		subject := userblock.SubjectKey(event.UserID, event.Nickname)
		now := time.Now()
		blockedHitLogMu.Lock()
		roomLogs := blockedHitLogs[event.RoomID]
		if roomLogs == nil {
			roomLogs = make(map[string]blockedHitLogState)
			blockedHitLogs[event.RoomID] = roomLogs
		}
		state := roomLogs[subject]
		shouldLog := state.last.IsZero() || now.Sub(state.last) >= 5*time.Second
		suppressed := state.suppressed
		if shouldLog {
			roomLogs[subject] = blockedHitLogState{last: now}
		} else {
			state.suppressed++
			roomLogs[subject] = state
		}
		blockedHitLogMu.Unlock()

		if shouldLog {
			log.Printf(
				"[BLOCKED_HIT] room=%d event=%s subject=%q nickname=%q suppressed_since_last=%d",
				event.RoomID,
				event.EventType,
				subject,
				event.Nickname,
				suppressed,
			)
		}
		return true
	})
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

	captureManager, err := capture.NewManager(
		cfg.FFmpegPath,
		cfg.CaptureRoot,
		collectorManager,
	)
	if err != nil {
		log.Fatalf("create capture manager: %v", err)
	}
	defer captureManager.Close()

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
	audioHub := audiohub.New()
	coreAudioClient, err := coreaudio.New(audioHub, cfg.CorePublicURL, cfg.CoreAudioTestWAVPath)
	if err != nil {
		log.Fatalf("create core audio runtime: %v", err)
	}
	api.SetAudioHub(audioHub)
	api.SetAudioClient(coreAudioClient, cfg.CorePublicURL)
	api.SetCaptureManager(captureManager)
	api.SetSpeechAnalysisManager(speechanalysis.NewManager())
	api.SetRoomBrain(brain)
	api.SetQuestionQueue(questions)
	api.SetAgentDecisionQueue(agentDecisions)
	api.SetAgentWorkRegistry(agentWork)
	api.SetSpeechRuntimeRegistry(speechState)
	api.SetUserBlockStore(userBlocks)

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
