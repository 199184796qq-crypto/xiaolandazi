package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func (peer *controlPeer) nextFeedback(t *testing.T, phase string) map[string]any {
	t.Helper()
	feedback := peer.next(t, "control_feedback", "")
	if feedback["phase"] != phase {
		t.Fatalf("expected feedback %s, got %v", phase, feedback)
	}
	return feedback
}

func (peer *controlPeer) nextTerminal(t *testing.T) map[string]any {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case message := <-peer.messages:
			if message["type"] == "control_feedback" || (message["type"] == "control_status" && (message["state"] == "done" || message["state"] == "error")) {
				return message
			}
		case err := <-peer.failures:
			t.Fatal("websocket closed unexpectedly", err)
		case <-deadline.C:
			t.Fatal("terminal event not received")
		}
	}
}

func (peer *controlPeer) noFeedback(t *testing.T, forbidden ...string) {
	t.Helper()
	deadline := time.NewTimer(75 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case message := <-peer.messages:
			if message["type"] != "control_feedback" {
				continue
			}
			for _, phase := range forbidden {
				if message["phase"] == phase {
					t.Fatal("unexpected feedback", message)
				}
			}
		case err := <-peer.failures:
			t.Fatal("websocket closed unexpectedly", err)
		case <-deadline.C:
			return
		}
	}
}

func TestWakeGreetingDoesNotDispatchHardwareCommand(t *testing.T) {
	for _, addressing := range []string{"female", "male", "child", "neutral", "invalid"} {
		t.Run(addressing, func(t *testing.T) {
			h := newControlHarness(t, "小蓝，小蓝！", false)
			h.voiceKind.Store("wake_greeting")
			h.voiceStatus.Store("succeeded")
			h.addressing.Store(addressing)
			peer := h.connect(t, true, true)
			const id = "ctl-wake-greet-0001"
			peer.voice(t, id)
			feedback := peer.nextTerminal(t)
			if feedback["type"] != "control_feedback" || feedback["phase"] != "greeting" || feedback["request_id"] != id || feedback["addressing"] != validAddressing(addressing) || feedback["message"] != greetingMessage(addressing) || feedback["listen_after"] != true {
				t.Fatal(feedback)
			}
			if status := peer.nextTerminal(t); status["state"] != "done" {
				t.Fatal("wake terminal status was not after queued greeting", status)
			}
			if h.dispatchCalls.Load() != 0 {
				t.Fatal("pure wake dispatched device command")
			}
			select {
			case event := <-h.events:
				t.Fatal("already-persisted greeting was recorded again", event)
			default:
			}
			peer.noFeedback(t, "accepted", "completed", "failed")
		})
	}
}

func TestContinuousCommandFeedbackRequiresDispatchACKAndReceipt(t *testing.T) {
	h := newControlHarness(t, "小蓝小蓝帮我音量调整到30%", false)
	h.addressing.Store("female")
	h.dispatchBlocking.Store(true)
	h.eventsBlocking.Store(true)
	peer := h.connect(t, true, true)
	const id = "ctl-feedback-proof-0001"
	peer.voice(t, id)
	peer.next(t, "control_status", "processing")
	deadline := time.Now().Add(time.Second)
	for h.dispatchCalls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if h.dispatchCalls.Load() != 1 {
		t.Fatal("dispatch did not start")
	}
	peer.noFeedback(t, "greeting", "accepted", "completed")
	h.dispatchBlocking.Store(false)
	h.dispatchBlock <- struct{}{}
	accepted := peer.nextFeedback(t, "accepted")
	if accepted["request_id"] != id || accepted["addressing"] != "female" || accepted["message"] != acceptedMessage("female") || accepted["listen_after"] != false {
		t.Fatal(accepted)
	}
	command := peer.next(t, "device_control", "")
	if command["request_id"] != id || command["value"] != float64(30) {
		t.Fatal(command)
	}
	peer.noFeedback(t, "greeting", "completed")
	ack := map[string]any{"type": "device_control_ack", "request_id": id, "action": "volume", "status": "success", "value": 30}
	peer.send(t, ack)
	select {
	case event := <-h.events:
		if event["status"] != "succeeded" || event["request_id"] != id || event["value"] != float64(30) {
			t.Fatal(event)
		}
	case <-time.After(time.Second):
		t.Fatal("ACK did not start persistence")
	}
	peer.noFeedback(t, "completed", "failed")
	h.eventsBlocking.Store(false)
	h.eventsBlock <- struct{}{}
	completed := peer.nextTerminal(t)
	if completed["type"] != "control_feedback" || completed["phase"] != "completed" || completed["request_id"] != id || completed["message"] != "已经调整好了。" || completed["listen_after"] != false {
		t.Fatal(completed)
	}
	done := peer.nextTerminal(t)
	if done["state"] != "done" || done["message"] != "音量已调整到30%" {
		t.Fatal("terminal status did not follow queued completion", done)
	}
	peer.send(t, ack)
	peer.noFeedback(t, "completed", "accepted")
	select {
	case event := <-h.events:
		t.Fatal("duplicate ACK persisted again", event)
	default:
	}
}

