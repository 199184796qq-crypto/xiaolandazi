package semantic

import (
	"context"
	"testing"
)

type fakeTopicEmbedder struct {
	vectors map[string][]float32
}

func (f fakeTopicEmbedder) Enabled() bool { return true }
func (f fakeTopicEmbedder) Model() string { return "fake" }
func (f fakeTopicEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = append([]float32(nil), f.vectors[text]...)
	}
	return out, nil
}

func TestTopicResolverFallbackSeparatesUnrelatedChat(t *testing.T) {
	resolver := NewTopicResolver(nil)
	price := resolver.Resolve(context.Background(), 1, "这个多少钱", "INTERACTION:CHAT")
	logistics := resolver.Resolve(context.Background(), 1, "发什么快递", "INTERACTION:CHAT")
	if price.ClusterKey == logistics.ClusterKey {
		t.Fatalf("unrelated chat must not share cooldown topic: price=%s logistics=%s", price.ClusterKey, logistics.ClusterKey)
	}
	if price.ClusterKey != "FAMILY:价格费用" {
		t.Fatalf("price topic=%s", price.ClusterKey)
	}
	if logistics.ClusterKey != "FAMILY:物流发货" {
		t.Fatalf("logistics topic=%s", logistics.ClusterKey)
	}
}

func TestTopicResolverVectorMergesSemanticParaphrases(t *testing.T) {
	resolver := NewTopicResolver(fakeTopicEmbedder{vectors: map[string][]float32{
		"怎么购买":  {1, 0},
		"从哪里下单": {0.99, 0.01},
	}})
	first := resolver.Resolve(context.Background(), 2, "怎么购买", "INTERACTION:CHAT")
	second := resolver.Resolve(context.Background(), 2, "从哪里下单", "INTERACTION:CHAT")
	if first.ClusterKey != second.ClusterKey {
		t.Fatalf("semantic paraphrases should share cluster: first=%s second=%s", first.ClusterKey, second.ClusterKey)
	}
	if second.Source != "vector" {
		t.Fatalf("second source=%s", second.Source)
	}
}
