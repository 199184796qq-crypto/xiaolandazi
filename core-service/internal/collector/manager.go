package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/coordination"
	eventstore "livecompanion/core/internal/events"
	"livecompanion/core/internal/model"
	roomstore "livecompanion/core/internal/room"
	"livecompanion/core/internal/roombrain"
)

type Stats struct {
	ActiveRooms         int
	ShardIndex          int
	ShardCount          int
	LeaseEnabled        bool
	LeaseNodeID         string
	EventQueueDepth     int
	EventIngressDropped uint64
	EventPersistDropped uint64
	EventDelivered      uint64
	EventPersisted      uint64
	EventPersistErrors  uint64
	RuntimePending      int
	RuntimeWrites       uint64
	RuntimeErrors       uint64
}

func (m *Manager) ProviderDescriptors() []ProviderDescriptor {
	catalog, ok := m.factory.(ProviderCatalog)
	if !ok {
		return nil
	}
	return catalog.ProviderDescriptors()
}

func (m *Manager) SetEventFilter(filter func(model.RoomEvent) bool) {
	if m == nil || m.eventPipeline == nil {
		return
	}
	m.eventPipeline.SetFilter(filter)
}

type Manager struct {
	rootCtx    context.Context
	rootCancel context.CancelFunc

	rooms                 *roomstore.Store
	events                *eventstore.Store
	hub                   *eventstore.Hub
	factory               Factory
	shardIndex            int
	shardCount            int
	publicEventLogEnabled bool
	leases                *coordination.RoomLeases
	failoverDelay         time.Duration
	eventPipeline         *eventPipeline
	runtimeUpdater        *runtimeUpdater

	mu                sync.Mutex
	sessions          map[int64]context.CancelFunc
	sessionLeases     map[int64]coordination.RoomLease
	missingLeaseSince map[int64]time.Time
	wg                sync.WaitGroup
}

func NewManager(
	rooms *roomstore.Store,
	events *eventstore.Store,
	hub *eventstore.Hub,
	factory Factory,
	shardIndex int,
	shardCount int,
	publicEventLogEnabled bool,
	leases *coordination.RoomLeases,
	failoverDelay time.Duration,
) *Manager {
	rootCtx, rootCancel := context.WithCancel(context.Background())

	if shardCount <= 0 {
		shardCount = 1
	}
	if shardIndex < 0 || shardIndex >= shardCount {
		shardIndex = 0
	}
	if failoverDelay < 0 {
		failoverDelay = 0
	}
	return &Manager{
		rootCtx:               rootCtx,
		rootCancel:            rootCancel,
		rooms:                 rooms,
		events:                events,
		hub:                   hub,
		factory:               factory,
		shardIndex:            shardIndex,
		shardCount:            shardCount,
		publicEventLogEnabled: publicEventLogEnabled,
		leases:                leases,
		failoverDelay:         failoverDelay,
		sessions:              make(map[int64]context.CancelFunc),
		sessionLeases:         make(map[int64]coordination.RoomLease),
		missingLeaseSince:     make(map[int64]time.Time),
		eventPipeline:         newEventPipeline(events, hub, publicEventLogEnabled),
		runtimeUpdater:        newRuntimeUpdater(rooms),
	}
}

func (m *Manager) Resume(ctx context.Context) error {
	rooms, err := m.rooms.List(ctx, nil)
	if err != nil {
		return err
	}

	for _, room := range rooms {
		m.Reconcile(room)
	}
	return nil
}

func (m *Manager) Sync(ctx context.Context) error {
	rooms, err := m.rooms.List(ctx, nil)
	if err != nil {
		return err
	}
	present := make(map[int64]struct{}, len(rooms))
	for _, room := range rooms {
		present[room.ID] = struct{}{}
		m.Reconcile(room)
	}

	m.mu.Lock()
	localIDs := make([]int64, 0, len(m.sessions))
	for roomID := range m.sessions {
		localIDs = append(localIDs, roomID)
	}
	m.mu.Unlock()
	for _, roomID := range localIDs {
		if _, ok := present[roomID]; !ok {
			m.stopLocal(roomID, true)
		}
	}
	return nil
}

func (m *Manager) RunSyncLoop(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.rootCtx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(m.rootCtx, interval)
			if err := m.Sync(ctx); err != nil {
				log.Printf("collector shard sync failed: %v", err)
			}
			cancel()
		}
	}
}

func (m *Manager) Start(room model.Room) {
	m.start(room, nil)
}

