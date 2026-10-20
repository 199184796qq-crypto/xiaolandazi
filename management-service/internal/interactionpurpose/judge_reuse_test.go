package interactionpurpose

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/semantic"
	"testing"
)

// bucketRepo 是带语义桶数据的 memoryRepo，用于测试 judgeReuse。
type bucketRepo struct {
	*testing.T
	buckets  []semantic.PurposeBucket
	conclMap map[string]string
	upserted int
}

func (r *bucketRepo) GetInteractionPurpose(context.Context, int64, int64, int64, string) (string, error) {
	return "", semantic.ErrDocumentNotFound
}
func (r *bucketRepo) UpsertSemanticDocument(context.Context, semantic.StoredDocument) error { return nil }
func (r *bucketRepo) GetSemanticDocument(context.Context, semantic.Lookup) (semantic.StoredDocument, error) {
	return semantic.StoredDocument{}, semantic.ErrDocumentNotFound
}
func (r *bucketRepo) ListSemanticDocuments(context.Context, semantic.Query) ([]semantic.StoredDocument, error) {
	return nil, nil
}
func (r *bucketRepo) ListPurposeBuckets(context.Context, int64, int64, int64, int) ([]semantic.PurposeBucket, error) {
	return r.buckets, nil
}
func (r *bucketRepo) UpsertPurposeBucket(context.Context, int64, int64, int64, string, string, string) error {
	r.upserted++
	return nil
}
func (r *bucketRepo) GetPurposeBucketConclusion(_ context.Context, _ int64, _ int64, _ int64, id string) (string, error) {
	c, ok := r.conclMap[id]
	if !ok {
		return "", semantic.ErrDocumentNotFound
	}
	return c, nil
}

func bucketConclusion(factsSHA string) string {
	c := Result{
		Question: true, Relevant: true, Confidence: 0.96,
		Purpose: "了解套餐", Scope: "当前油", FactsSHA: factsSHA,
		Topic: "PURPOSE:套餐", Source: "model", Input: "几个人吃哪个套餐",
	}
	raw, _ := json.Marshal(c)
	return string(raw)
}

func factsA() string { return "- [套餐] 4人餐299元" }
func factsB() string { return "- [套餐] 8人餐499元" }

// 场景1：同类问题命中（same=true + 合法 match_index + FactsSHA 一致）→ 复用桶结论。
func TestJudgeReuseHitsMatchingBucket(t *testing.T) {
	fa := contentstrategy.SHA(factsA())
	repo := &bucketRepo{
		buckets:  []semantic.PurposeBucket{{SourceID: "bucket:a", Question: "几个人吃哪个套餐"}},
		conclMap: map[string]string{"bucket:a": bucketConclusion(fa)},
	}
	resolver := Resolver{
		Repo: repo,
		Judge: func(context.Context, string) (string, error) {
			return `{"same":true,"match_index":0}`, nil
		},
	}
	got, ok := resolver.judgeReuse(context.Background(), Input{Tenant: 1, Room: 1, Plan: 1, Text: "我们4个人吃哪个", Facts: factsA()}, fa)
	if !ok || got.Source != "bucket" || got.Purpose != "了解套餐" {
		t.Fatalf("同类问题应复用桶结论，got=%+v ok=%v", got, ok)
	}
}

// 场景2（最关键）：事实包变了（不同商品/不同人数套餐）→ FactsSHA 不一致 → 绝不复用。
func TestJudgeReuseRejectsWhenFactsChanged(t *testing.T) {
	fa := contentstrategy.SHA(factsA())
	fb := contentstrategy.SHA(factsB())
	if fa == fb {
		t.Fatal("测试前提：两个事实包 SHA 必须不同")
	}
	repo := &bucketRepo{
		buckets:  []semantic.PurposeBucket{{SourceID: "bucket:a", Question: "几个人吃哪个套餐"}},
		conclMap: map[string]string{"bucket:a": bucketConclusion(fa)},
	}
	resolver := Resolver{
		Repo: repo,
		Judge: func(context.Context, string) (string, error) {
			return `{"same":true,"match_index":0}`, nil
		},
	}
	// 新问题换了人数（4人→8人），事实包随之变化
	got, ok := resolver.judgeReuse(context.Background(), Input{Tenant: 1, Room: 1, Plan: 1, Text: "我们8个人吃哪个", Facts: factsB()}, fb)
	if ok {
		t.Fatalf("事实包变了绝不应复用旧结论，got=%+v", got)
	}
}

