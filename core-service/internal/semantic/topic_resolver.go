package semantic

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	defaultTopicSimilarityThreshold = 0.84
	defaultTopicClusterLimit        = 64
)

type TopicResolution struct {
	ClusterKey     string  `json:"cluster_key"`
	CanonicalTopic string  `json:"canonical_topic"`
	Source         string  `json:"source"`
	Similarity     float64 `json:"similarity,omitempty"`
}

type TopicResolver interface {
	Resolve(context.Context, int64, string, string) TopicResolution
	ResetRoom(int64)
}

type topicCluster struct {
	Key       string
	Canonical string
	Vector    []float32
	Count     int
	LastSeen  time.Time
}

type ClusterTopicResolver struct {
	mu          sync.Mutex
	embedder    Embedder
	threshold   float64
	maxClusters int
	rooms       map[int64][]topicCluster
}

func NewTopicResolver(embedder Embedder) *ClusterTopicResolver {
	return &ClusterTopicResolver{
		embedder:    embedder,
		threshold:   defaultTopicSimilarityThreshold,
		maxClusters: defaultTopicClusterLimit,
		rooms:       make(map[int64][]topicCluster),
	}
}

func (r *ClusterTopicResolver) ResetRoom(roomID int64) {
	if r == nil || roomID <= 0 {
		return
	}
	r.mu.Lock()
	delete(r.rooms, roomID)
	r.mu.Unlock()
}

func (r *ClusterTopicResolver) Resolve(ctx context.Context, roomID int64, text, fallback string) TopicResolution {
	text = strings.TrimSpace(text)
	fallback = fallbackTopicKey(text, fallback)
	base := TopicResolution{
		ClusterKey:     fallback,
		CanonicalTopic: fallback,
		Source:         "rule",
	}
	if r == nil || roomID <= 0 || text == "" || r.embedder == nil || !r.embedder.Enabled() {
		return base
	}
	if ctx == nil {
		ctx = context.Background()
	}
	vectors, err := r.embedder.Embed(ctx, []string{text})
	if err != nil || len(vectors) != 1 || len(vectors[0]) == 0 {
		return base
	}
	vector := vectors[0]
	now := time.Now().UTC()

	r.mu.Lock()
	defer r.mu.Unlock()

	clusters := r.rooms[roomID]
	bestIndex := -1
	bestScore := 0.0
	for index := range clusters {
		score := CosineSimilarity(vector, clusters[index].Vector)
		if score > bestScore {
			bestScore = score
			bestIndex = index
		}
	}
	if bestIndex >= 0 && bestScore >= r.threshold {
		cluster := &clusters[bestIndex]
		cluster.Count++
		cluster.LastSeen = now
		if cluster.Count <= 4 {
			cluster.Vector = averageVectors(cluster.Vector, vector, cluster.Count)
		}
		r.rooms[roomID] = clusters
		return TopicResolution{
			ClusterKey:     cluster.Key,
			CanonicalTopic: cluster.Canonical,
			Source:         "vector",
			Similarity:     bestScore,
		}
	}

	cluster := topicCluster{
		Key:       semanticClusterKey(fallback, text),
		Canonical: fallback,
		Vector:    append([]float32(nil), vector...),
		Count:     1,
		LastSeen:  now,
	}
	clusters = append(clusters, cluster)
	if len(clusters) > r.maxClusters {
		oldest := 0
		for index := 1; index < len(clusters); index++ {
			if clusters[index].LastSeen.Before(clusters[oldest].LastSeen) {
				oldest = index
			}
		}
		clusters = append(clusters[:oldest], clusters[oldest+1:]...)
	}
	r.rooms[roomID] = clusters
	return TopicResolution{
		ClusterKey:     cluster.Key,
		CanonicalTopic: cluster.Canonical,
		Source:         "vector_new",
	}
}

func averageVectors(existing, incoming []float32, count int) []float32 {
	if len(existing) == 0 || len(existing) != len(incoming) || count <= 1 {
		return append([]float32(nil), incoming...)
	}
	previousWeight := float32(count - 1)
	currentWeight := float32(count)
	out := make([]float32, len(existing))
	for index := range existing {
		out[index] = (existing[index]*previousWeight + incoming[index]) / currentWeight
	}
	return out
}

func fallbackTopicKey(text, fallback string) string {
	fallback = strings.TrimSpace(strings.ToUpper(fallback))
	if fallback != "" && fallback != "INTERACTION:CHAT" && fallback != "CHAT" {
		return fallback
	}
	normalized := normalizeTopicText(text)
	if normalized == "" {
		return "INTERACTION:CHAT:GENERAL"
	}
	for _, family := range []struct {
		key      string
		keywords []string
	}{
		{"FAMILY:物流发货", []string{"快递", "物流", "发货", "包邮", "几天到", "多久到", "邮费"}},
		{"FAMILY:价格费用", []string{"多少钱", "价格", "价钱", "优惠", "便宜", "贵不贵", "活动价"}},
		{"FAMILY:购买下单", []string{"怎么买", "哪里买", "下单", "链接", "几号链接", "购物车"}},
		{"FAMILY:库存规格", []string{"库存", "还有吗", "规格", "尺寸", "型号", "几斤", "多少斤"}},
		{"FAMILY:售后服务", []string{"售后", "退货", "退款", "换货", "破损", "坏了"}},
		{"FAMILY:产品使用", []string{"怎么用", "怎么吃", "怎么做", "用法", "使用方法"}},
	} {
		for _, keyword := range family.keywords {
			if strings.Contains(normalized, keyword) {
				return family.key
			}
		}
	}
	return "CHAT:" + shortTopicHash(normalized)
}

func semanticClusterKey(fallback, text string) string {
	fallback = strings.TrimSpace(strings.ToUpper(fallback))
	if strings.HasPrefix(fallback, "SIGNAL:") {
		return fallback
	}
	return "SEM:" + shortTopicHash(fallback+"|"+normalizeTopicText(text))
}

func shortTopicHash(value string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return fmt.Sprintf("%016x", hash.Sum64())
}

func normalizeTopicText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}
