package semantic

import (
	"sort"
	"strings"
	"sync"
)

// ClusterStore keeps lightweight semantic groups before vector storage is enabled.
// It provides a deterministic fallback for question grouping and later RAG recall.
type ClusterStore struct {
	mu       sync.RWMutex
	clusters map[string][]Result
}

func NewClusterStore() *ClusterStore {
	return &ClusterStore{clusters: make(map[string][]Result)}
}

func (s *ClusterStore) Add(result Result) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(result.ClusterKey)
	if key == "" {
		key = "empty"
	}
	s.clusters[key] = append(s.clusters[key], result)
}

// Recall returns the closest deterministic semantic matches.
// Vector providers can replace this implementation without changing callers.
func (s *ClusterStore) Recall(clusterKey string, limit int) []Result {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]Result(nil), s.clusters[strings.TrimSpace(clusterKey)]...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}