func TestReceiptFailureCannotSpeakSuccess(t *testing.T) {
	for _, fault := range []string{"http", "wrong-status", "wrong-billing"} {
		t.Run(fault, func(t *testing.T) {
			h := newControlHarness(t, "音量调整到30%", false)
			switch fault {
			case "http":
				h.eventsHTTPStatus.Store(503)
			case "wrong-status":
				h.eventsStatus.Store("failed")
			case "wrong-billing":
				h.eventsBilling.Store("enabled")
			}
			peer := h.connect(t, true, true)
			const id = "ctl-feedback-fail-0001"
			peer.voice(t, id)
			peer.nextFeedback(t, "accepted")
			peer.next(t, "device_control", "")
			peer.send(t, map[string]any{"type": "device_control_ack", "request_id": id, "action": "volume", "status": "success", "value": 30})
			feedback := peer.nextTerminal(t)
			if feedback["type"] != "control_feedback" || feedback["phase"] != "failed" || feedback["message"] != "这次没执行成功，请再试一次。" {
				t.Fatal("failed prompt was not queued before terminal error", feedback)
			}
			status := peer.nextTerminal(t)
			if !strings.Contains(status["message"].(string), "未确认") {
				t.Fatal(status)
			}
			peer.noFeedback(t, "completed")
		})
	}
}

func TestUnknownCommandAndDeniedDispatchFeedback(t *testing.T) {
	for _, test := range []struct {
		name, text string
		denied     bool
	}{
		{"unknown", "调整声音", false},
		{"denied", "声音大一点", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newControlHarness(t, test.text, false)
			if test.denied {
				h.dispatchHTTPStatus.Store(409)
			}
			peer := h.connect(t, true, true)
			peer.voice(t, "ctl-feedback-unknown-0001")
			peer.nextFeedback(t, "failed")
			peer.noFeedback(t, "greeting", "accepted", "completed")
			if !test.denied && h.dispatchCalls.Load() != 0 {
				t.Fatal("unknown command dispatched")
			}
		})
	}
}

func TestLegacyControlFirmwareReceivesNoNewFeedback(t *testing.T) {
	h := newControlHarness(t, "音量调整到30%", false)
	peer := h.connect(t, true)
	const id = "ctl-legacy-feedback-0001"
	peer.voice(t, id)
	peer.next(t, "device_control", "")
	peer.send(t, map[string]any{"type": "device_control_ack", "request_id": id, "action": "volume", "status": "success", "value": 30})
	peer.next(t, "control_status", "done")
	peer.noFeedback(t, "greeting", "accepted", "completed", "failed")
}

func newFeedbackController(t *testing.T) (*terminalControlSession, *websocket.Conn) {
	t.Helper()
	connections := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err == nil {
			connections <- conn
		}
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	serverConn := <-connections
	t.Cleanup(func() { serverConn.Close() })
	h := newControlHarness(t, "音量30%", false)
	control := newTerminalControlSession(context.Background(), h.g, &lockedConn{conn: serverConn}, controlTestMAC, "client", 16000, true, true, true)
	control.lease = controlLease{MAC: controlTestMAC, ClientID: "client", RequestID: "ctl-feedback-gate-0001", DeviceID: 9, TenantID: 7}
	return control, client
}

func TestFeedbackAudioGateIsAuthorizedBoundedAndIndependent(t *testing.T) {
	control, _ := newFeedbackController(t)
	now := time.Now()
	control.feedback("greeting", greetingMessage("neutral"), true)
	playback := controlMessage{RequestID: control.lease.RequestID, Phase: "greeting", State: "start"}
	forged := playback
	forged.RequestID = "ctl-forged-feedback-0001"
	control.feedbackPlayback(forged, now)
	if control.writer.audioPaused {
		t.Fatal("forged playback paused room")
	}
	control.feedbackPlayback(playback, now)
	if !control.writer.audioPaused || !control.writer.feedbackPaused {
		t.Fatal("feedback did not pause local delivery")
	}
	deadline := control.feedbackDeadline
	control.feedbackPlayback(playback, now.Add(time.Second))
	if !control.feedbackDeadline.Equal(deadline) {
		t.Fatal("duplicate start extended playback indefinitely")
	}
	control.writer.pauseRoomAudio(true)
	playback.State = "stop"
	control.feedbackPlayback(playback, now.Add(2*time.Second))
	if control.feedbackActive != "await-listen" || !control.writer.audioPaused {
		t.Fatal("greeting handoff was not protected")
	}
	control.clearFeedbackPlayback()
	if !control.writer.audioPaused || control.writer.feedbackPaused {
		t.Fatal("clearing feedback erased microphone gate")
	}
	control.writer.pauseRoomAudio(false)
	if control.writer.audioPaused {
		t.Fatal("audio did not restore")
	}
	control.feedback("accepted", acceptedMessage("neutral"), false)
	playback.Phase, playback.State = "accepted", "start"
	control.feedbackPlayback(playback, now)
	control.feedbackTick(now.Add(16 * time.Second))
	if control.writer.audioPaused {
		t.Fatal("missing playback stop left local audio muted")
	}
	control.feedbackPlayback(playback, now.Add(17*time.Second))
	if control.writer.audioPaused {
		t.Fatal("timed-out playback permit was replayed")
	}
	control.feedbackPlayback(playback, now.Add(91*time.Second))
	if control.writer.audioPaused {
		t.Fatal("expired playback permit reused")
	}
}

