package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"livecompanion/management/internal/anchorcycle"
	"net/http"
	"sync"
	"time"
)

type previewStreams struct {
	sync.Mutex
	items map[string]*previewOutput
}
type previewOutput struct {
	*cycleSupply
	mu             sync.Mutex
	connected      bool
	sentMS         float64
	connection     uint64
	durations      map[string]float64
	receiverCancel context.CancelFunc
	receiverDone   chan struct{}
}

func (p *previewOutput) disconnect(ctx context.Context) {
	p.mu.Lock()
	cancel, done := p.receiverCancel, p.receiverDone
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
}

func previewStreamKey(state *anchorcycle.State) string {
	return fmt.Sprintf("%d:%d:%s", state.TenantID, state.PlanID, state.RunID)
}
func (s *Server) previewOutput(state *anchorcycle.State) *previewOutput {
	s.previewStreams.Lock()
	defer s.previewStreams.Unlock()
	if s.previewStreams.items == nil {
		s.previewStreams.items = map[string]*previewOutput{}
	}
	key := previewStreamKey(state)
	if output := s.previewStreams.items[key]; output != nil {
		return output
	}
	parent := s.anchorProducerContext
	if parent == nil {
		parent = context.Background()
	}
	output := &previewOutput{cycleSupply: newCycleSupply(s, parent, state), durations: map[string]float64{}}
	output.pause(true)
	s.previewStreams.items[key] = output
	return output
}
func (s *Server) previewStreamScope(w http.ResponseWriter, r *http.Request) (*anchorcycle.State, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return nil, false
	}
	tenant, plan, room, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return nil, false
	}
	state, err := s.anchorCycles.Get(r.Context(), tenant, plan, room)
	if err != nil {
		cycleError(w, err)
		return nil, false
	}
	if state.RunID != r.PathValue("runID") {
		cycleError(w, anchorcycle.ErrStale)
		return nil, false
	}
	if !s.requireCycleRunAccess(w, r, actor, state) {
		return nil, false
	}
	if !state.StreamControlled || state.Surface != "generation_preview" || state.RoomID != 0 {
		writeError(w, 409, "请重新开启使用连续声音流的试听")
		return nil, false
	}
	return state, true
}
func (s *Server) previewPlaybackControl(w http.ResponseWriter, r *http.Request) {
	state, ok := s.previewStreamScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Action   string  `json:"action"`
		PlayedMS float64 `json:"played_ms"`
	}
	if readJSON(w, r, &input) != nil || input.PlayedMS < 0 {
		writeError(w, 400, "试听控制参数无效")
		return
	}
	switch input.Action {
	case "progress", "pause", "resume", "limit", "stop":
	default:
		writeError(w, 400, "未知试听控制动作")
		return
	}
	output := s.previewOutput(state)
	output.mu.Lock()
	sent := output.sentMS
	output.mu.Unlock()
	output.cycleSupply.mu.Lock()
	output.playedMS = max(output.playedMS, min(input.PlayedMS, sent))
	finished := output.finished
	played := output.playedMS
	output.cycleSupply.mu.Unlock()
	switch input.Action {
	case "progress":
	case "pause":
		output.pause(true)
		output.disconnect(r.Context())
	case "resume":
		if finished {
			writeError(w, 409, "本场试听已停止，请重新开启")
			return
		}
		output.pause(false)
	case "limit", "stop":
		output.stop()
		output.disconnect(r.Context())
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
		defer cancel()
		latest, err := s.anchorCycles.Get(cleanup, state.TenantID, state.PlanID, state.RoomID)
		if err == nil && latest.RunID == state.RunID {
			_ = s.interruptAnchorCycle(cleanup, latest)
		}
	default:
		writeError(w, 400, "未知试听控制动作")
		return
	}
	writeJSON(w, 200, map[string]any{"paused": input.Action != "resume" && input.Action != "progress", "played_ms": played})
}
func (s *Server) stopPreviewStream(state *anchorcycle.State) {
	if state == nil {
		return
	}
	s.previewStreams.Lock()
	output := s.previewStreams.items[previewStreamKey(state)]
	delete(s.previewStreams.items, previewStreamKey(state))
	s.previewStreams.Unlock()
	if output != nil {
		output.stop()
	}
}

