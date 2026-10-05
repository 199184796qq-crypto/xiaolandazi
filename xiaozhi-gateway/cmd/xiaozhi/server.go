package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type gateway struct {
	cfg      config
	bindings *bindingStore
	upgrader websocket.Upgrader
	controls *controlRegistry
}

func newGateway(cfg config, bindings *bindingStore) *gateway {
	return &gateway{
		cfg:      cfg,
		bindings: bindings,
		controls: newControlRegistry(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(*http.Request) bool {
				return true
			},
		},
	}
}

func (g *gateway) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", g.health)
	mux.HandleFunc("GET /xiaozhi/ota/", g.otaGet)
	mux.HandleFunc("POST /xiaozhi/ota/", g.otaPost)
	mux.HandleFunc("GET /xiaozhi/v1/", g.websocket)
	mux.HandleFunc("POST /xiaozhi/v1/capture", g.capture)
	mux.Handle("GET /internal/v1/bindings", g.internal(http.HandlerFunc(g.listBindings)))
	mux.Handle("PUT /internal/v1/bindings/{deviceID}", g.internal(http.HandlerFunc(g.putBinding)))
	mux.Handle("DELETE /internal/v1/bindings/{deviceID}", g.internal(http.HandlerFunc(g.deleteBinding)))
	return requestLogger(mux)
}

func (g *gateway) internal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.cfg.internalToken == "" || r.Header.Get("X-Xiaozhi-Internal-Token") != g.cfg.internalToken {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *gateway) health(w http.ResponseWriter, _ *http.Request) {
	_, ffmpegErr := exec.LookPath(g.cfg.ffmpegPath)
	writeJSON(w, http.StatusOK, map[string]any{
		"service":          "xiaozhi-gateway",
		"status":           "ok",
		"bindings":         len(g.bindings.snapshot()),
		"ffmpeg_available": ffmpegErr == nil,
		"time":             time.Now().UTC().Format(time.RFC3339),
	})
}

func (g *gateway) otaGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":       "xiaozhi-gateway",
		"websocket_url": g.cfg.publicWSURL,
	})
}

func (g *gateway) otaPost(w http.ResponseWriter, r *http.Request) {
	deviceID := normalizeDeviceID(r.Header.Get("Device-Id"))
	clientID := strings.TrimSpace(r.Header.Get("Client-Id"))
	if deviceID == "" || clientID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Device-Id and Client-Id are required"})
		return
	}

	var input struct {
		Application struct {
			Version string `json:"version"`
		} `json:"application"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&input)
	version := strings.TrimSpace(input.Application.Version)
	if version == "" {
		version = "0.0.0"
	}
	p, err := g.provision(r.Context(), deviceID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "device registration service unavailable"})
		return
	}
	roomID, bound := p.RoomID, p.State == "bound"
	var business any = p
	if g.cfg.managementBaseURL == "" {
		business = map[string]any{"device_id": deviceID, "room_id": roomID, "bound": bound}
	}
	log.Printf("xiaozhi ota device_id=%s client_id=%s bound=%t room=%d firmware=%s", deviceID, clientID, bound, roomID, version)
	writeJSON(w, http.StatusOK, map[string]any{
		"server_time": map[string]any{
			"timestamp":       time.Now().UnixMilli(),
			"timezone_offset": 8 * 60,
		},
		"firmware": map[string]any{
			"version": version,
			"url":     "",
		},
		"websocket": map[string]any{
			"url":     g.cfg.publicWSURL,
			"token":   g.deviceToken(deviceID, clientID),
			"version": 1,
		},
		"xiaolan": business,
	})
}

func (g *gateway) listBindings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": g.bindings.snapshot()})
}

func (g *gateway) putBinding(w http.ResponseWriter, r *http.Request) {
	if g.cfg.managementBaseURL != "" {
		writeJSON(w, 409, map[string]string{"error": "use customer device binding API"})
		return
	}
	var input struct {
		RoomID int64 `json:"room_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	deviceID := normalizeDeviceID(r.PathValue("deviceID"))
	if err := g.bindings.bind(deviceID, input.RoomID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_id": deviceID, "room_id": input.RoomID})
}

func (g *gateway) deleteBinding(w http.ResponseWriter, r *http.Request) {
	if g.cfg.managementBaseURL != "" {
		writeJSON(w, 409, map[string]string{"error": "use customer device binding API"})
		return
	}
	deviceID := normalizeDeviceID(r.PathValue("deviceID"))
	if err := g.bindings.unbind(deviceID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_id": deviceID, "unbound": true})
}

func (g *gateway) deviceToken(deviceID, clientID string) string {
	mac := hmac.New(sha256.New, []byte(g.cfg.secret))
	_, _ = mac.Write([]byte(normalizeDeviceID(deviceID)))
	_, _ = mac.Write([]byte{'\n'})
	_, _ = mac.Write([]byte(strings.TrimSpace(clientID)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (g *gateway) validAuthorization(value, deviceID, clientID string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	want := g.deviceToken(deviceID, clientID)
	return hmac.Equal([]byte(got), []byte(want))
}

func randomID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("http method=%s path=%s duration=%s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}
