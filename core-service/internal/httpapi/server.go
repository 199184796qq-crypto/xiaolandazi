package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/core/internal/collector"
	eventstore "livecompanion/core/internal/events"
	"livecompanion/core/internal/media"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/questionqueue"
	roomstore "livecompanion/core/internal/room"
	"livecompanion/core/internal/userblock"
)

type Server struct {
	rooms         *roomstore.Store
	events        *eventstore.Store
	hub           *eventstore.Hub
	collectors    *collector.Manager
	media         *media.Manager
	brain         roomBrain
	questions     *questionqueue.Queue
	userBlocks    *userblock.Store
	env           string
	internalToken string
}

func New(
	rooms *roomstore.Store,
	events *eventstore.Store,
	hub *eventstore.Hub,
	collectors *collector.Manager,
	mediaManager *media.Manager,
	env string,
	internalToken string,
) *Server {
	return &Server{
		rooms:         rooms,
		events:        events,
		hub:           hub,
		collectors:    collectors,
		media:         mediaManager,
		questions:     questionqueue.New(),
		env:           env,
		internalToken: internalToken,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /metrics", s.metrics)

	mux.Handle("GET /internal/v1/rooms", s.internal(http.HandlerFunc(s.listRooms)))
	mux.Handle("POST /internal/v1/rooms/runtime-states", s.internal(http.HandlerFunc(s.batchRoomRuntimeStates)))
	mux.Handle("POST /internal/v1/rooms", s.internal(http.HandlerFunc(s.createRoom)))
	mux.Handle("GET /internal/v1/rooms/{roomID}", s.internal(http.HandlerFunc(s.getRoom)))
	mux.Handle("PATCH /internal/v1/rooms/{roomID}/runtime", s.internal(http.HandlerFunc(s.updateRoomRuntime)))
	mux.Handle("DELETE /internal/v1/rooms/{roomID}", s.internal(http.HandlerFunc(s.deleteRoom)))
	mux.Handle("GET /internal/v1/rooms/{roomID}/events", s.internal(http.HandlerFunc(s.listEvents)))
	mux.Handle("GET /internal/v1/rooms/{roomID}/brain", s.internal(http.HandlerFunc(s.getRoomBrain)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/brain/pins", s.internal(http.HandlerFunc(s.recordRoomBrainPin)))
	mux.Handle("GET /internal/v1/rooms/{roomID}/questions", s.internal(http.HandlerFunc(s.listRoomQuestions)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/questions", s.internal(http.HandlerFunc(s.enqueueRoomQuestion)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/questions/claim", s.internal(http.HandlerFunc(s.claimRoomQuestion)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/questions/{questionID}/complete", s.internal(http.HandlerFunc(s.completeRoomQuestion)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/questions/{questionID}/release", s.internal(http.HandlerFunc(s.releaseRoomQuestion)))
	mux.Handle("DELETE /internal/v1/rooms/{roomID}/questions/{questionID}", s.internal(http.HandlerFunc(s.dropRoomQuestion)))
	mux.Handle("GET /internal/v1/rooms/{roomID}/blocked-users", s.internal(http.HandlerFunc(s.listRoomBlockedUsers)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/blocked-users", s.internal(http.HandlerFunc(s.blockRoomUser)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/blocked-users/restore", s.internal(http.HandlerFunc(s.restoreRoomBlockedUser)))
	mux.Handle("GET /internal/v1/rooms/{roomID}/stream", s.internal(http.HandlerFunc(s.streamEvents)))
	mux.Handle("GET /internal/v1/rooms/{roomID}/preview", s.internal(http.HandlerFunc(s.previewRoom)))
	mux.Handle("GET /internal/v1/rooms/{roomID}/live/{file}", s.internal(http.HandlerFunc(s.liveMedia)))
	if strings.EqualFold(strings.TrimSpace(s.env), "development") {
		mux.Handle("POST /internal/v1/dev/rooms/{roomID}/events", s.internal(http.HandlerFunc(s.createDevEvent)))
		mux.Handle("POST /internal/v1/dev/rooms/{roomID}/brain/reset", s.internal(http.HandlerFunc(s.resetRoomBrain)))
		mux.Handle("POST /internal/v1/dev/rooms/{roomID}/brain/scenario", s.internal(http.HandlerFunc(s.simulateRoomBrainScenario)))
		mux.Handle("POST /internal/v1/dev/audio/test", s.internal(http.HandlerFunc(s.createDevAudioTest)))
		mux.Handle("POST /internal/v1/dev/audio/program/start", s.internal(http.HandlerFunc(s.startDevAudioProgram)))
		mux.Handle("POST /internal/v1/dev/audio/interaction", s.internal(http.HandlerFunc(s.insertDevAudioInteraction)))
		mux.Handle("GET /internal/v1/dev/audio/mainline-map", s.internal(http.HandlerFunc(s.getDevAudioMainlineMap)))
		mux.Handle("GET /internal/v1/dev/audio/interactions", s.internal(http.HandlerFunc(s.listDevAudioInteractions)))
		mux.Handle("DELETE /internal/v1/dev/audio/interactions", s.internal(http.HandlerFunc(s.clearDevAudioInteractions)))
		mux.Handle("POST /internal/v1/dev/audio/program/stop", s.internal(http.HandlerFunc(s.stopDevAudioProgram)))
		mux.Handle("POST /internal/v1/dev/audio/events", s.internal(http.HandlerFunc(s.receiveDevAudioEvent)))
		mux.Handle("GET /internal/v1/dev/audio/tasks/{taskID}", s.internal(http.HandlerFunc(s.getDevAudioTaskState)))
	}

	return requestLogger(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	collectorStats := s.collectors.Stats()
	mediaStats := s.media.Stats()
	collectorProviders := s.collectors.ProviderDescriptors()
	writeJSON(w, http.StatusOK, map[string]any{
		"service":               "core-service",
		"status":                "ok",
		"time":                  time.Now().UTC().Format(time.RFC3339),
		"active_rooms":          collectorStats.ActiveRooms,
		"shard_index":           collectorStats.ShardIndex,
		"shard_count":           collectorStats.ShardCount,
		"lease_enabled":         collectorStats.LeaseEnabled,
		"node_id":               collectorStats.LeaseNodeID,
		"media_active_sessions": mediaStats.ActiveSessions,
		"media_max_sessions":    mediaStats.MaxSessions,
		"collector_providers":   collectorProviders,
	})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	collectorStats := s.collectors.Stats()
	mediaStats := s.media.Stats()
	providerCount := len(s.collectors.ProviderDescriptors())
	leaseEnabled := 0
	if collectorStats.LeaseEnabled {
		leaseEnabled = 1
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(
		w,
		"livecompanion_core_active_rooms %d\n"+
			"livecompanion_core_shard_index %d\n"+
			"livecompanion_core_shard_count %d\n"+
			"livecompanion_core_room_leases_enabled %d\n"+
			"livecompanion_core_media_active_sessions %d\n"+
			"livecompanion_core_media_max_sessions %d\n"+
			"livecompanion_core_collector_provider_count %d\n"+
			"livecompanion_core_event_queue_depth %d\n"+
			"livecompanion_core_event_ingress_dropped_total %d\n"+
			"livecompanion_core_event_persist_dropped_total %d\n"+
			"livecompanion_core_event_delivered_total %d\n"+
			"livecompanion_core_event_persisted_total %d\n"+
			"livecompanion_core_event_persist_errors_total %d\n"+
			"livecompanion_core_runtime_pending %d\n"+
			"livecompanion_core_runtime_writes_total %d\n"+
			"livecompanion_core_runtime_errors_total %d\n",
		collectorStats.ActiveRooms,
		collectorStats.ShardIndex,
		collectorStats.ShardCount,
		leaseEnabled,
		mediaStats.ActiveSessions,
		mediaStats.MaxSessions,
		providerCount,
		collectorStats.EventQueueDepth,
		collectorStats.EventIngressDropped,
		collectorStats.EventPersistDropped,
		collectorStats.EventDelivered,
		collectorStats.EventPersisted,
		collectorStats.EventPersistErrors,
		collectorStats.RuntimePending,
		collectorStats.RuntimeWrites,
		collectorStats.RuntimeErrors,
	)
}

func (s *Server) internal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.internalToken == "" || r.Header.Get("X-Core-Token") != s.internalToken {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	items, err := s.rooms.List(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list rooms failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) batchRoomRuntimeStates(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Items []roomstore.Key `json:"items"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(input.Items) > 1000 {
		writeError(w, http.StatusBadRequest, "too many room state items")
		return
	}
	rooms, err := s.rooms.ListByKeys(r.Context(), input.Items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "batch room state failed")
		return
	}
	type stateItem struct {
		TenantID  int64     `json:"tenant_id"`
		RoomID    int64     `json:"room_id"`
		Status    string    `json:"status"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	items := make([]stateItem, 0, len(rooms))
	for _, room := range rooms {
		items = append(items, stateItem{TenantID: room.TenantID, RoomID: room.ID, Status: room.Status, UpdatedAt: room.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var input model.CreateRoomInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input.ExternalRoomID = strings.TrimSpace(input.ExternalRoomID)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Name = strings.TrimSpace(input.Name)
	input.CollectorMode = strings.TrimSpace(input.CollectorMode)

	if input.TenantID <= 0 {
		writeError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	if input.ExternalRoomID == "" {
		writeError(w, http.StatusBadRequest, "external_room_id is required")
		return
	}
	if input.Platform == "" {
		input.Platform = "douyin"
	}
	if input.CollectorMode == "" {
		input.CollectorMode = "auto"
	}

	item, err := s.rooms.Create(r.Context(), input)
	if err != nil {
		if errors.Is(err, roomstore.ErrConflict) {
			writeError(w, http.StatusConflict, "room already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "create room failed")
		return
	}

	s.collectors.Reconcile(item)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	item, err := s.rooms.Get(r.Context(), tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}

	writeJSON(w, http.StatusOK, item)
}

func (s *Server) updateRoomRuntime(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	var input model.UpdateRoomRuntimeInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.MonitorEnabled == nil && input.DeviceOnline == nil {
		writeError(w, http.StatusBadRequest, "runtime state is required")
		return
	}

	item, err := s.rooms.UpdateRuntime(
		r.Context(),
		tenantID,
		roomID,
		input,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "update room runtime failed")
		return
	}

	s.collectors.Reconcile(item)
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) deleteRoom(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	s.collectors.Stop(roomID)

	if err := s.rooms.Delete(r.Context(), tenantID, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "delete room failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	if _, err := s.rooms.Get(r.Context(), tenantID, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	items, err := s.events.ListRecent(r.Context(), tenantID, roomID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list room events failed")
		return
	}
	if s.userBlocks != nil {
		filtered := items[:0]
		for _, item := range items {
			if s.eventIsBlocked(roomID, item.UserID, item.Nickname) {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	if _, err := s.rooms.Get(r.Context(), tenantID, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}
	if !s.collectors.IsServingRoom(roomID) {
		writeError(w, http.StatusServiceUnavailable, "room is served by another core node")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, cancel := s.hub.Subscribe(roomID)
	defer cancel()

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	flushTicker := time.NewTicker(100 * time.Millisecond)
	defer flushTicker.Stop()
	pending := false

	for {
		select {
		case <-r.Context().Done():
			if pending {
				flusher.Flush()
			}
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
			pending = false
		case <-flushTicker.C:
			if pending {
				flusher.Flush()
				pending = false
			}
		case event, open := <-ch:
			if !open {
				if pending {
					flusher.Flush()
				}
				return
			}
			if s.eventIsBlocked(roomID, event.UserID, event.Nickname) {
				continue
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			pending = true
		}
	}
}

func (s *Server) liveMedia(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	item, err := s.rooms.Get(r.Context(), tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}
	if !s.collectors.IsServingRoom(roomID) {
		writeError(w, http.StatusServiceUnavailable, "room is served by another core node")
		return
	}

	fileName := strings.TrimSpace(r.PathValue("file"))
	path, err := s.media.File(r.Context(), item, fileName)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "live video unavailable")
		return
	}

	switch {
	case strings.HasSuffix(fileName, ".m3u8"):
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store, max-age=0")
	case strings.HasSuffix(fileName, ".ts"):
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "public, max-age=2")
	default:
		writeError(w, http.StatusBadRequest, "unsupported media file")
		return
	}

	http.ServeFile(w, r, path)
}
func (s *Server) previewRoom(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}

	item, err := s.rooms.Get(r.Context(), tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}
	if !s.collectors.IsServingRoom(roomID) {
		writeError(w, http.StatusServiceUnavailable, "room is served by another core node")
		return
	}

	previewCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	data, contentType, err := s.collectors.Preview(previewCtx, item)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "room preview unavailable")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusServiceUnavailable, "room preview empty")
		return
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
func (s *Server) createDevEvent(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}

	roomItem, err := s.rooms.Get(r.Context(), &tenantID, roomID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "get room failed")
		return
	}

	var input model.CreateEventInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input.EventType = strings.TrimSpace(input.EventType)
	input.Nickname = strings.TrimSpace(input.Nickname)
	input.Content = strings.TrimSpace(input.Content)

	if input.EventType == "" {
		input.EventType = "chat"
	}
	if input.Nickname == "" {
		input.Nickname = "测试用户"
	}
	if input.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	event, err := s.events.Create(r.Context(), roomItem.TenantID, roomID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create event failed")
		return
	}
	if err := s.rooms.MarkLive(r.Context(), roomItem.TenantID, roomID); err != nil {
		writeError(w, http.StatusInternalServerError, "update room failed")
		return
	}

	s.hub.Publish(event)
	writeJSON(w, http.StatusCreated, event)
}

func optionalTenantID(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if raw == "" {
		return nil, true
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "invalid tenant_id")
		return nil, false
	}
	return &value, true
}

func requiredTenantID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, ok := optionalTenantID(w, r)
	if !ok {
		return 0, false
	}
	if value == nil {
		writeError(w, http.StatusBadRequest, "tenant_id is required")
		return 0, false
	}
	return *value, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	value, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "invalid resource id")
		return 0, false
	}
	return value, true
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("http method=%s path=%s duration=%s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