type previewPCMFrame struct {
	Type       string  `json:"type"`
	ID         string  `json:"candidate_id"`
	SampleRate int     `json:"sample_rate"`
	Channels   int     `json:"channels"`
	PCM        []byte  `json:"pcm"`
	ClipMS     float64 `json:"clip_ms"`
	Caption    string  `json:"caption"`
	StreamMS   float64 `json:"stream_ms"`
}
type previewPCMClip struct {
	Data                            []byte
	Rate, Channels, Align, ByteRate int
	StartMS                         float64
	ID                              string
	State                           *anchorcycle.State
	Index                           int
}

func (s *Server) loadPreviewPCM(ctx context.Context, state *anchorcycle.State, index int) (previewPCMClip, error) {
	segment := state.Accepted[index]
	if err := checkCycleVoiceBinding(state, segment); err != nil {
		return previewPCMClip{}, err
	}
	key, bucket, err := cycleVoiceCacheObject(segment.Voice)
	if err != nil {
		return previewPCMClip{}, err
	}
	store, err := s.cycleCacheOSS()
	if err != nil || store.Bucket() != bucket {
		return previewPCMClip{}, errors.New("试听声音存储不一致")
	}
	reader, err := store.Open(ctx, key)
	if err != nil {
		return previewPCMClip{}, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, (64<<20)+1))
	if err != nil || len(raw) > 64<<20 {
		return previewPCMClip{}, errors.New("试听声音读取失败")
	}
	format, pcm, byteRate, err := parseGeneratedVoiceWAV(raw)
	if err != nil || len(format) < 16 || binary.LittleEndian.Uint16(format[:2]) != 1 || binary.LittleEndian.Uint16(format[14:16]) != 16 {
		return previewPCMClip{}, errors.New("试听声音不是有效的16位PCM")
	}
	channels := int(binary.LittleEndian.Uint16(format[2:4]))
	rate := int(binary.LittleEndian.Uint32(format[4:8]))
	align := int(binary.LittleEndian.Uint16(format[12:14]))
	if channels < 1 || channels > 2 || rate < 8000 || rate > 96000 || align != channels*2 || int(byteRate) != rate*align || len(pcm)%align != 0 || len(pcm) == 0 {
		return previewPCMClip{}, errors.New("试听PCM格式不一致")
	}
	return previewPCMClip{Data: pcm, Rate: rate, Channels: channels, Align: align, ByteRate: int(byteRate), ID: segment.Selected.ID, State: state, Index: index}, nil
}
func (s *Server) streamPreviewPCM(w http.ResponseWriter, r *http.Request) {
	state, ok := s.previewStreamScope(w, r)
	if !ok {
		return
	}
	s.servePreviewPCM(w, r, s.previewOutput(state), s.loadPreviewPCM, true)
}