// 场景3：判断模型 same=true 但 match_index 缺失（nil）→ 不默认命中第 0 桶。
func TestJudgeReuseRejectsMissingMatchIndex(t *testing.T) {
	fa := contentstrategy.SHA(factsA())
	repo := &bucketRepo{
		buckets:  []semantic.PurposeBucket{{SourceID: "bucket:a", Question: "几个人吃哪个套餐"}},
		conclMap: map[string]string{"bucket:a": bucketConclusion(fa)},
	}
	resolver := Resolver{
		Repo: repo,
		Judge: func(context.Context, string) (string, error) {
			return `{"same":true}`, nil // 缺 match_index
		},
	}
	if got, ok := resolver.judgeReuse(context.Background(), Input{Tenant: 1, Room: 1, Plan: 1, Text: "我们4个人吃哪个", Facts: factsA()}, fa); ok {
		t.Fatalf("缺 match_index 不应命中第0桶，got=%+v", got)
	}
}

// 场景4：match_index 越界（>= 桶数）→ 拒绝命中。
func TestJudgeReuseRejectsOutOfRangeMatchIndex(t *testing.T) {
	fa := contentstrategy.SHA(factsA())
	repo := &bucketRepo{
		buckets:  []semantic.PurposeBucket{{SourceID: "bucket:a", Question: "几个人吃哪个套餐"}},
		conclMap: map[string]string{"bucket:a": bucketConclusion(fa)},
	}
	resolver := Resolver{
		Repo: repo,
		Judge: func(context.Context, string) (string, error) {
			return `{"same":true,"match_index":3}`, nil // 只有 1 个桶
		},
	}
	if got, ok := resolver.judgeReuse(context.Background(), Input{Tenant: 1, Room: 1, Plan: 1, Text: "我们4个人吃哪个", Facts: factsA()}, fa); ok {
		t.Fatalf("越界 match_index 不应命中，got=%+v", got)
	}
}

// 场景5：判断模型 same=false（不同类别）→ 不复用。
func TestJudgeReuseRejectsWhenNotSame(t *testing.T) {
	fa := contentstrategy.SHA(factsA())
	repo := &bucketRepo{
		buckets:  []semantic.PurposeBucket{{SourceID: "bucket:a", Question: "几个人吃哪个套餐"}},
		conclMap: map[string]string{"bucket:a": bucketConclusion(fa)},
	}
	resolver := Resolver{
		Repo: repo,
		Judge: func(context.Context, string) (string, error) {
			return `{"same":false}`, nil
		},
	}
	if got, ok := resolver.judgeReuse(context.Background(), Input{Tenant: 1, Room: 1, Plan: 1, Text: "你们店在哪", Facts: factsA()}, fa); ok {
		t.Fatalf("不同类别不应复用，got=%+v", got)
	}
}

// 场景6：无历史桶 → 直接不复用（不调判断模型）。
func TestJudgeReuseSkipsWhenNoBuckets(t *testing.T) {
	fa := contentstrategy.SHA(factsA())
	repo := &bucketRepo{buckets: nil, conclMap: map[string]string{}}
	judgeCalled := false
	resolver := Resolver{
		Repo: repo,
		Judge: func(context.Context, string) (string, error) {
			judgeCalled = true
			return `{"same":true,"match_index":0}`, nil
		},
	}
	if _, ok := resolver.judgeReuse(context.Background(), Input{Tenant: 1, Room: 1, Plan: 1, Text: "套餐", Facts: factsA()}, fa); ok {
		t.Fatal("无历史桶不应命中")
	}
	if judgeCalled {
		t.Fatal("无历史桶不应调用判断模型")
	}
}
