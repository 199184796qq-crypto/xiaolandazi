package semantic

import (
	"strings"
	"sync"
	"time"
)

// Event is the minimal semantic input from the live room pipeline.
// Semantic processing is asynchronous and must not block collection.
type Event struct {
	RoomID   int64
	EventID  int64
	Content  string
	Source   string
	CreatedAt time.Time
}

// Result is the semantic trace output used by clustering, recall and RAG layers.
type Result struct {
	RoomID       int64
	EventID      int64
	Normalized  string
	ClusterKey   string
	Keywords     []string
	CreatedAt    time.Time
}

// Engine is the production semantic boundary.
// Embedding/vector providers can be plugged in later without changing Core.
type Engine struct {
	mu      sync.RWMutex
	traces  []Result
}

// TraceCount returns the amount of semantic observations retained in memory.
// It is used for diagnostics and later production metrics.
func (e *Engine) TraceCount() int {
	if e == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.traces)
}

func NewEngine() *Engine {
	return &Engine{traces: make([]Result, 0)}
}

func (e *Engine) Analyze(event Event) Result {
	text := strings.TrimSpace(event.Content)
	result := Result{
		RoomID:      event.RoomID,
		EventID:     event.EventID,
		Normalized: text,
		ClusterKey: buildClusterKey(text),
		CreatedAt:  time.Now().UTC(),
	}

	e.mu.Lock()
	e.traces = append(e.traces, result)
	e.mu.Unlock()

	return result
}

func (e *Engine) Recent(limit int) []Result {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if limit <= 0 || limit > len(e.traces) {
		limit = len(e.traces)
	}
	start := len(e.traces) - limit
	return append([]Result(nil), e.traces[start:]...)
}

func buildClusterKey(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return "empty"
	}
	if len(text) > 64 {
		return text[:64]
	}
	return text
}
