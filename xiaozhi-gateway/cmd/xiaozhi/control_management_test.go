package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranscribeRejectsTerminalStatusAndWrongBilling(t *testing.T) {
	for _, test := range []struct {
		name, status, billing string
		charged               int64
		accepted              bool
	}{
		{"recognized-disabled", "recognized", "disabled", 0, true},
		{"timeout-has-stale-text", "timeout", "disabled", 0, false},
		{"failed-has-stale-text", "failed", "disabled", 0, false},
		{"already-executed-must-not-repeat-adjust", "succeeded", "disabled", 0, false},
		{"missing-status", "", "disabled", 0, false},
		{"processing-is-not-final-transcript", "processing", "disabled", 0, false},
		{"wrong-billing-zero-charge", "recognized", "enabled", 0, false},
		{"missing-billing", "recognized", "", 0, false},
		{"nonzero-charge", "recognized", "disabled", 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"text": "声音大一点", "request_id": r.Header.Get("X-Request-ID"), "event_id": 101, "status": test.status, "billing_mode": test.billing, "charged_beans": test.charged})
			}))
			defer backend.Close()
			g := newGateway(config{managementBaseURL: backend.URL, internalToken: "test-token"}, &bindingStore{data: map[string]int64{}})
			text, err := g.transcribeDeviceVoice(context.Background(), controlLease{MAC: controlTestMAC, RequestID: "ctl-status-test-0001"}, [][]byte{{0xf8, 0xff, 0xfe}}, 16000)
			if (err == nil) != test.accepted {
				t.Fatalf("status=%q billing=%q charged=%d text=%q error=%v", test.status, test.billing, test.charged, text, err)
			}
			if !test.accepted && text != "" {
				t.Fatal("terminal/unapproved transcript escaped to command parser")
			}
		})
	}
}

func TestDispatchResponseFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name                      string
		statusCode                int
		state, billing, requestID string
		eventID                   int64
		allowed                   bool
		charged                   int64
		valid                     bool
	}{
		{"first-dispatch", 200, "executing", "disabled", "ctl-dispatch-0001", 44, true, 0, true},
		{"duplicate-after-gateway-restart", 409, "executing", "disabled", "ctl-dispatch-0001", 44, false, 0, false},
		{"not-authorized", 200, "executing", "disabled", "ctl-dispatch-0001", 44, false, 0, false},
		{"not-transitioned", 200, "recognized", "disabled", "ctl-dispatch-0001", 44, true, 0, false},
		{"already-executed", 200, "succeeded", "disabled", "ctl-dispatch-0001", 44, true, 0, false},
		{"wrong-request", 200, "executing", "disabled", "another-request-001", 44, true, 0, false},
		{"missing-event", 200, "executing", "disabled", "ctl-dispatch-0001", 0, true, 0, false},
		{"wrong-billing", 200, "executing", "enabled", "ctl-dispatch-0001", 44, true, 0, false},
		{"nonzero-charge", 200, "executing", "disabled", "ctl-dispatch-0001", 44, true, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/v1/xiaozhi/command-dispatch" || r.Header.Get("X-Xiaozhi-Internal-Token") != "test-token" {
					t.Error("wrong dispatch route/auth")
				}
				var input map[string]any
				_ = json.NewDecoder(r.Body).Decode(&input)
				if input["hardware_mac"] != controlTestMAC || input["request_id"] != "ctl-dispatch-0001" || input["action"] != "volume" || input["operation"] != "adjust" {
					t.Error("invalid dispatch authority/body", input)
				}
				if _, has := input["value"]; has {
					t.Error("dispatch invented actual value")
				}
				writeJSON(w, test.statusCode, map[string]any{"request_id": test.requestID, "event_id": test.eventID, "status": test.state, "dispatch_allowed": test.allowed, "billing_mode": test.billing, "charged_beans": test.charged})
			}))
			defer backend.Close()
			g := newGateway(config{managementBaseURL: backend.URL, internalToken: "test-token"}, &bindingStore{data: map[string]int64{}})
			err := g.dispatchDeviceCommand(context.Background(), controlLease{MAC: controlTestMAC, RequestID: "ctl-dispatch-0001"}, commandValue("volume", "adjust", 10))
			if (err == nil) != test.valid {
				t.Fatal("dispatch validation mismatch", err)
			}
		})
	}
}

