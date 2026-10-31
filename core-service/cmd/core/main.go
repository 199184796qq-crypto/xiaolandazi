package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	"livecompanion/core/internal/contentblock"
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
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/semantic"
	"livecompanion/core/internal/speechanalysis"
	"livecompanion/core/internal/speechruntime"
	"livecompanion/core/internal/strategycenter"
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
	contentBlocks, err := contentblock.NewStore(startupCtx, database)
	if err != nil {
		log.Fatalf("load room content blocks: %v", err)
	}
	if err := hydrateRoomBrainState(startupCtx, rooms, events, brain, userBlocks, cfg.EventCacheLimit, cfg.ImportantEventCacheLimit); err != nil {
		log.Printf("hydrate room brain state: %v", err)
	}
	baseEvents := basepipeline.New(brain, questions, userBlocks)
	strategyPolicies := strategycenter.New()
	if err := strategyPolicies.Load(startupCtx, database); err != nil {
		log.Printf("hydrate live strategy policies: %v", err)
	}
	semanticEmbedder := semantic.NewFromEnv()
	paidAgents := paidpipeline.New(agentWork, agentDecisions, strategyPolicies)
	paidAgents.SetBrain(brain)
	paidAgents.SetSemanticEmbedder(semanticEmbedder)
	paidAgents.SetSessions(events)
	if existingRooms, listErr := rooms.List(startupCtx, nil); listErr == nil {
		for _, existingRoom := range existingRooms {
			paidAgents.RefreshInteractionStats(existingRoom.ID)
		}
	} else {
		log.Printf("initialize interaction strategy stats: %v", listErr)
	}
	go paidAgents.Run(appCtx)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-appCtx.Done():
				return
			case <-ticker.C:
				if removed := agentDecisions.Sweep(); removed > 0 {
					log.Printf("agent decision lifecycle sweep removed=%d", removed)
				}
			}
		}
	}()
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
	// 被屏蔽的用户与内容在采集入口就丢弃：不入库、不推送 SSE、不进互动与聚类。
	// 读取侧过滤仍然保留，用于历史里已经存下来的事件。
	logBlockedHit := func(event model.RoomEvent, key, code string) {
		now := time.Now()
		blockedHitLogMu.Lock()
		roomLogs := blockedHitLogs[event.RoomID]
		if roomLogs == nil {
			roomLogs = make(map[string]blockedHitLogState)
			blockedHitLogs[event.RoomID] = roomLogs
		}
		state := roomLogs[key]
		shouldLog := state.last.IsZero() || now.Sub(state.last) >= 5*time.Second
		suppressed := state.suppressed
		if shouldLog {
			roomLogs[key] = blockedHitLogState{last: now}
		} else {
			state.suppressed++
			roomLogs[key] = state
		}
		blockedHitLogMu.Unlock()

		if shouldLog {
			log.Printf(
				"%s room=%d event=%s subject=%q nickname=%q suppressed_since_last=%d",
				code,
				event.RoomID,
				event.EventType,
				key,
				event.Nickname,
				suppressed,
			)
		}
	}
	collectorManager.SetEventFilter(func(event model.RoomEvent) bool {
		if userBlocks.IsBlocked(event.RoomID, event.UserID, event.Nickname) {
			logBlockedHit(event, userblock.SubjectKey(event.UserID, event.Nickname), "[BLOCKED_HIT]")
			return true
		}
		if contentBlocks.IsBlocked(event.RoomID, event.Content) {
			key := contentblock.ContentKey(event.Content)
			if len(key) > 12 {
				key = key[:12]
			}
			logBlockedHit(event, "content:"+key, "[BLOCKED_CONTENT_HIT]")
			return true
		}
		return false
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
	archiveStore, archiveErr := capture.NewOSSArchive(cfg.OSSEndpoint, cfg.OSSPublicEndpoint, cfg.OSSBucket, cfg.OSSAccessKeyID, cfg.OSSAccessKeySecret)
	var recordingArchive capture.ArchiveStore
	if archiveErr != nil {
		// Do not log SDK errors which may contain credential-bearing URLs.
		log.Print("recording OSS archive unavailable; check OSS configuration")
	} else {
		recordingArchive = archiveStore
	}
	archiveRooms, err := rooms.List(startupCtx, nil)
	if err != nil {
		log.Fatalf("list capture archive rooms: %v", err)
	}
	if err := captureManager.ConfigureArchives(recordingArchive, archiveRooms); err != nil {
		log.Fatalf("restore capture archives: %v", err)
	}
	go captureManager.RunArchives(appCtx)

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
	roomAudioEngine := roomaudio.New()
	roomAudioEngine.SetSpokenObserver(paidAgents.ObserveSpoken)
	coreAudioClient, err := coreaudio.New(audioHub, cfg.CorePublicURL, cfg.CoreAudioTestWAVPath)
	if err != nil {
		log.Fatalf("create core audio runtime: %v", err)
	}
	coreAudioClient.SetRoomAudioEngine(roomAudioEngine)
	paidAgents.SetAudioStartedReader(func(roomID int64) time.Time {
		snapshot, err := coreAudioClient.ProgramSnapshot(context.Background(), roomID)
		if err != nil {
			return time.Time{}
		}
		return snapshot.ActualStartedAt
	})
	paidAgents.SetWarmupEligibility(func(roomID int64) bool {
		if !collectorManager.IsServingRoom(roomID) {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		room, err := rooms.Get(ctx, nil, roomID)
		return err == nil && strings.EqualFold(room.Status, "live")
	})
	paidAgents.SetTenantResolver(func(roomID int64) (int64, bool) {
		room, err := rooms.Get(context.Background(), nil, roomID)
		return room.TenantID, err == nil
	})
	paidAgents.SetContextReader(func(roomID int64) (string, string) {
		program, err := coreAudioClient.ProgramSnapshot(context.Background(), roomID)
		if err != nil {
			return "", ""
		}
		text := program.TrackText
		if program.CurrentSegment != nil {
			text = program.CurrentSegment.Text
		}
		return text, program.TrackID
	})
	coreAudioClient.SetMainlineAgentSignalProvider(func(roomID int64) coreaudio.MainlineAgentSignals {
		view, viewErr := brain.Snapshot(roomID)
		if viewErr != nil {
			return coreaudio.MainlineAgentSignals{}
		}
		topics := make([]string, 0, len(view.Intelligence.TopTopics))
		for _, topic := range view.Intelligence.TopTopics {
			topics = append(topics, topic.Topic)
		}
		return coreaudio.MainlineAgentSignals{
			Heat:                view.Intelligence.Heat,
			Progress:            view.Director.Progress,
			Atmosphere:          view.Director.Atmosphere,
			QuestionPressure:    view.Intelligence.QuestionPressure,
			Orders30s:           view.Intelligence.Orders30s,
			OrderSignals30s:     view.Intelligence.OrderSignals30s,
			NegativeFeedback30s: view.Intelligence.NegativeFeedback30s,
			TopTopics:           topics,
		}
	})
	api.SetAudioHub(audioHub)
	api.SetRoomAudioEngine(roomAudioEngine)
	api.SetSemanticEmbedder(semanticEmbedder)
	if semanticEmbedder.Enabled() {
		log.Printf("semantic embedding enabled model=%s", semanticEmbedder.Model())
	} else {
		log.Printf("semantic embedding disabled")
	}
	api.SetStrategyPolicyStore(strategyPolicies)
	api.SetAudioClient(coreAudioClient, cfg.CorePublicURL)
	brain.RegisterPurposeMessages(api.MessageCaptain(), func(roomID int64) string {
		r := agentWork.Get(roomID)
		return fmt.Sprintf("%s:%d:%d", r.BootID, r.PlanID, r.RunGeneration)
	})
	paidAgents.SetMessageCaptain(api.MessageCaptain())
	managementURL := strings.TrimSpace(os.Getenv("MANAGEMENT_INTERNAL_URL"))
	if managementURL == "" {
		managementURL = "http://127.0.0.1:8080"
	}
	paidAgents.SetPurposeResolver(paidpipeline.HTTPPurposeResolver{URL: managementURL, Token: cfg.InternalToken})
	api.SetCaptureManager(captureManager)
	api.SetSpeechAnalysisManager(speechanalysis.NewManager())
	api.SetRoomBrain(brain)
	api.SetQuestionQueue(questions)
	api.SetAgentDecisionQueue(agentDecisions)
	api.SetAgentWorkRegistry(agentWork)
	api.SetSpeechRuntimeRegistry(speechState)
	api.SetUserBlockStore(userBlocks)
	api.SetContentBlockStore(contentBlocks)
	go api.RunAgentRuntimeWatch(appCtx, time.Second)

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