func (m *Manager) start(room model.Room, lease *coordination.RoomLease) {
	if !room.MonitorEnabled {
		return
	}

	m.mu.Lock()
	if _, exists := m.sessions[room.ID]; exists {
		m.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(m.rootCtx)
	m.sessions[room.ID] = cancel
	if lease != nil {
		m.sessionLeases[room.ID] = *lease
	}
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		var renewDone chan struct{}
		if lease != nil && m.leases != nil {
			renewDone = make(chan struct{})
			go func() {
				defer close(renewDone)
				m.renewRoomLease(ctx, cancel, *lease)
			}()
		}
		defer func() {
			cancel()
			if renewDone != nil {
				<-renewDone
			}
			if lease != nil && m.leases != nil {
				releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
				if err := m.leases.Release(releaseCtx, *lease); err != nil {
					log.Printf("collector room=%d release lease fence=%d: %v", room.ID, lease.Fence, err)
				}
				releaseCancel()
			}
			m.mu.Lock()
			delete(m.sessions, room.ID)
			delete(m.sessionLeases, room.ID)
			delete(m.missingLeaseSince, room.ID)
			m.mu.Unlock()
			m.runtimeUpdater.ClearRoom(room.ID)
			m.eventPipeline.CloseRoom(room.ID)
		}()

		m.run(ctx, room)
	}()
}

func (m *Manager) Stop(roomID int64) {
	// Preserve the last successful session cache when a user stops monitoring.
	// A future failed reconnect must not erase the previous live-room history.
	m.stopLocal(roomID, false)
}

func (m *Manager) stopLocal(roomID int64, clearEvents bool) {
	m.mu.Lock()
	cancel, ok := m.sessions[roomID]
	m.mu.Unlock()

	if ok {
		cancel()
	}
	if clearEvents {
		m.clearRoomEvents(roomID)
	}
}

func (m *Manager) Reconcile(room model.Room) {
	if !room.MonitorEnabled {
		if m.IsServingRoom(room.ID) {
			m.Stop(room.ID)
			_ = m.rooms.SetStatus(
				context.Background(),
				room.TenantID,
				room.ID,
				"stopped",
			)
		}
		return
	}

	if m.IsServingRoom(room.ID) {
		return
	}

	if m.leases == nil {
		if !m.OwnsRoom(room) {
			m.stopLocal(room.ID, false)
			return
		}
		m.Start(room)
		return
	}

	if !m.OwnsRoom(room) {
		leaseCtx, cancel := context.WithTimeout(m.rootCtx, 2*time.Second)
		_, exists, err := m.leases.Current(
			leaseCtx,
			room.TenantID,
			room.ID,
		)
		cancel()
		if err != nil {
			log.Printf("collector room=%d inspect lease: %v", room.ID, err)
			return
		}
		if exists {
			m.mu.Lock()
			delete(m.missingLeaseSince, room.ID)
			m.mu.Unlock()
			return
		}

		now := time.Now()
		m.mu.Lock()
		missingSince := m.missingLeaseSince[room.ID]
		if missingSince.IsZero() {
			m.missingLeaseSince[room.ID] = now
		}
		m.mu.Unlock()
		if missingSince.IsZero() ||
			now.Sub(missingSince) < m.failoverDelay {
			return
		}
	} else {
		m.mu.Lock()
		delete(m.missingLeaseSince, room.ID)
		m.mu.Unlock()
	}

	leaseCtx, cancel := context.WithTimeout(m.rootCtx, 2*time.Second)
	lease, acquired, err := m.leases.Acquire(
		leaseCtx,
		room.TenantID,
		room.ID,
	)
	cancel()
	if err != nil {
		log.Printf("collector room=%d acquire lease: %v", room.ID, err)
		return
	}
	if !acquired {
		return
	}
	log.Printf(
		"collector room=%d lease acquired owner=%s fence=%d preferred=%t",
		room.ID,
		lease.Owner,
		lease.Fence,
		m.OwnsRoom(room),
	)
	m.start(room, &lease)
}

func (m *Manager) OwnsRoom(room model.Room) bool {
	if m.shardCount <= 1 {
		return true
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%d", room.TenantID, room.ID)
	return int(h.Sum64()%uint64(m.shardCount)) == m.shardIndex
}

func (m *Manager) IsServingRoom(roomID int64) bool {
	m.mu.Lock()
	_, ok := m.sessions[roomID]
	m.mu.Unlock()
	return ok
}

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	active := len(m.sessions)
	m.mu.Unlock()
	stats := Stats{
		ActiveRooms:  active,
		ShardIndex:   m.shardIndex,
		ShardCount:   m.shardCount,
		LeaseEnabled: m.leases != nil,
	}
	if m.leases != nil {
		stats.LeaseNodeID = m.leases.NodeID()
	}
	if m.eventPipeline != nil {
		pipelineStats := m.eventPipeline.Stats()
		stats.EventQueueDepth = pipelineStats.QueueDepth
		stats.EventIngressDropped = pipelineStats.IngressDropped
		stats.EventPersistDropped = pipelineStats.PersistDropped
		stats.EventDelivered = pipelineStats.Delivered
		stats.EventPersisted = pipelineStats.Persisted
		stats.EventPersistErrors = pipelineStats.PersistErrors
	}
	if m.runtimeUpdater != nil {
		runtimeStats := m.runtimeUpdater.Stats()
		stats.RuntimePending = runtimeStats.Pending
		stats.RuntimeWrites = runtimeStats.Writes
		stats.RuntimeErrors = runtimeStats.Errors
	}
	return stats
}

