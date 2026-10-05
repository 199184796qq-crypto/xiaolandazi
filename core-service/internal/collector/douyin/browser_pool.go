package douyin

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/model"
)

var ErrCollectorCapacity = errors.New("collector worker pool capacity reached")

type browserRuntime interface {
	StartRoom(context.Context, model.Room) (*BrowserSession, error)
	RequestPreview(context.Context, int64) ([]byte, string, error)
	RequestStream(context.Context, int64, string) (collector.StreamSource, error)
	Stream(int64) (collector.StreamSource, error)
}

type BrowserPool struct {
	mu         sync.Mutex
	workers    []*BrowserManager
	maxPerWork int
	roomWorker map[int64]int
	counts     []int
}

func NewBrowserPool(workerCount, maxRoomsPerWorker int, configuredPath string, headless bool) *BrowserPool {
	if workerCount <= 0 {
		workerCount = 1
	}
	if maxRoomsPerWorker <= 0 {
		maxRoomsPerWorker = 20
	}
	pool := &BrowserPool{
		workers:    make([]*BrowserManager, workerCount),
		maxPerWork: maxRoomsPerWorker,
		roomWorker: make(map[int64]int),
		counts:     make([]int, workerCount),
	}
	for i := range pool.workers {
		pool.workers[i] = NewBrowserManager(configuredPath, headless)
	}
	return pool
}

func (p *BrowserPool) StartRoom(ctx context.Context, room model.Room) (*BrowserSession, error) {
	workerIndex, err := p.assign(room)
	if err != nil {
		return nil, err
	}
	session, err := p.workers[workerIndex].StartRoom(ctx, room)
	if err != nil {
		p.release(room.ID, workerIndex)
		return nil, err
	}
	session.onClose = func() { p.release(room.ID, workerIndex) }
	return session, nil
}

func (p *BrowserPool) RequestPreview(ctx context.Context, roomID int64) ([]byte, string, error) {
	worker, ok := p.workerForRoom(roomID)
	if !ok {
		return nil, "", errors.New("room collector session is not active")
	}
	return worker.RequestPreview(ctx, roomID)
}

func (p *BrowserPool) Stream(roomID int64) (collector.StreamSource, error) {
	worker, ok := p.workerForRoom(roomID)
	if !ok {
		return collector.StreamSource{}, errors.New("room collector session is not active")
	}
	return worker.Stream(roomID)
}

func (p *BrowserPool) RequestStream(
	ctx context.Context,
	roomID int64,
	url string,
) (collector.StreamSource, error) {
	worker, ok := p.workerForRoom(roomID)
	if !ok {
		return collector.StreamSource{}, errors.New("room collector session is not active")
	}
	return worker.RequestStream(ctx, roomID, url)
}

func (p *BrowserPool) Close() {
	for _, worker := range p.workers {
		worker.Close()
	}
}

func (p *BrowserPool) Capacity() int {
	return len(p.workers) * p.maxPerWork
}

func (p *BrowserPool) assign(room model.Room) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if index, ok := p.roomWorker[room.ID]; ok {
		return index, nil
	}

	start := stableWorkerIndex(room.TenantID, room.ID, len(p.workers))
	for offset := 0; offset < len(p.workers); offset++ {
		index := (start + offset) % len(p.workers)
		if p.counts[index] >= p.maxPerWork {
			continue
		}
		p.roomWorker[room.ID] = index
		p.counts[index]++
		return index, nil
	}
	return 0, fmt.Errorf("%w: workers=%d max_per_worker=%d", ErrCollectorCapacity, len(p.workers), p.maxPerWork)
}

func (p *BrowserPool) release(roomID int64, expectedWorker int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	index, ok := p.roomWorker[roomID]
	if !ok || index != expectedWorker {
		return
	}
	delete(p.roomWorker, roomID)
	if p.counts[index] > 0 {
		p.counts[index]--
	}
}

func (p *BrowserPool) workerForRoom(roomID int64) (*BrowserManager, bool) {
	p.mu.Lock()
	index, ok := p.roomWorker[roomID]
	p.mu.Unlock()
	if !ok || index < 0 || index >= len(p.workers) {
		return nil, false
	}
	return p.workers[index], true
}

func stableWorkerIndex(tenantID, roomID int64, count int) int {
	if count <= 1 {
		return 0
	}
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "%d:%d", tenantID, roomID)
	return int(hash.Sum64() % uint64(count))
}
