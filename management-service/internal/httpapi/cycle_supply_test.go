package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type supplyRepository struct {
	anchorcycle.Repository
	mu    sync.Mutex
	state *anchorcycle.State
}

func (r *supplyRepository) Get(context.Context, int64, int64, int64) (*anchorcycle.State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, _ := json.Marshal(r.state)
	var state anchorcycle.State
	_ = json.Unmarshal(raw, &state)
	return &state, nil
}
func TestSupplyDeliversFirstUnitAndStopsBothWorkersWithoutLateDelivery(t *testing.T) {
	state := cycleTestState(t)
	state.Config.TTS = &anchorcycle.VoiceConfig{Rate: 1, BufferSeconds: 20}
	repo := &supplyRepository{state: &state}
	server := &Server{anchorCycles: repo}
	p := newCycleSupply(server, context.Background(), &state)
	defer p.stop()
	var calls atomic.Int32
	var inventory atomic.Int64
	delivered := make(chan string, 2)
	startup := make(chan string, 8)
	secondStarted := make(chan struct{})
	p.startup = func(_ context.Context, stage string) { startup <- stage }
	p.externalStock = func(context.Context) (int64, error) { return inventory.Load(), nil }
	p.advance = func(ctx context.Context, current *anchorcycle.State) (*anchorcycle.State, error) {
		n := calls.Add(1)
		if n == 2 {
			close(secondStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if n > 2 {
			t.Error("unexpected additional text call")
		}
		repo.mu.Lock()
		repo.state.Accepted = append(repo.state.Accepted, anchorcycle.Segment{Selected: anchorcycle.Candidate{ID: "one", Text: strings.Repeat("稿", 100)}})
		repo.state.Revision++
		repo.mu.Unlock()
		return repo.Get(ctx, 0, 0, 0)
	}
	p.synthesize = func(ctx context.Context, current *anchorcycle.State, id string) (*anchorcycle.VoiceAsset, error) {
		voice := &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{Text: "第一段完整稿。", AudioDurationMS: 30000}}
		repo.mu.Lock()
		repo.state.Accepted[0].Voice = voice
		repo.mu.Unlock()
		return voice, nil
	}
	p.onReady = func(ctx context.Context, current *anchorcycle.State, segment anchorcycle.Segment) error {
		inventory.Store(30000)
		delivered <- segment.Selected.ID
		return nil
	}
	p.start()
	select {
	case id := <-delivered:
		if id != "one" {
			t.Fatal(id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first ready sound waited for second batch")
	}
	var stages []string
	for len(startup) > 0 {
		stages = append(stages, <-startup)
	}
	if strings.Join(stages, ",") != "preparing_content,generating_voice,voice_ready" {
		t.Fatal("unexpected public startup stages", stages)
	}
	time.Sleep(400 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("high watermark did not stop text generation", calls.Load())
	}
	inventory.Store(0)
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("low watermark did not resume text generation")
	}
	p.stop()
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("producer workers survived stop")
	}
	select {
	case id := <-delivered:
		t.Fatal("late output after stop", id)
	default:
	}
}

func TestSupplyProgressPublishesContentCheckOnce(t *testing.T) {
	state := cycleTestState(t)
	p := newCycleSupply(&Server{}, context.Background(), &state)
	defer p.stop()
	var stages []string
	p.startup = func(_ context.Context, stage string) { stages = append(stages, stage) }
	report, ok := p.progressContext("text").Value(cycleProgressKey{}).(cycleProgressReporter)
	if !ok {
		t.Fatal("progress reporter missing")
	}
	report(25)
	report(60)
	report(80)
	if strings.Join(stages, ",") != "checking_content" {
		t.Fatal(stages)
	}
}
func TestProductionGatePauseBlocksNextModelCallAndStopCancelsIt(t *testing.T) {
	state := cycleTestState(t)
	p := newCycleSupply(&Server{}, context.Background(), &state)
	p.pause(true)
	result := make(chan error, 1)
	go func() { _, err := (&Server{}).beginModelExchange(p.ctx, "tts", "selected", nil); result <- err }()
	select {
	case <-result:
		t.Fatal("paused producer started another model call")
	case <-time.After(30 * time.Millisecond):
	}
	p.stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not wake gated provider")
	}
}
func TestSupplyStockCountsTTSOnceAndUsesActualPlayback(t *testing.T) {
	state := cycleTestState(t)
	state.StreamControlled = true
	state.PreviewLimitMS = 120000
	state.Accepted = []anchorcycle.Segment{{Selected: anchorcycle.Candidate{ID: "ready", Text: "完整稿"}, Voice: &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 30000, Text: "完整稿"}}}, {Selected: anchorcycle.Candidate{ID: "tts", Text: "下一份文字。"}, Voice: &anchorcycle.VoiceAsset{Status: "generating"}}}
	p := newCycleSupply(&Server{}, context.Background(), &state)
	defer p.stop()
	p.playedMS = 10000
	stock, remaining, err := p.stock(context.Background())
	if err != nil || stock.ReadyMS != 20000 || stock.PendingCount != 1 || stock.PendingMS != 2500 || remaining != 110000 {
		t.Fatal(stock, remaining, err)
	}
}