func (m *Manager) renewRoomLease(
	ctx context.Context,
	cancel context.CancelFunc,
	lease coordination.RoomLease,
) {
	interval := m.leases.RenewInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var failureSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, renewCancel := context.WithTimeout(ctx, 2*time.Second)
			ok, err := m.leases.Renew(renewCtx, lease)
			renewCancel()
			if err == nil && ok {
				failureSince = time.Time{}
				continue
			}
			if err == nil && !ok {
				log.Printf(
					"collector room=%d lease lost fence=%d",
					lease.RoomID,
					lease.Fence,
				)
				cancel()
				return
			}
			log.Printf(
				"collector room=%d lease renew failed fence=%d: %v",
				lease.RoomID,
				lease.Fence,
				err,
			)
			if failureSince.IsZero() {
				failureSince = time.Now()
				continue
			}
			maxFailure := m.leases.TTL() - interval
			if maxFailure < interval {
				maxFailure = interval
			}
			if time.Since(failureSince) >= maxFailure {
				log.Printf(
					"collector room=%d stopping before lease expiry fence=%d",
					lease.RoomID,
					lease.Fence,
				)
				cancel()
				return
			}
		}
	}
}

func (m *Manager) Stream(
	ctx context.Context,
	room model.Room,
) (StreamSource, error) {
	provider, ok := m.factory.(StreamProvider)
	if !ok {
		return StreamSource{}, errors.New("collector stream source is not supported")
	}
	return provider.Stream(ctx, room)
}
func (m *Manager) Preview(
	ctx context.Context,
	room model.Room,
) ([]byte, string, error) {
	provider, ok := m.factory.(PreviewProvider)
	if !ok {
		return nil, "", errors.New("collector preview is not supported")
	}
	return provider.Preview(ctx, room)
}
func (m *Manager) Close() {
	m.rootCancel()

	m.mu.Lock()
	for _, cancel := range m.sessions {
		cancel()
	}
	m.mu.Unlock()

	// Do not clear room events on process shutdown. Redis keeps the last
	// successful live session available for the next UI/backend start.
	m.wg.Wait()
	if m.eventPipeline != nil {
		m.eventPipeline.Close()
	}
	if m.runtimeUpdater != nil {
		m.runtimeUpdater.Close()
	}
}

