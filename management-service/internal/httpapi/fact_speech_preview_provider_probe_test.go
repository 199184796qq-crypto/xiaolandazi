package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Opt-in provider replay: current saved assets and real mainline model, with no
// plan adoption, publishing, playback, runtime persistence or direction writes.
func TestFactSpeechPreviewProvider(t *testing.T) {
	if os.Getenv("FACT_SPEECH_PREVIEW_PROBE") != "1" {
		t.Skip("explicit provider replay required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 140*time.Second)
	defer cancel()
	items, err := store.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range items {
		if !strings.Contains(f.Key, "无理由") {
			continue
		}
		state, err := s.prepareFactSpeechPreview(ctx, 14, 4, 26, f, "事实话术预览测试")
		if err != nil {
			t.Fatal(err)
		}
		output, err := s.generateFactSpeechPreview(ctx, &state)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(map[string]any{"fact_name": f.Key, "fact_value": f.Value, "preview": output, "warnings": state.Warnings, "angle_count": len(state.AnglePlans)})
		t.Log(string(encoded))
		if output["text"] == "" || state.Config.TTS != nil || state.Delivery != nil {
			t.Fatal("invalid read-only preview")
		}
		return
	}
	t.Fatal("explicit return-policy fixture missing")
}
