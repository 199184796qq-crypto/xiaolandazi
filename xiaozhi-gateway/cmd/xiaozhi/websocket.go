package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type lockedConn struct {
	conn           *websocket.Conn
	mu             sync.Mutex
	audioPaused    bool
	roomPaused     bool
	feedbackPaused bool
}

func (c *lockedConn) json(value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if event, ok := value.(map[string]any); ok && event["type"] == "tts" && c.audioPaused {
		return nil
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(value)
}

func (c *lockedConn) binary(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.audioPaused {
		return nil
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(websocket.BinaryMessage, payload)
}

// Keep consuming Core's room PCM timeline while suppressing delivery only to
// this terminal. No Core pause/stop API is called and no room setting is changed.
func (c *lockedConn) pauseRoomAudio(paused bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roomPaused = paused
	c.audioPaused = c.roomPaused || c.feedbackPaused
}

func (c *lockedConn) pauseFeedbackAudio(paused bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.feedbackPaused = paused
	c.audioPaused = c.roomPaused || c.feedbackPaused
}

type wsRead struct {
	messageType int
	payload     []byte
	err         error
}

// coreAudioEngineSnapshot is the small portion of Core's audio-engine
// snapshot that the Xiaozhi terminal needs in order to render the text that
// belongs to the audio currently being played.  Core already maintains this
// timeline; the gateway only mirrors its current segment onto the device
// WebSocket as a protocol message.
type coreAudioEngineSnapshot struct {
	SpeechFeed struct {
		Current *struct {
			SegmentID string `json:"segment_id"`
			Text      string `json:"text"`
		} `json:"current"`
	} `json:"speech_feed"`
}

func readMessages(ctx context.Context, conn *websocket.Conn) <-chan wsRead {
	ch := make(chan wsRead, 1)
	go func() {
		defer close(ch)
		for {
			messageType, payload, err := conn.ReadMessage()
			select {
			case ch <- wsRead{messageType: messageType, payload: payload, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

func (g *gateway) websocket(w http.ResponseWriter, r *http.Request) {
	deviceID := normalizeDeviceID(r.Header.Get("Device-Id"))
	clientID := strings.TrimSpace(r.Header.Get("Client-Id"))
	if deviceID == "" || clientID == "" {
		http.Error(w, "missing device identity", http.StatusBadRequest)
		return
	}
	if !g.validAuthorization(r.Header.Get("Authorization"), deviceID, clientID) {
		log.Printf("xiaozhi websocket auth rejected device_id=%s client_id=%s", deviceID, clientID)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	p, provisionErr := g.provision(r.Context(), deviceID)
	if provisionErr != nil {
		http.Error(w, "device registration service unavailable", http.StatusServiceUnavailable)
		return
	}
	roomID, bound := p.RoomID, p.State == "bound"
	if !bound && g.cfg.managementBaseURL == "" {
		log.Printf("xiaozhi websocket unbound device_id=%s client_id=%s", deviceID, clientID)
		http.Error(w, "device is not bound to a room", http.StatusConflict)
		return
	}

	headerVersion := strings.TrimSpace(r.Header.Get("Protocol-Version"))
	if headerVersion != "" && headerVersion != "1" {
		http.Error(w, "only protocol version 1 is enabled", http.StatusUpgradeRequired)
		return
	}

	conn, err := g.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	log.Printf("xiaozhi websocket connected device_id=%s client_id=%s room=%d", deviceID, clientID, roomID)
	defer log.Printf("xiaozhi websocket disconnected device_id=%s client_id=%s room=%d", deviceID, clientID, roomID)
	conn.SetReadLimit(1 << 20)
	writer := &lockedConn{conn: conn}
	sessionID := randomID()

	_ = conn.SetReadDeadline(time.Now().Add(12 * time.Second))
	messageType, payload, err := conn.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		return
	}

	var hello struct {
		Type        string `json:"type"`
		Version     int    `json:"version"`
		Transport   string `json:"transport"`
		AudioParams struct {
			Format     string `json:"format"`
			SampleRate int    `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"audio_params"`
		Features struct {
			DeviceControl  bool `json:"device_control"`
			CameraCapture  bool `json:"camera_capture"`
			DeviceFeedback bool `json:"device_feedback"`
		} `json:"features"`
	}
	if err := json.Unmarshal(payload, &hello); err != nil || hello.Type != "hello" || hello.Transport != "websocket" {
		_ = writer.json(map[string]any{"type": "alert", "status": "Error", "message": "invalid hello"})
		return
	}
	if hello.Version == 0 {
		hello.Version = 1
	}
	if hello.Version != 1 {
		_ = writer.json(map[string]any{"type": "alert", "status": "Unsupported", "message": "当前小蓝适配器先支持 WebSocket 协议 v1"})
		return
	}
	if hello.AudioParams.Format != "" && !strings.EqualFold(hello.AudioParams.Format, "opus") {
		_ = writer.json(map[string]any{"type": "alert", "status": "Unsupported", "message": "audio format must be opus"})
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if err := writer.json(map[string]any{
		"type":       "hello",
		"transport":  "websocket",
		"session_id": sessionID,
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    defaultSampleRate,
			"channels":       1,
			"frame_duration": defaultFrameMS,
		},
	}); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if g.cfg.managementBaseURL != "" {
		micRate := hello.AudioParams.SampleRate
		if micRate == 0 {
			micRate = 16000
		}
		if hello.AudioParams.Channels != 0 && hello.AudioParams.Channels != 1 {
			hello.Features.DeviceControl = false
		}
		g.managedDeviceSession(ctx, writer, deviceID, clientID, sessionID, p, micRate, hello.Features.DeviceControl, hello.Features.CameraCapture, hello.Features.DeviceFeedback)
		return
	}
	audioDone := make(chan error, 1)
	go func() {
		audioDone <- g.streamRoomAudio(ctx, writer, roomID, sessionID)
	}()

	reads := readMessages(ctx, conn)
	for {
		select {
		case err := <-audioDone:
			if err != nil && !errors.Is(err, context.Canceled) {
				_ = writer.json(map[string]any{
					"session_id": sessionID,
					"type":       "alert",
					"status":     "Audio",
					"message":    "直播间声音连接中",
				})
			}
			return
		case read, open := <-reads:
			if !open || read.err != nil {
				return
			}
			if read.messageType == websocket.BinaryMessage {
				continue
			}
			if read.messageType != websocket.TextMessage {
				continue
			}
			var message map[string]any
			if json.Unmarshal(read.payload, &message) != nil {
				continue
			}
			if message["type"] == "goodbye" {
				return
			}
		}
	}
}

func (g *gateway) streamRoomAudio(ctx context.Context, writer *lockedConn, roomID int64, sessionID string) error {
	coreURL := strings.TrimRight(g.cfg.coreBaseURL, "/") +
		"/v1/rooms/" + strconv.FormatInt(roomID, 10) + "/composite.pcm"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("core composite stream http=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	cmd := exec.CommandContext(
		ctx,
		g.cfg.ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "s16le", "-ar", "24000", "-ac", "1", "-i", "pipe:0",
		"-vn",
		"-c:a", "libopus",
		"-application", "lowdelay",
		"-frame_duration", "60",
		"-ar", "24000",
		"-ac", "1",
		"-f", "ogg",
		"-page_duration", "20000",
		"-flush_packets", "1",
		"pipe:1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	const ttsIdleTimeout = 1200 * time.Millisecond
	var sendMu sync.Mutex
	started := false
	lastInputAt := time.Time{}
	audioStarted := make(chan struct{})
	var audioStartedOnce sync.Once
	speechCtx, speechCancel := context.WithCancel(ctx)
	defer speechCancel()
	go g.streamRoomText(speechCtx, writer, roomID, sessionID, audioStarted)
	copyDone := make(chan error, 1)
	go func() {
		defer stdin.Close()
		buffer := make([]byte, 32*1024)
		for {
			count, readErr := resp.Body.Read(buffer)
			if count > 0 {
				sendMu.Lock()
				if !started {
					if err := writer.json(map[string]any{
						"session_id": sessionID,
						"type":       "tts",
						"state":      "start",
					}); err != nil {
						sendMu.Unlock()
						copyDone <- err
						return
					}
					started = true
					audioStartedOnce.Do(func() { close(audioStarted) })
				}
				lastInputAt = time.Now()
				sendMu.Unlock()
				if _, err := stdin.Write(buffer[:count]); err != nil {
					copyDone <- err
					return
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					copyDone <- nil
				} else {
					copyDone <- readErr
				}
				return
			}
		}
	}()

	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				sendMu.Lock()
				if started && !lastInputAt.IsZero() && now.Sub(lastInputAt) >= ttsIdleTimeout {
					_ = writer.json(map[string]any{
						"session_id": sessionID,
						"type":       "tts",
						"state":      "stop",
					})
					started = false
				}
				sendMu.Unlock()
			}
		}
	}()

	packetErr := readOggPackets(stdout, func(packet []byte, index int) error {
		if index < 2 {
			return nil
		}
		return writer.binary(packet)
	})
	if packetErr != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()

	sendMu.Lock()
	if started {
		_ = writer.json(map[string]any{
			"session_id": sessionID,
			"type":       "tts",
			"state":      "stop",
		})
		started = false
	}
	sendMu.Unlock()

	if packetErr != nil {
		return packetErr
	}
	if waitErr != nil && ctx.Err() == nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		if detail == "" {
			detail = waitErr.Error()
		}
		return fmt.Errorf("ffmpeg opus stream failed: %s", detail)
	}
	select {
	case copyErr := <-copyDone:
		if copyErr != nil && ctx.Err() == nil {
			return copyErr
		}
	default:
	}
	return ctx.Err()
}

// streamRoomText mirrors Core's current speech segment to the Xiaozhi
// protocol.  The PCM endpoint intentionally remains audio-only; polling the
// already-public audio-engine snapshot keeps the audio framing untouched and
// lets old Core audio streams continue to work.  A new segment is emitted
// once as the protocol's sentence_start event, which the flashed firmware
// already renders as an assistant chat bubble.
func (g *gateway) streamRoomText(ctx context.Context, writer *lockedConn, roomID int64, sessionID string, audioStarted <-chan struct{}) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	select {
	case <-audioStarted:
	case <-ctx.Done():
		return
	}

	lastSegmentID := ""
	lastText := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		endpoint := strings.TrimRight(g.cfg.coreBaseURL, "/") +
			"/v1/rooms/" + strconv.FormatInt(roomID, 10) + "/audio-engine"
		requestCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			continue
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			continue
		}
		var snapshot coreAudioEngineSnapshot
		if resp.StatusCode == http.StatusOK {
			_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&snapshot)
		}
		_ = resp.Body.Close()
		cancel()

		current := snapshot.SpeechFeed.Current
		if current == nil {
			lastSegmentID = ""
			lastText = ""
			continue
		}
		segmentID := strings.TrimSpace(current.SegmentID)
		text := strings.TrimSpace(current.Text)
		if segmentID == "" || text == "" {
			continue
		}
		if segmentID == lastSegmentID && text == lastText {
			continue
		}
		if err := writer.json(map[string]any{
			"session_id": sessionID,
			"type":       "tts",
			"state":      "sentence_start",
			"text":       text,
		}); err != nil {
			return
		}
		lastSegmentID = segmentID
		lastText = text
	}
}

func readOggPackets(r io.Reader, onPacket func([]byte, int) error) error {
	br := bufio.NewReader(r)
	var packet []byte
	packetIndex := 0
	for {
		header := make([]byte, 27)
		if _, err := io.ReadFull(br, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		if string(header[:4]) != "OggS" {
			return errors.New("invalid ogg capture pattern")
		}
		segmentCount := int(header[26])
		laces := make([]byte, segmentCount)
		if _, err := io.ReadFull(br, laces); err != nil {
			return err
		}
		bodyLen := 0
		for _, lace := range laces {
			bodyLen += int(lace)
		}
		body := make([]byte, bodyLen)
		if _, err := io.ReadFull(br, body); err != nil {
			return err
		}
		offset := 0
		for _, lace := range laces {
			n := int(lace)
			packet = append(packet, body[offset:offset+n]...)
			offset += n
			if lace < 255 {
				if len(packet) > 0 {
					if err := onPacket(append([]byte(nil), packet...), packetIndex); err != nil {
						return err
					}
					packetIndex++
				}
				packet = packet[:0]
			}
		}
	}
}