func (m *Manager) run(ctx context.Context, room model.Room) {
	backoff := 5 * time.Second
	currentPlatformRoomID, hasCachedEvents := m.cachedPlatformRoomID(room.ID)
	sessionEnded := false

	for {
		if ctx.Err() != nil {
			return
		}

		if err := m.rooms.SetStatus(context.Background(), room.TenantID, room.ID, "connecting"); err != nil {
			log.Printf("collector room=%d set connecting: %v", room.ID, err)
			return
		}

		runner, err := m.factory.Create(room)
		if err != nil {
			_ = m.rooms.SetStatus(context.Background(), room.TenantID, room.ID, "error")
			log.Printf("collector room=%d create runner: %v", room.ID, err)
			return
		}
		attemptLive := false
		live := func(_ context.Context) error {
			attemptLive = true
			m.runtimeUpdater.Touch(room)
			return nil
		}

		ensureSession := func(input model.CreateEventInput) {
			platformRoomID := platformRoomIDFromPayload(input.Payload)
			if platformRoomID == "" {
				return
			}
			if platformRoomID == currentPlatformRoomID && !sessionEnded {
				return
			}

			// First confirmed broadcast starts a fresh logical session. Any later
			// return from offline is deliberately marked as reopen-pending so the
			// user can choose to merge it with the previous broadcast or start a
			// fresh active runtime. We keep cached raw events for review either way.
			eventType := "session_start"
			if hasCachedEvents {
				eventType = "session_reopen"
			}
			m.eventPipeline.Enqueue(room, model.CreateEventInput{
				EventType:  eventType,
				OccurredAt: time.Now().UTC(),
			}, true, false)
			currentPlatformRoomID = platformRoomID
			hasCachedEvents = true
			sessionEnded = false
		}

		emit := func(_ context.Context, input model.CreateEventInput) error {
			ensureSession(input)
			if input.EventType == "room" {
				m.applyRoomMetrics(room, input)
				m.eventPipeline.Enqueue(room, input, false, false)
				return nil
			}

			event, accepted := m.eventPipeline.Enqueue(room, input, true, true)
			if accepted {
				m.runtimeUpdater.Touch(room)
				if (strings.EqualFold(input.EventType, "chat") || strings.EqualFold(input.EventType, "comment")) && roombrain.IsOrderSignalText(input.Content) {
					payload, _ := json.Marshal(map[string]any{
						"source_event_id": event.ID,
						"verified_order":  false,
						"source":          "user_chat",
					})
					m.eventPipeline.Enqueue(room, model.CreateEventInput{
						EventType:  "order_signal",
						UserID:     input.UserID,
						Nickname:   input.Nickname,
						Content:    input.Content,
						OccurredAt: event.OccurredAt,
						Payload:    payload,
					}, true, true)
				}
			}
			return nil
		}

		err = runner.Run(ctx, room, live, emit)
		if errors.Is(err, ErrOffline) && attemptLive && currentPlatformRoomID != "" {
			m.eventPipeline.Enqueue(room, model.CreateEventInput{
				EventType:  "session_end",
				OccurredAt: time.Now().UTC(),
			}, false, false)
			sessionEnded = true
		}
		m.eventPipeline.CloseRoom(room.ID)
		// Drop any coalesced live/online write before publishing an offline/error
		// state. Otherwise a delayed cloud-DB flush could resurrect a stopped run.
		m.runtimeUpdater.ClearRoom(room.ID)
		// Keep the last successful session cache. It is only replaced after a
		// future real event proves that Douyin's platform room_id has changed.
		if ctx.Err() != nil {
			return
		}

		if errors.Is(err, ErrOffline) {
			_ = m.rooms.SetStatus(context.Background(), room.TenantID, room.ID, "offline")
			if err != nil {
				log.Printf("collector room=%d runner=%s offline: %v", room.ID, runner.Name(), err)
			}
			backoff = 30 * time.Second
		} else {
			_ = m.rooms.SetStatus(context.Background(), room.TenantID, room.ID, "error")
			if err != nil {
				log.Printf("collector room=%d runner=%s error: %v", room.ID, runner.Name(), err)
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func platformRoomIDFromPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var value struct {
		RoomID json.RawMessage `json:"room_id"`
	}
	if err := json.Unmarshal(payload, &value); err != nil || len(value.RoomID) == 0 {
		return ""
	}
	roomID := strings.Trim(strings.TrimSpace(string(value.RoomID)), "\"")
	if roomID == "" || roomID == "0" || strings.EqualFold(roomID, "null") {
		return ""
	}
	return roomID
}

func (m *Manager) cachedPlatformRoomID(roomID int64) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	items, err := m.events.ListRecent(ctx, nil, roomID, 100)
	if err != nil {
		log.Printf("collector room=%d inspect cached session: %v", roomID, err)
		return "", false
	}
	for _, item := range items {
		if platformRoomID := platformRoomIDFromPayload(item.Payload); platformRoomID != "" {
			return platformRoomID, true
		}
	}
	return "", len(items) > 0
}

func (m *Manager) clearRoomEvents(roomID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := m.events.ClearRoom(ctx, roomID); err != nil {
		log.Printf("collector room=%d clear redis events: %v", roomID, err)
	}
}
func logPublicEvent(room model.Room, event model.RoomEvent) {
	log.Printf(
		"[PUBLIC] room=%d web_rid=%s event=%s user=%q content=%q event_id=%d",
		room.ID,
		room.ExternalRoomID,
		publicEventLabel(event.EventType),
		sanitizeLogText(event.Nickname, 80),
		sanitizeLogText(event.Content, 240),
		event.ID,
	)
}

func publicEventLabel(eventType string) string {
	switch eventType {
	case "chat":
		return "弹幕"
	case "member":
		return "进房"
	case "like":
		return "点赞"
	case "follow":
		return "关注"
	case "gift":
		return "礼物"
	default:
		return eventType
	}
}

func sanitizeLogText(value string, maxRunes int) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.TrimSpace(value)

	runes := []rune(value)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return value
}
func (m *Manager) applyRoomMetrics(
	room model.Room,
	input model.CreateEventInput,
) {
	if len(input.Payload) == 0 {
		return
	}

	var payload struct {
		OnlineCount uint64 `json:"online_count"`
	}
	if err := json.Unmarshal(input.Payload, &payload); err != nil {
		return
	}
	if payload.OnlineCount == 0 {
		return
	}

	m.runtimeUpdater.SetOnlineCount(room, payload.OnlineCount)
}
