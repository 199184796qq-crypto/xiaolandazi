// Close an explicitly selected, already failed paused meter. This never starts
// or stops Core playback and refuses working, replaced or different sessions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/db"
)

func main() {
	tenant := flag.Int64("tenant", 0, "explicit tenant")
	room := flag.Int64("room", 0, "explicit room")
	session := flag.Int64("session", 0, "explicit failed paused session")
	flag.Parse()
	if *tenant <= 0 || *room <= 0 || *session <= 0 {
		fail("explicit tenant, room and session are required")
	}
	cfg := config.Load()
	store, err := db.Open(cfg)
	if err != nil {
		fail("database unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	current, err := store.GetLiveRuntimeByRoom(ctx, *tenant, *room)
	if err != nil || current.ID != *session || current.Status != "paused" {
		fail("selected failed paused session is no longer current")
	}
	client := coreclient.New(cfg.CoreBaseURL, cfg.CoreToken)
	query := url.Values{"tenant_id": {strconv.FormatInt(*tenant, 10)}}
	resp, err := client.DoRoom(ctx, *tenant, *room, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/agent-runtime", *room), query, nil)
	if err != nil {
		fail("Core unavailable")
	}
	var state struct {
		State                 string `json:"state"`
		StopReason            string `json:"stop_reason"`
		WorkingSeconds        uint64 `json:"working_seconds"`
		LeaseRemainingSeconds uint64 `json:"lease_remaining_seconds"`
	}
	err = json.NewDecoder(resp.Body).Decode(&state)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || state.State != "stopped" || state.StopReason != "system_error" || state.LeaseRemainingSeconds != 0 {
		fail("Core is not an already stopped failed runtime")
	}
	program, err := client.GetRoomProgramSnapshot(ctx, *tenant, *room)
	if err != nil || program.Running {
		fail("audio program still running or unavailable")
	}
	current, err = store.GetLiveRuntimeByRoom(ctx, *tenant, *room)
	if err != nil || current.ID != *session || current.Status != "paused" {
		fail("session changed during verification")
	}
	closed, err := store.StopLiveRuntimeSessionMeterSystem(ctx, *session, "formal_mainline_failed", state.WorkingSeconds, time.Now().UTC())
	if err != nil {
		fail("failed session settlement unsuccessful")
	}
	fmt.Printf("session=%d status=%s billed_seconds=%d; Core playback unchanged\n", closed.ID, closed.Status, closed.TotalBilledSeconds)
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
