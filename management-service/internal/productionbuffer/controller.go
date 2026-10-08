package productionbuffer

import (
	"math"
	"sort"
	"sync"
)

// Content inventory is separate from the receiver's short PCM jitter queue.
// Pending text and its active TTS are one unit, never two reservations.
type Stock struct {
	ReadyMS, PendingMS, ReservedMS int64
	PendingCount                   int
}

func (s Stock) ProjectedMS() int64 {
	return max(0, s.ReadyMS) + max(0, s.PendingMS) + max(0, s.ReservedMS)
}

type Thresholds struct{ LowMS, HighMS int64 }
type Controller struct {
	mu            sync.Mutex
	high          int64
	preview       bool
	filling       bool
	refill, clips []int64
}

func New(highMS int64, preview bool) *Controller {
	return &Controller{high: max(20_000, highMS), preview: preview, filling: true}
}
func (c *Controller) Observe(refillMS, audioMS int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if refillMS > 0 {
		c.refill = append(c.refill, refillMS)
	}
	if audioMS > 0 {
		c.clips = append(c.clips, audioMS)
	}
	if len(c.refill) > 32 {
		c.refill = c.refill[len(c.refill)-32:]
	}
	if len(c.clips) > 32 {
		c.clips = c.clips[len(c.clips)-32:]
	}
}
func percentile(values []int64, p float64, fallback int64) int64 {
	if len(values) == 0 {
		return fallback
	}
	copy := append([]int64(nil), values...)
	sort.Slice(copy, func(i, j int) bool { return copy[i] < copy[j] })
	return copy[max(0, int(math.Ceil(float64(len(copy))*p))-1)]
}
func (c *Controller) thresholds(remainingMS int64) Thresholds {
	high := c.high
	clip := percentile(c.clips, .5, 25_000)
	low := min(high-10_000, max(15_000, percentile(c.refill, .95, high*2/3)+5_000))
	if c.preview {
		// Preserve complete generated units, but do not build a whole formal
		// broadcast's inventory for a preview that is about to be paused.
		high = min(high, max(25_000, clip*2))
		low = min(low, max(15_000, high-clip))
		if remainingMS > 0 {
			high = min(high, max(5_000, remainingMS))
			low = min(low, high)
		}
	}
	return Thresholds{LowMS: low, HighMS: high}
}
func (c *Controller) Thresholds(remainingMS int64) Thresholds {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.thresholds(remainingMS)
}
func (c *Controller) ShouldGenerate(stock Stock, remainingMS int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.thresholds(remainingMS)
	if stock.ProjectedMS() >= t.HighMS {
		c.filling = false
	}
	if stock.ReadyMS < t.LowMS && stock.ProjectedMS() < t.HighMS {
		c.filling = true
	}
	// One active text job and at most two adopted units awaiting sound.
	return c.filling && stock.PendingCount < 2 && stock.ReservedMS == 0
}