func TestEnhancedACKTimeoutDoesNotSpeakCompleted(t *testing.T) {
	control, client := newFeedbackController(t)
	control.state = "pending"
	control.command = commandValue("volume", "set", 30)
	control.deadline = time.Now().Add(-time.Second)
	control.tick(time.Now())
	if control.state != "finishing" {
		t.Fatal("timeout was not persisted")
	}
	select {
	case result := <-control.terminals:
		control.committed(result)
	case <-time.After(time.Second):
		t.Fatal("timeout receipt was not returned")
	}
	var status, feedback map[string]any
	if err := client.ReadJSON(&feedback); err != nil {
		t.Fatal(err)
	}
	if err := client.ReadJSON(&status); err != nil {
		t.Fatal(err)
	}
	if status["state"] != "error" || feedback["phase"] != "failed" {
		t.Fatal(status, feedback)
	}
	control.acknowledge(controlMessage{RequestID: control.lease.RequestID, Action: "volume", Status: "success", Value: json.RawMessage("30")})
	if control.state != "" {
		t.Fatal("late ACK revived timed-out command")
	}
}

func TestEnhancedAbortCancelsASRWithoutClosingSocket(t *testing.T) {
	h := newControlHarness(t, "音量30%", true)
	peer := h.connect(t, true, true)
	peer.voice(t, "ctl-enhanced-abort-0001")
	peer.next(t, "control_status", "processing")
	peer.send(t, map[string]any{"type": "abort", "request_id": "ctl-enhanced-abort-0001"})
	status := peer.next(t, "control_status", "error")
	if status["message"] != "已取消本次指令" {
		t.Fatal(status)
	}
	peer.noFeedback(t, "accepted", "completed", "failed")
	peer.send(t, map[string]any{"type": "listen", "state": "start", "request_id": "ctl-enhanced-alive-0002"})
	peer.next(t, "control_status", "error")
	if h.dispatchCalls.Load() != 0 {
		t.Fatal("canceled ASR dispatched command")
	}
}

func TestTerminalPromptCopyAndOrder(t *testing.T) {
	for _, test := range []struct {
		name, action, operation, outcome, diagnostic, phase, spoken, prompt string
	}{
		{"capture", "capture", "set", "succeeded", "照片已上传，等待环境处理", "completed", "已经拍好并上传了。", "capture_completed"},
		{"query", "volume", "query", "succeeded", "当前音量30%", "completed", "结果已经显示在屏幕上了。", "info"},
		{"failure", "volume", "set", "failed", "设备返回的实际设置值与目标不符，请重试", "failed", "这次没执行成功，请再试一次。", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			control, client := newFeedbackController(t)
			control.command = deviceCommand{Action: test.action, Operation: test.operation}
			control.finalTerminal(test.outcome, test.diagnostic)
			var feedback, status map[string]any
			if err := client.ReadJSON(&feedback); err != nil {
				t.Fatal(err)
			}
			if err := client.ReadJSON(&status); err != nil {
				t.Fatal(err)
			}
			if feedback["type"] != "control_feedback" || feedback["phase"] != test.phase || feedback["message"] != test.spoken || status["type"] != "control_status" || status["message"] != test.diagnostic {
				t.Fatal("prompt copy/order or detailed screen diagnostic lost", feedback, status)
			}
			if test.prompt != "" && feedback["prompt"] != test.prompt {
				t.Fatal("wrong static asset selector", feedback)
			}
		})
	}
}

func TestAddressingCopyMatchesApprovedAssets(t *testing.T) {
	for _, test := range []struct{ addressing, greeting, accepted string }{
		{"female", "靓女，有什么吩咐？", "靓女稍等，马上就干。"},
		{"male", "帅哥，有什么吩咐？", "帅哥稍等，马上就干。"},
		{"child", "小伙伴，有什么吩咐？", "小伙伴稍等，马上就干。"},
		{"neutral", "我在，有什么吩咐？", "好的，稍等，马上就干。"},
	} {
		if greetingMessage(test.addressing) != test.greeting || acceptedMessage(test.addressing) != test.accepted {
			t.Fatal("addressing differs from approved fixed assets", test)
		}
	}
}
