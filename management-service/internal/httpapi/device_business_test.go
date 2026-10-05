package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestDeviceBusinessUnauthorizedAndBadHeaders(t *testing.T) {
	s := &Server{}
	s.SetXiaozhiInternalToken("test-secret")
	for _, h := range []http.HandlerFunc{s.deviceVoice, s.deviceCommandUnderstand, s.deviceCapture, s.deviceCommandEvent, s.deviceCommandDispatch, s.deviceBusinessQueue, s.deviceBusinessResult, s.internalDeviceBusinessImage} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodPost, "/", nil))
		if w.Code != 401 {
			t.Fatalf("unauthorized got %d", w.Code)
		}
	}
	for _, headers := range [][2]string{{"", "request123"}, {"01:02:03:04:05:06", "short"}, {"01:02:03:04:05:06", strings.Repeat("a", 97)}, {"01:02:03:04:05:06", "request/123"}} {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("X-Xiaozhi-Internal-Token", "test-secret")
		r.Header.Set("X-Device-MAC", headers[0])
		r.Header.Set("X-Request-ID", headers[1])
		w := httptest.NewRecorder()
		s.deviceVoice(w, r)
		if w.Code != 400 {
			t.Fatalf("bad headers got %d", w.Code)
		}
	}
}

func TestDeviceCommandUnderstandingStrictAllowlist(t *testing.T) {
	valid := []string{
		`{"status":"understood","action":"volume","operation":"adjust","value":-10,"message":"马上调低一点","confidence":0.98}`,
		`{"status":"understood","action":"font_size","operation":"set","value":2,"message":"马上调大字体","confidence":0.95}`,
		`{"status":"understood","action":"capture","operation":"set","value":null,"message":"马上查看环境","confidence":0.99}`,
		"```json\n{\"status\":\"clarify\",\"action\":\"volume\",\"operation\":\"set\",\"value\":30,\"message\":\"想调到多少音量\",\"confidence\":0.7}\n```",
	}
	for _, raw := range valid {
		if _, err := decodeDeviceCommandUnderstanding(raw); err != nil {
			t.Fatalf("valid semantic output rejected: %s: %v", raw, err)
		}
	}
	invalid := []string{
		`{"status":"understood","action":"volume","operation":"set","value":100,"message":"调到最大","confidence":0.99}`,
		`{"status":"understood","action":"factory_reset","operation":"set","value":null,"message":"正在恢复","confidence":0.99}`,
		`{"status":"understood","action":"volume","operation":"adjust","value":5,"message":"调整一点","confidence":0.99}`,
		`{"status":"understood","action":"capture","operation":"set","value":null,"message":"查看环境","confidence":0.80}`,
		`{"status":"understood","action":"capture","operation":"set","value":null,"message":"查看环境","confidence":0.99,"extra":true}`,
	}
	for _, raw := range invalid {
		if _, err := decodeDeviceCommandUnderstanding(raw); err == nil {
			t.Fatalf("unsafe semantic output accepted: %s", raw)
		}
	}
	for _, text := range []string{"帮我恢复出厂设置", "重启一下", "把设备解绑", "帮我付款"} {
		if !riskySemanticDeviceText(text) {
			t.Fatalf("risky semantic command was not blocked: %s", text)
		}
	}
}

