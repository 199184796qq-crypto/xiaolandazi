package collector

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	eventstore "livecompanion/core/internal/events"
	"livecompanion/core/internal/model"
	roomstore "livecompanion/core/internal/room"
)

type Manager struct {
	rootCtx    context.Context
	rootCancel context.CancelFunc

	rooms   *roomstore.Store
	events  *eventstore.Store
	hub     *eventstore.Hub
	factory Factory

	mu       sync.Mutex
	sessions map[int64]context.CancelFunc
	wg       sync.WaitGroup
}

func NewManager(
	rooms *roomstore.Store,
	events *eventstore.Store,
	hub *eventstore.Hub,
	factory Factory,
) *Manager {
	rootCtx, rootCancel := context.WithCancel(context.Background())

	return &Manager{
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
		rooms:      rooms,
		events:     events,
		hub:        hub,
		factory:    factory,
		sessions:   make(map[int64]context.CancelFunc),
	}
}

func (m *Manager) Resume(ctx context.Context) error {
	rooms, err := m.rooms.List(ctx, nil)
	if err != nil {
		return err
	}

	for _, room := range rooms {
		m.clearRoomEvents(room.ID)
		m.Reconcile(room)
	}
	return nil
}

func (m *Manager) Start(room model.Room) {
	if !room.MonitorEnabled || !room.DeviceOnline {
		m.clearRoomEvents(room.ID)
		return
	}

	m.mu.Lock()
	if _, exists := m.sessions[room.ID]; exists {
		m.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(m.rootCtx)
	m.sessions[room.ID] = cancel
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			delete(m.sessions, room.ID)
			m.mu.Unlock()
		}()

		m.run(ctx, room)
	}()
}

func (m *Manager) Stop(roomID int64) {
	m.mu.Lock()
	cancel, ok := m.sessions[roomID]
	m.mu.Unlock()

	if ok {
		cancel()
	}
	m.clearRoomEvents(roomID)
}

func (m *Manager) Reconcile(room model.Room) {
	switch {
	case !room.MonitorEnabled:
		m.Stop(room.ID)
		_ = m.rooms.SetStatus(
			context.Background(),
			room.TenantID,
			room.ID,
			"stopped",
		)
	case !room.DeviceOnline:
		m.Stop(room.ID)
		_ = m.rooms.SetStatus(
			context.Background(),
			room.TenantID,
			room.ID,
			"device_offline",
		)
	default:
		m.Start(room)
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
	roomIDs := make([]int64, 0, len(m.sessions))
	for roomID, cancel := range m.sessions {
		cancel()
		roomIDs = append(roomIDs, roomID)
	}
	m.mu.Unlock()

	for _, roomID := range roomIDs {
		m.clearRoomEvents(roomID)
	}

	m.wg.Wait()
}

func (m *Manager) run(ctx context.Context, room model.Room) {
	backoff := 5 * time.Second

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

		live := func(runCtx context.Context) error {
			if err := m.events.ClearRoom(runCtx, room.ID); err != nil {
				return err
			}
			return m.rooms.SetStatus(runCtx, room.TenantID, room.ID, "live")
		}

		emit := func(runCtx context.Context, input model.CreateEventInput) error {
			if err := m.rooms.MarkLive(runCtx, room.TenantID, room.ID); err != nil {
				return err
			}

			if input.EventType == "room" {
				m.applyRoomMetrics(runCtx, room, input)
				return nil
			}

			event, err := m.events.Create(
				runCtx,
				room.TenantID,
				room.ID,
				input,
			)
			if err != nil {
				return err
			}

			logPublicEvent(room, event)
			m.hub.Publish(event)
			return nil
		}

		err = runner.Run(ctx, room, live, emit)
		m.clearRoomEvents(room.ID)
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
	ctx context.Context,
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

	_ = m.rooms.SetOnlineCount(ctx, room.TenantID, room.ID, payload.OnlineCount)
}
