package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"livecompanion/management/internal/model"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit read-only probe: the caller supplies already-played archive rows
// through stdin. It performs GETs only; never creates speech, sessions or rows.
func TestSessionDownloadReadOnlyReplay(t *testing.T) {
	if os.Getenv("SESSION_DOWNLOAD_READONLY_REPLAY") != "1" {
		t.Skip("requires explicit archive replay input")
	}
	var rows []struct {
		ID, Tenant, Room, Session int64
		Ref, Kind, Text, URL      string
		Duration                  int
		Played, Expires           time.Time
	}
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 16<<20)).Decode(&rows); err != nil {
		t.Fatal("invalid replay fixture")
	}
	var assets []model.SessionSpeechAsset
	for _, row := range rows {
		played, expires := row.Played, row.Expires
		assets = append(assets, model.SessionSpeechAsset{ID: row.ID, TenantID: row.Tenant, RoomID: row.Room, SessionID: row.Session, SourceRef: row.Ref, Kind: row.Kind, Text: row.Text, AudioURL: row.URL, DurationMS: row.Duration, PlayedAt: &played, ExpiresAt: &expires, Available: row.URL != "" && time.Now().Before(expires)})
	}
	items := sessionDownloadItems(assets)
	counts := map[string]int{}
	pairs := 0
	var selected *sessionDownloadItem
	for i := range items {
		item := &items[i]
		counts[item.Kind]++
		if item.Kind == "bridge" {
			t.Fatal("standalone bridge leaked")
		}
		if len(item.Parts) == 2 {
			pairs++
			if selected == nil && item.Available {
				selected = item
			}
		}
	}
	if selected == nil {
		t.Fatal("no complete available interaction pair")
	}
	for _, part := range selected.Parts {
		if !strings.Contains(selected.Text, strings.TrimSpace(part.Text)) {
			t.Fatal("paired text missing")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	w := httptest.NewRecorder()
	writeSessionDownloadAudio(ctx, w, *selected, sessionAudioClient())
	if w.Code != 200 {
		t.Fatalf("replay download failed with HTTP %d", w.Code)
	}
	f, pcm, rate, err := parseGeneratedVoiceWAV(w.Body.Bytes())
	if err != nil || len(pcm) == 0 || rate == 0 || int(binary.LittleEndian.Uint32(w.Body.Bytes()[4:8])) != w.Body.Len()-8 {
		t.Fatal("invalid merged WAV")
	}
	if binary.LittleEndian.Uint16(f[:2]) != 1 {
		t.Fatal("unexpected merged codec")
	}
	report := map[string]any{"raw_rows": len(rows), "grouped_rows": len(items), "counts": counts, "paired_interactions": pairs, "download_id": selected.ID, "download_http": w.Code, "merged_wav_bytes": w.Body.Len(), "pcm_ms": len(pcm) * 1000 / int(rate), "riff_size_finalized": true}
	encoded, _ := json.Marshal(report)
	t.Log("ARCHIVE_REPLAY_REPORT " + string(encoded))
}
