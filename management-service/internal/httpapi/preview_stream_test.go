package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPreviewStreamPreservesFullAuditedMetadataAndHidesFiles(t *testing.T) {
	state := cycleTestState(t)
	state.StreamControlled = true
	state.Surface = "generation_preview"
	state.PreviewLimitMS = 120000
	state.Accepted = []anchorcycle.Segment{{Selected: anchorcycle.Candidate{ID: "full", Text: "完整的已采纳话术。"}, Voice: &anchorcycle.VoiceAsset{Status: "ready", PreviewReservedMS: 8633, Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 40000, Text: "完整的已采纳话术。", AudioURL: "https://private.example/audio.wav", AssetManifest: map[string]any{"audio_object_key": "private.wav"}}}}}
	raw, _ := json.Marshal(cyclePreviewProjection(&state))
	if strings.Contains(string(raw), "private.wav") || strings.Contains(string(raw), "private.example") {
		t.Fatal("file reference leaked", string(raw))
	}
	voice := cycleStateForBrowser(nil, &state).Accepted[0].Voice
	if voice.Bundle.AudioDurationMS != 40000 || voice.Bundle.Text != state.Accepted[0].Selected.Text || state.Accepted[0].Voice.Bundle.AudioURL == "" {
		t.Fatal("projection cropped adopted content or mutated stored result")
	}
	if previewAudioComplete(&state) {
		t.Fatal("generated duration became a playback stop")
	}
}

func TestPreviewPlaybackSnapshotIsReadOnlyAndScopedToRun(t *testing.T) {
	state := cycleTestState(t)
	state.StreamControlled, state.Surface = true, "generation_preview"
	state.RoomID = 0
	server := &Server{}
	if snapshot := server.previewPlaybackSnapshot(&state); snapshot == nil || !snapshot.Paused || snapshot.PlayedMS != 0 || len(server.previewStreams.items) != 0 {
		t.Fatal("reading unused preview created a producer", snapshot)
	}
	output := server.previewOutput(&state)
	defer output.stop()
	output.cycleSupply.mu.Lock()
	output.playedMS, output.paused, output.finished = 43567.5, true, true
	output.progress, output.progressPart = 95, "voice"
	output.cycleSupply.mu.Unlock()
	output.mu.Lock()
	output.connected, output.sentMS = true, 44000
	output.mu.Unlock()
	snapshot := server.previewPlaybackSnapshot(&state)
	if snapshot.PlayedMS != 43567.5 || !snapshot.Finished || !snapshot.Paused || !snapshot.Connected || snapshot.Progress != 95 || snapshot.Part != "voice" {
		t.Fatal("snapshot lost actual playback state", snapshot)
	}
	other := state
	other.RunID = "different-run"
	if value := server.previewPlaybackSnapshot(&other); value.PlayedMS != 0 || value.Finished || value.Connected || len(server.previewStreams.items) != 1 {
		t.Fatal("read crossed runs or created an output", value)
	}
	other.RoomID = 26
	if server.previewPlaybackSnapshot(&other) != nil {
		t.Fatal("formal room received a preview playback snapshot")
	}
}
func TestPreviewPCMStreamCrossesFractionalFileBoundaryAndDisconnectStopsWrites(t *testing.T) {
	state := cycleTestState(t)
	state.StreamControlled = true
	state.Surface = "generation_preview"
	state.PreviewLimitMS = 120000
	// Metadata rounds to 201ms; true PCM duration is 200.625ms. Old boundary
	// comparisons can repeatedly reload the exhausted first file forever.
	for _, id := range []string{"a", "b"} {
		state.Accepted = append(state.Accepted, anchorcycle.Segment{Selected: anchorcycle.Candidate{ID: id, Text: id}, Voice: &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 201, Text: id}}})
	}
	output := &previewOutput{cycleSupply: newCycleSupply(&Server{}, context.Background(), &state), durations: map[string]float64{}}
	defer output.stop()
	load := func(ctx context.Context, s *anchorcycle.State, index int) (previewPCMClip, error) {
		return previewPCMClip{Data: make([]byte, 3210), Rate: 8000, Channels: 1, Align: 2, ByteRate: 16000, ID: s.Accepted[index].Selected.ID, State: s, Index: index}, nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { (&Server{}).servePreviewPCM(w, r, output, load, false) }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	var ids []string
	for len(ids) < 4 {
		var packet struct {
			Type string `json:"type"`
			ID   string `json:"candidate_id"`
		}
		if decoder.Decode(&packet) != nil {
			t.Fatal("stream stopped before next full unit", ids)
		}
		if packet.Type == "audio" {
			ids = append(ids, packet.ID)
		}
	}
	if strings.Join(ids, ",") != "a,a,b,b" {
		t.Fatal("file was replayed or skipped", ids)
	}
	output.pause(true)
	output.disconnect(ctx)
	output.mu.Lock()
	sent, connected := output.sentMS, output.connected
	output.mu.Unlock()
	if connected || sent < 401 || sent > 402 {
		t.Fatal("disconnect or sample cursor wrong", sent, connected)
	}
	time.Sleep(50 * time.Millisecond)
	output.mu.Lock()
	later := output.sentMS
	output.mu.Unlock()
	if later != sent {
		t.Fatal("PCM continued after pause")
	}
	output.stop()
	if output.ctx.Err() == nil {
		t.Fatal("stop did not cancel production context")
	}
}
