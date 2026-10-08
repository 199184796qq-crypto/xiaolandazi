package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/productionbuffer"
	"strings"
	"sync"
	"time"
	"unicode"
)

type productionGateKey struct{}

func waitProductionGate(ctx context.Context) error {
	if gate, ok := ctx.Value(productionGateKey{}).(func(context.Context) error); ok {
		return gate(ctx)
	}
	return ctx.Err()
}

type cycleSupply struct {
	s                *Server
	ctx              context.Context
	cancel           context.CancelFunc
	initial          *anchorcycle.State
	policy           *productionbuffer.Controller
	guard            func(context.Context) (bool, error)
	externalStock    func(context.Context) (int64, error)
	beforeText       func(context.Context, *anchorcycle.State) error
	onReady          func(context.Context, *anchorcycle.State, anchorcycle.Segment) error
	startup          func(context.Context, string)
	mu               sync.Mutex
	state            *anchorcycle.State
	paused, finished bool
	failure          string
	reserved         int64
	playedMS         float64
	delivered        map[string]bool
	started          map[string]time.Time
	done             chan struct{}
	workers          sync.WaitGroup
	progress         int
	progressPart     string
	startupPercent   int
	advance          func(context.Context, *anchorcycle.State) (*anchorcycle.State, error)
	synthesize       func(context.Context, *anchorcycle.State, string) (*anchorcycle.VoiceAsset, error)
}