func (s *Server) servePreviewPCM(w http.ResponseWriter, r *http.Request, output *previewOutput, loadPCM func(context.Context, *anchorcycle.State, int) (previewPCMClip, error), produce bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 503, "连续声音流不可用")
		return
	}
	output.mu.Lock()
	if output.connected {
		output.mu.Unlock()
		writeError(w, 409, "本场已有接收端，请先暂停原接收端")
		return
	}
	output.connected = true
	receiverContext, cancelReceiver := context.WithCancel(r.Context())
	r = r.WithContext(receiverContext)
	doneReceiver := make(chan struct{})
	output.receiverCancel, output.receiverDone = cancelReceiver, doneReceiver
	output.connection++
	connection := output.connection
	output.mu.Unlock()
	defer func() {
		cancelReceiver()
		output.mu.Lock()
		if output.connection == connection {
			output.pause(true)
			output.connected = false
		}
		output.mu.Unlock()
		close(doneReceiver)
	}()
	output.cycleSupply.mu.Lock()
	played := output.playedMS
	finished := output.finished
	output.cycleSupply.mu.Unlock()
	if finished {
		writeError(w, 409, "本场试听已停止，请重新开启")
		return
	}
	output.pause(false)
	// Start workers exactly once; HTTP reconnection only replaces the receiver.
	output.mu.Lock()
	first := output.connection == 1
	output.sentMS = played
	output.mu.Unlock()
	if first && produce {
		output.start()
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	encoder := json.NewEncoder(w)
	send := func(value any) error {
		if err := encoder.Encode(value); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	var clip previewPCMClip
	position := 0
	lastRevision := int64(-1)
	lastReady := -1
	lastState := time.Time{}
	cursor := played
	for r.Context().Err() == nil && output.ctx.Err() == nil {
		output.cycleSupply.mu.Lock()
		paused, failure, ack := output.paused, output.failure, output.playedMS
		progress, part := output.progress, output.progressPart
		output.cycleSupply.mu.Unlock()
		if failure != "" {
			_ = send(map[string]any{"type": "error", "message": failure})
			return
		}
		if paused {
			return
		}
		current := output.snapshot()
		if current == nil {
			return
		}
		stock, _, err := output.stock(r.Context())
		if err != nil {
			_ = send(map[string]any{"type": "error", "message": err.Error()})
			return
		}
		ready := 0
		for _, segment := range current.Accepted {
			if segment.Voice != nil && segment.Voice.Status == "ready" {
				ready++
			}
		}
		if current.Revision != lastRevision || ready != lastReady || time.Since(lastState) > time.Second {
			if send(map[string]any{"type": "state", "run": cyclePreviewProjectionForRequest(r, current), "ready_ms": stock.ReadyMS, "progress": progress, "part": part}) != nil {
				return
			}
			lastRevision, lastReady, lastState = current.Revision, ready, time.Now()
		}
		if cursor-ack >= 800 {
			if supplyWait(r.Context(), 40*time.Millisecond) != nil {
				return
			}
			continue
		}
		if position >= len(clip.Data) {
			var total float64
			index := -1
			for i, segment := range current.Accepted {
				if segment.Voice == nil || segment.Voice.Status != "ready" {
					break
				}
				output.mu.Lock()
				duration, known := output.durations[segment.Selected.ID]
				output.mu.Unlock()
				if !known {
					duration = float64(segment.Voice.Bundle.AudioDurationMS)
				}
				end := total + duration
				if cursor < end-.001 {
					index = i
					break
				}
				total = end
			}
			if index < 0 {
				if supplyWait(r.Context(), 100*time.Millisecond) != nil {
					return
				}
				continue
			}
			clip, err = loadPCM(output.ctx, current, index)
			if err != nil {
				output.fail(err)
				_ = send(map[string]any{"type": "error", "message": err.Error()})
				return
			}
			clip.StartMS = total
			output.mu.Lock()
			output.durations[clip.ID] = float64(len(clip.Data)) * 1000 / float64(clip.ByteRate)
			output.mu.Unlock()
			position = int((cursor-total)*float64(clip.ByteRate)/1000) / clip.Align * clip.Align
			position = max(0, min(position, len(clip.Data)))
		}
		count := min(clip.Rate/5*clip.Align, len(clip.Data)-position)
		if count <= 0 {
			continue
		}
		clipMS := float64(position) * 1000 / float64(clip.ByteRate)
		caption := ""
		for _, line := range clip.State.Accepted[clip.Index].Voice.Bundle.Timeline {
			if clipMS >= float64(line.StartMS) && clipMS < float64(line.EndMS) {
				caption = line.Text
				break
			}
		}
		frame := previewPCMFrame{Type: "audio", ID: clip.ID, SampleRate: clip.Rate, Channels: clip.Channels, PCM: clip.Data[position : position+count], ClipMS: clipMS, Caption: caption, StreamMS: cursor}
		if send(frame) != nil {
			return
		}
		position += count
		cursor = clip.StartMS + float64(position)*1000/float64(clip.ByteRate)
		output.mu.Lock()
		output.sentMS = cursor
		output.mu.Unlock()
		if supplyWait(r.Context(), time.Duration(float64(count)*float64(time.Second)/float64(clip.ByteRate))) != nil {
			return
		}
	}
	output.cycleSupply.mu.Lock()
	failure := output.failure
	output.cycleSupply.mu.Unlock()
	if failure != "" {
		_ = send(map[string]any{"type": "error", "message": failure})
	}
}