func (r *supplyRepository) Acquire(context.Context, int64, int64, int64, string) error { return nil }
func (r *supplyRepository) Release(context.Context, int64, int64, int64, string)       {}
func (r *supplyRepository) Save(_ context.Context, state *anchorcycle.State) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, _ := json.Marshal(state)
	var copy anchorcycle.State
	_ = json.Unmarshal(raw, &copy)
	r.state = &copy
	return nil
}
func TestFormalSupplySkipsVoiceFailuresAndDeliversNextWithoutCancel(t *testing.T) {
	state := cycleTestState(t)
	state.Surface = "formal_live"
	state.Config.TTS = &anchorcycle.VoiceConfig{Rate: 1, BufferSeconds: 20}
	state.Accepted = []anchorcycle.Segment{{Selected: anchorcycle.Candidate{ID: "bad", Text: "本段失败"}}, {Selected: anchorcycle.Candidate{ID: "good", Text: "下一段正常"}}}
	repo := &supplyRepository{state: &state}
	p := newCycleSupply(&Server{anchorCycles: repo}, context.Background(), &state)
	defer p.stop()
	p.synthesize = func(_ context.Context, _ *anchorcycle.State, id string) (*anchorcycle.VoiceAsset, error) {
		if id == "bad" {
			return nil, errors.New("TTS生成失败")
		}
		v := &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 1000}}
		repo.mu.Lock()
		repo.state.Accepted[1].Voice = v
		repo.mu.Unlock()
		return v, nil
	}
	ready := make(chan string, 1)
	p.onReady = func(_ context.Context, _ *anchorcycle.State, s anchorcycle.Segment) error {
		ready <- s.Selected.ID
		return nil
	}
	done := make(chan struct{})
	go func() { p.voiceLoop(); close(done) }()
	select {
	case id := <-ready:
		if id != "good" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("failed voice blocked next segment")
	}
	p.mu.Lock()
	failure := p.failure
	skipped := p.skippedVoices["bad"]
	p.mu.Unlock()
	if failure != "" || !skipped || p.ctx.Err() != nil {
		t.Fatal("segment failure stopped session", failure)
	}
	stock, _, err := p.stock(context.Background())
	if err != nil || stock.PendingCount != 0 {
		t.Fatal("failed voice kept stock permanently full", stock, err)
	}
	p.stop()
	<-done
}
func TestFormalSupplyTextFailureMovesToNextTask(t *testing.T) {
	state := cycleTestState(t)
	state.Surface = "formal_live"
	state.Config.Rolling = true
	state.Config.PointGap = 0
	state.Config.CombinationGap = 0
	state.Status = "content_blocked"
	state.Error = "本段审核失败"
	repo := &supplyRepository{state: &state}
	p := newCycleSupply(&Server{anchorCycles: repo}, context.Background(), &state)
	defer p.stop()
	continued := make(chan struct{})
	p.advance = func(ctx context.Context, s *anchorcycle.State) (*anchorcycle.State, error) {
		if s.Status != "ready" || len(s.GenerationSkips) != 1 {
			t.Error("blocked state not recovered")
		}
		close(continued)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan struct{})
	go func() { p.textLoop(); close(done) }()
	select {
	case <-continued:
	case <-time.After(time.Second):
		t.Fatal("review failure stopped text worker")
	}
	if p.ctx.Err() != nil {
		t.Fatal("formal worker cancelled")
	}
	p.stop()
	<-done
}
