package semantic

import (
    "strings"
    "sync"
)

// Runtime connects semantic tracing, clustering and recall without forcing
// embedding availability. It is the production boundary for future RAG flow.
type Runtime struct {
    mu sync.RWMutex
    engine *Engine
    clusters *ClusterStore
    memory map[string][]string
}

func NewRuntime() *Runtime {
    return &Runtime{
        engine: NewEngine(),
        clusters: NewClusterStore(),
        memory: make(map[string][]string),
    }
}

// Observe records live data and returns a stable topic used by queue/RAG.
func (r *Runtime) Observe(event Event) Result {
    result := r.engine.Analyze(event)
    r.clusters.Add(result)
    return result
}

// Remember stores verified answers/facts before vector persistence is enabled.
func (r *Runtime) Remember(topic, text string) {
    if r == nil { return }
    topic = strings.TrimSpace(topic)
    text = strings.TrimSpace(text)
    if topic == "" || text == "" { return }
    r.mu.Lock()
    defer r.mu.Unlock()
    r.memory[topic] = append(r.memory[topic], text)
}

// RecallMemory provides deterministic fallback memory recall for RAG.
func (r *Runtime) RecallMemory(topic string) []string {
    if r == nil { return nil }
    r.mu.RLock()
    defer r.mu.RUnlock()
    return append([]string(nil), r.memory[strings.TrimSpace(topic)]...)
}

func (r *Runtime) Recall(topic string, limit int) []Result {
    if r == nil { return nil }
    return r.clusters.Recall(topic, limit)
}