func TestDeviceCommandUnderstandingEndpointUsesSemanticModelAndNeverBills(t *testing.T) {
	modelCalls := 0
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls++
		if r.Header.Get("Authorization") != "Bearer semantic-test-key" || r.URL.Path != "/chat/completions" {
			t.Fatal("invalid model request")
		}
		writeJSON(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"status":"understood","action":"volume","operation":"adjust","value":-10,"message":"马上调低一点","confidence":0.98}`}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18},
		})
	}))
	defer modelServer.Close()
	t.Setenv("DASHSCOPE_BASE_URL", modelServer.URL)
	t.Setenv("DASHSCOPE_API_KEY", "semantic-test-key")

	s := &Server{}
	s.SetXiaozhiInternalToken("test-secret")
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/xiaozhi/command-understand", strings.NewReader(`{"hardware_mac":"1c2904310eb8","request_id":"ctl-semantic-endpoint-0001","text":"声音有点吵，收着点"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Xiaozhi-Internal-Token", "test-secret")
	recorder := httptest.NewRecorder()
	s.deviceCommandUnderstand(recorder, request)
	if recorder.Code != 200 || modelCalls != 1 {
		t.Fatalf("semantic endpoint failed: code=%d calls=%d body=%s", recorder.Code, modelCalls, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response["status"] != "understood" || response["action"] != "volume" || response["billing_mode"] != "disabled" || response["charged_beans"] != float64(0) {
		t.Fatal("invalid semantic receipt", response, err)
	}

	request = httptest.NewRequest(http.MethodPost, "/internal/v1/xiaozhi/command-understand", strings.NewReader(`{"hardware_mac":"1c2904310eb8","request_id":"ctl-semantic-endpoint-0002","text":"恢复出厂设置"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Xiaozhi-Internal-Token", "test-secret")
	recorder = httptest.NewRecorder()
	s.deviceCommandUnderstand(recorder, request)
	if recorder.Code != 200 || modelCalls != 1 || !strings.Contains(recorder.Body.String(), `"status":"unsupported"`) {
		t.Fatalf("risky command was not blocked before model: code=%d calls=%d body=%s", recorder.Code, modelCalls, recorder.Body.String())
	}
}

func TestDeviceCommandDispatchValidation(t *testing.T) {
	for _, input := range []model.DeviceCommandDispatchInput{{Action: "volume", Operation: "adjust"}, {Action: "font_size", Operation: "reset"}, {Action: "capture", Operation: "set"}} {
		input.HardwareMAC = "010203040506"
		input.RequestID = "ctl-test-dispatch"
		if err := validateDeviceDispatch(&input); err != nil || input.HardwareMAC != "01:02:03:04:05:06" {
			t.Fatal(input, err)
		}
	}
	for _, input := range []model.DeviceCommandDispatchInput{{Action: "", Operation: ""}, {Action: "volume", Operation: "reset"}, {Action: "font_size", Operation: "mute"}, {Action: "capture", Operation: "adjust"}} {
		input.HardwareMAC = "01:02:03:04:05:06"
		input.RequestID = "ctl-test-dispatch"
		if validateDeviceDispatch(&input) == nil {
			t.Fatal("invalid dispatch accepted", input)
		}
	}
}

func TestDeviceCommandValidation(t *testing.T) {
	valid := []model.DeviceCommandEventInput{
		{Action: "volume", Operation: "set", Status: "success", Value: json.RawMessage("30")},
		{Action: "volume", Operation: "mute", Status: "succeeded", Value: json.RawMessage("0")},
		{Action: "font_size", Operation: "reset", Status: "succeeded", Value: json.RawMessage("1")},
		{Action: "font_size", Operation: "set", Status: "succeeded", Value: json.RawMessage(`"large"`)},
		{Action: "capture", Status: "success"},
		{Status: "rejected"}, {Status: "timeout"}, {Status: "error"},
	}
	for _, input := range valid {
		input.HardwareMAC = "01:02:03:04:05:06"
		input.RequestID = "ctl-request_123"
		if err := validateDeviceCommand(&input); err != nil {
			t.Fatalf("valid %v: %v", input, err)
		}
	}
	invalid := []model.DeviceCommandEventInput{{Action: "erase_flash", Status: "succeeded"}, {Action: "volume", Operation: "set", Status: "succeeded", Value: json.RawMessage("101")}, {Action: "volume", Operation: "adjust", Status: "succeeded", Value: json.RawMessage("30.5")}, {Action: "volume", Operation: "set", Status: "succeeded"}, {Action: "font_size", Operation: "set", Status: "succeeded", Value: json.RawMessage("3")}, {Action: "font_size", Operation: "mute", Status: "failed"}, {Action: "capture", Status: "success", Value: json.RawMessage("1")}, {Status: "succeeded"}}
	for _, input := range invalid {
		input.HardwareMAC = "01:02:03:04:05:06"
		input.RequestID = "request_123"
		if validateDeviceCommand(&input) == nil {
			t.Fatalf("invalid accepted %v", input)
		}
	}
}

func TestDeviceJPEGAndAudioValidation(t *testing.T) {
	var good bytes.Buffer
	if err := jpeg.Encode(&good, image.NewRGBA(image.Rect(0, 0, 8, 5)), nil); err != nil {
		t.Fatal(err)
	}
	w, h, err := validateDeviceJPEG(good.Bytes())
	if err != nil || w != 8 || h != 5 {
		t.Fatal(w, h, err)
	}
	var wide bytes.Buffer
	_ = jpeg.Encode(&wide, image.NewRGBA(image.Rect(0, 0, 2049, 1)), nil)
	for _, data := range [][]byte{[]byte("fake.jpg"), good.Bytes()[:20], wide.Bytes()} {
		if _, _, err := validateDeviceJPEG(data); err == nil {
			t.Fatal("bad image accepted")
		}
	}
	wav := make([]byte, 44)
	copy(wav, "RIFF")
	copy(wav[8:], "WAVE")
	if ext, _ := deviceAudioFormat(wav); ext != ".wav" {
		t.Fatal(ext)
	}
	ogg := append([]byte("OggS"), make([]byte, 23)...)
	ogg = append(ogg, []byte("OpusHead")...)
	if ext, _ := deviceAudioFormat(ogg); ext != ".ogg" {
		t.Fatal(ext)
	}
	if ext, _ := deviceAudioFormat([]byte("pretend.wav")); ext != "" {
		t.Fatal("fake audio accepted")
	}
}

func TestDeviceMultipartSizeAndNoFile(t *testing.T) {
	for _, size := range []int{0, int(maxDeviceBusinessBytes) + 1} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, _ := writer.CreateFormFile("file", "test.wav")
		_, _ = file.Write(bytes.Repeat([]byte("a"), size))
		_ = writer.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		if _, _, err := readDeviceBusinessFile(httptest.NewRecorder(), r); err == nil {
			t.Fatal("size accepted", size)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("file=bogus"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, _, err := readDeviceBusinessFile(httptest.NewRecorder(), r); err == nil {
		t.Fatal("not multipart accepted")
	}
}

func TestDeviceBillingResponseIsAlwaysDisabled(t *testing.T) {
	out := deviceBusinessResponse(model.DeviceBusinessEvent{ID: 3, RequestID: "request123", BillingMode: "enabled", ChargedBeans: 999})
	if out["billing_mode"] != "disabled" || out["charged_beans"] != 0 {
		t.Fatal(out)
	}
}

func TestDeviceVoiceResponseFlushBeforeSlowCleanup(t *testing.T) {
	cleanupStarted := make(chan struct{})
	releaseCleanup := make(chan struct{})
	cleanupFinished := make(chan struct{})
	// This channel-bound fake emulates the synchronous Delete defer. It must
	// remain running when the real HTTP client finishes decoding the response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			close(cleanupStarted)
			select {
			case <-releaseCleanup:
			case <-time.After(3 * time.Second):
			}
			close(cleanupFinished)
		}()
		// Exercise the actual middleware writers, not just httptest.Recorder.
		wrapped := &inboxStatusWriter{ResponseWriter: &auditStatusWriter{ResponseWriter: w}}
		if err := writeDeviceVoiceResponse(wrapped, map[string]any{"request_id": "ctl-slow-cleanup", "event_id": 1, "status": "recognized", "billing_mode": "disabled", "charged_beans": 0, "text": "音量30%", "speaker_addressing": "neutral", "kind": "command"}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	defer close(releaseCleanup)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal("success receipt was blocked by cleanup", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal("complete JSON was blocked by cleanup", err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(data, &receipt); err != nil || receipt["request_id"] != "ctl-slow-cleanup" || receipt["charged_beans"] != float64(0) {
		t.Fatal("invalid receipt", receipt, err)
	}
	if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" || response.ContentLength != int64(len(data)) {
		t.Fatal("invalid response contract", response.StatusCode, response.Header)
	}
	select {
	case <-cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("bounded cleanup was not attempted")
	}
	select {
	case <-cleanupFinished:
		t.Fatal("receipt was not delivered before slow cleanup completed")
	default:
	}
}

func TestDeviceVoiceBudgetsLeaveGatewayMargin(t *testing.T) {
	if deviceVoiceResponseTimeout >= 30*time.Second || deviceVoiceCommitReserve < time.Second {
		t.Fatal("voice request has no gateway/commit margin")
	}
	if 25*time.Second+3*time.Second+deviceVoiceCommitReserve > deviceVoiceResponseTimeout {
		t.Fatal("phase budgets exceed response deadline")
	}
}
