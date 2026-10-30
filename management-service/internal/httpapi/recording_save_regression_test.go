package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestRecordingScriptLongUploadedAudio(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length int
		assets []int64
		valid  bool
	}{
		{"forty-minute-upload", 15534, []int64{26103}, true},
		{"total-boundary", 80000, []int64{26103}, true},
		{"over-total", 80001, []int64{26103}, false},
		{"pasted-text-boundary", 4000, nil, true},
		{"pasted-text-still-bounded", 4001, nil, false},
		{"invalid-asset", 15534, []int64{0}, false},
		{"multiple-assets", 15534, []int64{1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := map[string]any{"recording_script": recordingScript{Tabs: []recordingScriptTab{{TabID: "t1", Text: strings.Repeat("声", tc.length), AudioAssetIDs: tc.assets}}}}
			err := normalizeRecordingScript(ctx)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if tc.valid {
				script, ok := recordingScriptFromContext(ctx)
				if !ok || len([]rune(script.Tabs[0].Text)) != tc.length {
					t.Fatal("long transcript truncated")
				}
			}
		})
	}
	ctx := map[string]any{"recording_script": recordingScript{Tabs: []recordingScriptTab{
		{TabID: "a", Text: strings.Repeat("声", 50000), AudioAssetIDs: []int64{1}},
		{TabID: "b", Text: strings.Repeat("声", 30001), AudioAssetIDs: []int64{2}},
	}}}
	if normalizeRecordingScript(ctx) == nil {
		t.Fatal("combined total cap bypassed")
	}
}

// The page fixture can hand its actual production-serializer payload to the
// real Go decoder and validators, without a database or paid provider.
func TestRecordingBrowserSavePayload(t *testing.T) {
	path := os.Getenv("RECORDING_SAVE_TEST_PAYLOAD")
	if path == "" {
		t.Skip("set RECORDING_SAVE_TEST_PAYLOAD to the local browser fixture output")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= 64<<10 {
		t.Fatal("fixture must exercise the old 64 KiB rejection")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewReader(raw))
	var input model.CreateLiveAgentPlanVersionInput
	if err := readJSON(w, r, &input); err != nil {
		t.Fatal(err)
	}
	if err := normalizeRecordingScript(input.GenerationContext); err != nil {
		t.Fatal(err)
	}
	if err := validateLiveAgentPlanVersionInput(&input); err != nil {
		t.Fatal(err)
	}
	script, ok := recordingScriptFromContext(input.GenerationContext)
	if !ok || len(script.Tabs) == 0 || len(input.Variants) != 1 {
		t.Fatal("recording payload lost")
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) == 0 {
		t.Fatal(err)
	}
}
