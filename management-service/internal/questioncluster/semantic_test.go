package questioncluster

import (
	"context"
	"testing"
)

type fakeClusterEmbedder struct {
	vectors [][]float32
	err     error
}

func (f fakeClusterEmbedder) Enabled() bool { return true }
func (f fakeClusterEmbedder) Model() string { return "fake" }
func (f fakeClusterEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return f.vectors, f.err
}

func TestSemanticCandidateTopicsKeepsOnlyRelatedPairs(t *testing.T) {
	topics := []topicBucket{
		{Topic: "Q:多少钱", SampleQuestions: []string{"这个多少钱"}},
		{Topic: "Q:什么价", SampleQuestions: []string{"今天什么价格"}},
		{Topic: "Q:发什么快递", SampleQuestions: []string{"物流是哪家"}},
	}
	eligible := map[string]struct{}{
		"Q:多少钱":   {},
		"Q:什么价":   {},
		"Q:发什么快递": {},
	}
	embedder := fakeClusterEmbedder{vectors: [][]float32{
		{1, 0},
		{0.99, 0.01},
		{0, 1},
	}}
	got, err := semanticCandidateTopics(context.Background(), embedder, topics, eligible)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("candidate count=%d want=2; got=%v", len(got), got)
	}
	if _, ok := got["Q:多少钱"]; !ok {
		t.Fatal("price bucket must remain a candidate")
	}
	if _, ok := got["Q:什么价"]; !ok {
		t.Fatal("similar price bucket must remain a candidate")
	}
	if _, ok := got["Q:发什么快递"]; ok {
		t.Fatal("unrelated shipping bucket should be removed from LLM merge candidates")
	}
}

func TestSemanticCandidateTopicsDisabledFallsBack(t *testing.T) {
	got, err := semanticCandidateTopics(context.Background(), nil, nil, map[string]struct{}{"Q:a": {}, "Q:b": {}})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("disabled semantic prefilter should return nil fallback, got=%v", got)
	}
}