func newCycleSupply(s *Server, parent context.Context, state *anchorcycle.State) *cycleSupply {
	ctx, cancel := context.WithCancel(parent)
	high := int64(120_000)
	if state.Config.TTS != nil {
		high = int64(state.Config.TTS.BufferSeconds) * 1000
	}
	p := &cycleSupply{s: s, ctx: ctx, cancel: cancel, initial: state, policy: productionbuffer.New(high, state.StreamControlled), delivered: map[string]bool{}, started: map[string]time.Time{}, done: make(chan struct{})}
	p.ctx = context.WithValue(ctx, productionGateKey{}, p.wait)
	p.advance = func(ctx context.Context, current *anchorcycle.State) (*anchorcycle.State, error) {
		return s.advanceAnchorCycle(ctx, current.TenantID, current.PlanID, current.RoomID, current.RunID, current.Revision)
	}
	p.synthesize = func(ctx context.Context, current *anchorcycle.State, id string) (*anchorcycle.VoiceAsset, error) {
		return s.synthesizeAnchorCycleVoice(ctx, current, id, false)
	}
	p.receive(state)
	return p
}
func (p *cycleSupply) receive(state *anchorcycle.State) {
	raw, _ := json.Marshal(state)
	var copy anchorcycle.State
	_ = json.Unmarshal(raw, &copy)
	p.mu.Lock()
	p.state = &copy
	p.mu.Unlock()
}
func (p *cycleSupply) snapshot() *anchorcycle.State { p.mu.Lock(); defer p.mu.Unlock(); return p.state }
func (p *cycleSupply) stop() {
	p.mu.Lock()
	p.paused, p.finished = true, true
	p.mu.Unlock()
	p.cancel()
}
func (p *cycleSupply) pause(value bool) { p.mu.Lock(); p.paused = value; p.mu.Unlock() }
func (p *cycleSupply) fail(err error) {
	if err == nil || p.ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	if p.failure == "" {
		p.failure = err.Error()
	}
	p.paused = true
	p.mu.Unlock()
	p.cancel()
}
func (p *cycleSupply) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.mu.Lock()
		paused, finished := p.paused, p.finished
		p.mu.Unlock()
		if finished {
			return context.Canceled
		}
		if !paused && p.guard != nil {
			var err error
			paused, err = p.guard(ctx)
			if err != nil {
				return err
			}
		}
		if !paused {
			return nil
		}
		if err := supplyWait(ctx, 250*time.Millisecond); err != nil {
			return err
		}
	}
}
func supplyWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func spokenCount(text string) int64 {
	var count int64
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			count++
		}
	}
	return count
}
func (p *cycleSupply) stock(ctx context.Context) (productionbuffer.Stock, int64, error) {
	state := p.snapshot()
	if state == nil {
		return productionbuffer.Stock{}, 0, anchorcycle.ErrMissing
	}
	p.mu.Lock()
	reserved, played := p.reserved, p.playedMS
	delivered := map[string]bool{}
	for id, v := range p.delivered {
		delivered[id] = v
	}
	p.mu.Unlock()
	units, ms := int64(0), int64(0)
	for _, segment := range state.Accepted {
		if segment.Voice != nil && segment.Voice.Status == "ready" {
			units += spokenCount(segment.Voice.Bundle.Text)
			ms += segment.Voice.Bundle.AudioDurationMS
		}
	}
	speed := float64(4)
	if state.Config.TTS != nil {
		speed *= state.Config.TTS.Rate
	}
	if ms > 0 && units > 0 {
		speed = max(2, min(8, float64(units)*1000/float64(ms)))
	}
	stock := productionbuffer.Stock{ReservedMS: reserved}
	for _, segment := range state.Accepted {
		if delivered[segment.Selected.ID] {
			continue
		}
		if segment.Voice != nil && segment.Voice.Status == "ready" {
			stock.ReadyMS += segment.Voice.Bundle.AudioDurationMS
		} else {
			stock.PendingCount++
			stock.PendingMS += int64(float64(spokenCount(cycleVoiceText(segment.Selected))) * 1000 / speed)
		}
	}
	remaining := int64(0)
	if state.StreamControlled {
		stock.ReadyMS = max(0, stock.ReadyMS-int64(played))
		if state.PreviewLimitMS > 0 {
			remaining = max(1, state.PreviewLimitMS-int64(played))
		}
	}
	if p.externalStock != nil {
		external, err := p.externalStock(ctx)
		if err != nil {
			return stock, remaining, err
		}
		stock.ReadyMS += external
	}
	return stock, remaining, nil
}
func (p *cycleSupply) start() {
	p.workers.Add(2)
	go func() { defer p.workers.Done(); p.textLoop() }()
	go func() { defer p.workers.Done(); p.voiceLoop() }()
	go func() { p.workers.Wait(); close(p.done) }()
}
func (p *cycleSupply) progressContext(part string) context.Context {
	return context.WithValue(p.ctx, cycleProgressKey{}, cycleProgressReporter(func(percent int) {
		p.mu.Lock()
		if p.progressPart != part || percent > p.progress {
			p.progress, p.progressPart = percent, part
		}
		p.mu.Unlock()
		if part == "text" && percent >= 60 {
			p.reportStartup("checking_content")
		}
	}))
}
func (p *cycleSupply) reportStartup(stage string) {
	percent := anchorcycle.StartupPercent(stage)
	if percent == 0 {
		return
	}
	p.mu.Lock()
	if percent <= p.startupPercent {
		p.mu.Unlock()
		return
	}
	p.startupPercent = percent
	report := p.startup
	p.mu.Unlock()
	if report != nil {
		report(p.ctx, stage)
	}
}
func (p *cycleSupply) load() (*anchorcycle.State, error) {
	state, err := p.s.anchorCycles.Get(p.ctx, p.initial.TenantID, p.initial.PlanID, p.initial.RoomID)
	if err == nil && state.RunID != p.initial.RunID {
		err = anchorcycle.ErrStale
	}
	return state, err
}
func (p *cycleSupply) textLoop() {
	for p.ctx.Err() == nil {
		if err := p.wait(p.ctx); err != nil {
			p.fail(err)
			return
		}
		stock, remaining, err := p.stock(p.ctx)
		if err != nil {
			p.fail(err)
			return
		}
		if !p.policy.ShouldGenerate(stock, remaining) {
			if supplyWait(p.ctx, 300*time.Millisecond) != nil {
				return
			}
			continue
		}
		state, err := p.load()
		if err != nil {
			p.fail(err)
			return
		}
		if state.Uncertain || state.Phase == "ended" || state.Status == "failed" || strings.HasSuffix(state.Status, "_blocked") {
			p.fail(errors.New("本场生成暂停：" + state.Error))
			return
		}
		if p.beforeText != nil {
			if err = p.beforeText(p.ctx, state); err != nil {
				p.fail(err)
				return
			}
		}
		before := len(state.Accepted)
		started := time.Now()
		p.mu.Lock()
		p.reserved = int64(max(80, state.Config.MaxChars)) * 250
		p.mu.Unlock()
		p.reportStartup("preparing_content")
		latest, err := p.advance(p.progressContext("text"), state)
		p.mu.Lock()
		p.reserved = 0
		if err == nil && latest != nil && len(latest.Accepted) >= before {
			for _, segment := range latest.Accepted[before:] {
				p.started[segment.Selected.ID] = started
			}
		}
		p.mu.Unlock()
		if err != nil {
			p.fail(err)
			return
		}
		p.receive(latest)
	}
}
func (p *cycleSupply) voiceLoop() {
	for p.ctx.Err() == nil {
		if err := p.wait(p.ctx); err != nil {
			p.fail(err)
			return
		}
		state, err := p.load()
		if err != nil {
			p.fail(err)
			return
		}
		var next *anchorcycle.Segment
		p.receive(state)
		for i := range state.Accepted {
			segment := &state.Accepted[i]
			if segment.Voice == nil || segment.Voice.Status != "ready" {
				next = segment
				break
			}
		}
		if next == nil {
			if supplyWait(p.ctx, 250*time.Millisecond) != nil {
				return
			}
			continue
		}
		if next.Voice != nil && next.Voice.Status == "failed" {
			p.fail(errors.New(next.Voice.Error))
			return
		}
		p.reportStartup("generating_voice")
		voice, err := p.synthesize(p.progressContext("voice"), state, next.Selected.ID)
		if err != nil {
			p.fail(err)
			return
		}
		if voice == nil || voice.Status != "ready" {
			p.fail(errors.New("声音未完整生成，后台供料已暂停"))
			return
		}
		p.reportStartup("voice_ready")
		next.Voice = voice
		p.mu.Lock()
		started := p.started[next.Selected.ID]
		delete(p.started, next.Selected.ID)
		p.mu.Unlock()
		if !started.IsZero() {
			p.policy.Observe(time.Since(started).Milliseconds(), voice.Bundle.AudioDurationMS)
		}
		if err = p.wait(p.ctx); err != nil {
			return
		}
		if p.onReady != nil {
			if err = p.onReady(p.ctx, state, *next); err != nil {
				p.fail(err)
				return
			}
			p.mu.Lock()
			p.delivered[next.Selected.ID] = true
			p.mu.Unlock()
		}
		latest, err := p.load()
		if err != nil {
			p.fail(err)
			return
		}
		p.receive(latest)
	}
}