func TestSemanticUnderstandingResponseFailsClosed(t *testing.T) {
	value := 10
	valid := commandUnderstanding{Status: "understood", Action: "volume", Operation: "adjust", Value: &value, Confidence: .98}
	if _, err := validateUnderstoodCommand(valid); err != nil {
		t.Fatal(err)
	}
	for _, out := range []commandUnderstanding{
		{Status: "understood", Action: "volume", Operation: "set", Value: intPtr(100), Confidence: .99},
		{Status: "understood", Action: "factory_reset", Operation: "set", Confidence: .99},
		{Status: "understood", Action: "capture", Operation: "set", Confidence: .80},
		{Status: "understood", Action: "font_size", Operation: "set", Value: intPtr(3), Confidence: .99},
	} {
		if _, err := validateUnderstoodCommand(out); err == nil {
			t.Fatal("unsafe semantic command accepted", out)
		}
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/xiaozhi/command-understand" || r.Header.Get("X-Xiaozhi-Internal-Token") != "test-token" {
			t.Error("wrong semantic route/auth")
		}
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input["text"] != "声音有点吵，收着点" || input["hardware_mac"] != controlTestMAC {
			t.Error("invalid semantic request", input)
		}
		writeJSON(w, 200, map[string]any{"request_id": "ctl-semantic-0001", "status": "understood", "action": "volume", "operation": "adjust", "value": -10, "message": "马上调低一点", "confidence": .98, "billing_mode": "disabled", "charged_beans": 0})
	}))
	defer backend.Close()
	g := newGateway(config{managementBaseURL: backend.URL, internalToken: "test-token"}, &bindingStore{data: map[string]int64{}})
	out, err := g.understandDeviceCommand(context.Background(), controlLease{MAC: controlTestMAC, RequestID: "ctl-semantic-0001"}, "声音有点吵，收着点")
	if err != nil || out.Action != "volume" || out.Value == nil || *out.Value != -10 {
		t.Fatal(out, err)
	}
}

func intPtr(value int) *int { return &value }

func TestRecognitionGreetingKindAndPersistedIdentity(t *testing.T) {
	for _, test := range []struct {
		name, kind, status, text string
		eventID                  int64
		valid                    bool
	}{
		{"wake-double", "wake_greeting", "succeeded", "小蓝，小蓝！", 101, true},
		{"wake-empty-asr", "wake_greeting", "succeeded", "", 101, false},
		{"wake-courtesy-alone", "wake_greeting", "succeeded", "帮我", 101, false},
		{"wake-politeness-only", "wake_greeting", "succeeded", "请", 101, false},
		{"wake-single", "wake_greeting", "succeeded", "小蓝", 101, true},
		{"wake-not-persisted", "wake_greeting", "recognized", "小蓝小蓝", 101, false},
		{"wake-missing-event", "wake_greeting", "succeeded", "小蓝小蓝", 0, false},
		{"wake-wrong-kind-command", "wake_greeting", "succeeded", "小蓝小蓝音量30%", 101, false},
		{"command-continuous", "command", "recognized", "小蓝小蓝帮我音量30%", 101, true},
		{"command-legacy-kind", "", "recognized", "音量30%", 101, true},
		{"command-missing-event", "command", "recognized", "音量30%", 0, false},
		{"command-terminal-cache", "command", "succeeded", "音量30%", 101, false},
		{"unknown-kind", "external_tool", "recognized", "音量30%", 101, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, 200, map[string]any{"text": test.text, "kind": test.kind, "status": test.status, "event_id": test.eventID, "request_id": r.Header.Get("X-Request-ID"), "speaker_addressing": "not-a-valid-address", "billing_mode": "disabled", "charged_beans": 0})
			}))
			defer backend.Close()
			g := newGateway(config{managementBaseURL: backend.URL, internalToken: "test-token"}, &bindingStore{data: map[string]int64{}})
			out, err := g.recogniseDeviceVoice(context.Background(), controlLease{MAC: controlTestMAC, RequestID: "ctl-recognition-kind-0001"}, [][]byte{{0xf8, 0xff, 0xfe}}, 16000)
			if (err == nil) != test.valid {
				t.Fatal("recognition kind validation mismatch", out, err)
			}
			if err == nil && out.SpeakerAddressing != "neutral" {
				t.Fatal("invalid addressing was not neutral", out)
			}
		})
	}
}

func TestEventReceiptCannotClaimSuccessWithoutMatchingPersistence(t *testing.T) {
	for _, test := range []struct {
		name, requestID, status, billing string
		eventID, charged                 int64
		valid                            bool
	}{
		{"persisted", "ctl-receipt-check-0001", "succeeded", "disabled", 101, 0, true},
		{"wrong-request", "ctl-another-request-0001", "succeeded", "disabled", 101, 0, false},
		{"missing-event", "ctl-receipt-check-0001", "succeeded", "disabled", 0, 0, false},
		{"failed-status", "ctl-receipt-check-0001", "failed", "disabled", 101, 0, false},
		{"billing-enabled", "ctl-receipt-check-0001", "succeeded", "enabled", 101, 0, false},
		{"charged", "ctl-receipt-check-0001", "succeeded", "disabled", 101, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/v1/xiaozhi/command-events" {
					t.Error("wrong receipt endpoint")
				}
				writeJSON(w, 200, map[string]any{"request_id": test.requestID, "event_id": test.eventID, "status": test.status, "billing_mode": test.billing, "charged_beans": test.charged})
			}))
			defer backend.Close()
			g := newGateway(config{managementBaseURL: backend.URL, internalToken: "test-token"}, &bindingStore{data: map[string]int64{}})
			value := 30
			err := g.persistCommandEvent(context.Background(), controlLease{MAC: controlTestMAC, RequestID: "ctl-receipt-check-0001"}, commandValue("volume", "set", 30), "succeeded", &value, "音量已调整到30%")
			if (err == nil) != test.valid {
				t.Fatal("receipt validation mismatch", err)
			}
		})
	}
}
