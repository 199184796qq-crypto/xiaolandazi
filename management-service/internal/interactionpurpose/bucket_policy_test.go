package interactionpurpose

import (
	"context"
	"encoding/json"
	"testing"

	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/semantic"
)

type bucketPolicyRepo struct {
	*bucketRepo
	reads, conclusions int
}

func (r *bucketPolicyRepo) ListPurposeBuckets(ctx context.Context, tenant, room, plan int64, limit int) ([]semantic.PurposeBucket, error) {
	r.reads++
	return r.bucketRepo.ListPurposeBuckets(ctx, tenant, room, plan, limit)
}

func (r *bucketPolicyRepo) GetPurposeBucketConclusion(ctx context.Context, tenant, room, plan int64, id string) (string, error) {
	r.conclusions++
	return r.bucketRepo.GetPurposeBucketConclusion(ctx, tenant, room, plan, id)
}

func TestBucketPolicyDefaultsOffAndExplicitOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "default_off"
		if enabled {
			name = "explicit_on"
		}
		t.Run(name, func(t *testing.T) {
			facts := "- [物流] 京东"
			cached := Result{Question: true, Relevant: true, Confidence: .96, Purpose: "了解快递公司", Scope: "当前油", FactsSHA: contentstrategy.SHA(facts), Input: "发哪个快递", Source: "model"}
			raw, err := json.Marshal(cached)
			if err != nil {
				t.Fatal(err)
			}
			repo := &bucketPolicyRepo{bucketRepo: &bucketRepo{
				buckets:  []semantic.PurposeBucket{{SourceID: "bucket:shipping", Question: "发哪个快递"}},
				conclMap: map[string]string{"bucket:shipping": string(raw)},
			}}
			judges, completes := 0, 0
			resolver := Resolver{Repo: repo, BucketEnabled: enabled,
				Judge: func(context.Context, string) (string, error) {
					judges++
					return `{"same":true,"match_index":0}`, nil
				},
				Complete: func(context.Context, string) (string, error) {
					completes++
					return modelResult(), nil
				},
			}
			got, err := resolver.Resolve(context.Background(), Input{Tenant: 14, Room: 29, Plan: 4, Text: "发哪个快递", Facts: facts, Prompt: "v1"})
			if err != nil {
				t.Fatal(err)
			}
			if !enabled {
				if got.Source != "model" || judges != 0 || completes != 1 || repo.reads != 0 || repo.conclusions != 0 || repo.upserted != 0 {
					t.Fatalf("disabled policy touched buckets: source=%s judge=%d complete=%d reads=%d conclusions=%d writes=%d", got.Source, judges, completes, repo.reads, repo.conclusions, repo.upserted)
				}
			} else if got.Source != "bucket" || judges != 1 || completes != 0 || repo.reads != 1 || repo.conclusions != 1 {
				t.Fatalf("explicit opt-in failed: source=%s judge=%d complete=%d reads=%d conclusions=%d", got.Source, judges, completes, repo.reads, repo.conclusions)
			}
		})
	}
}

func TestBucketPolicyEnabledMissStoresNewBucket(t *testing.T) {
	repo := &bucketRepo{}
	judges, completes := 0, 0
	resolver := Resolver{Repo: repo, BucketEnabled: true,
		Judge: func(context.Context, string) (string, error) { judges++; return `{"same":false}`, nil },
		Complete: func(context.Context, string) (string, error) {
			completes++
			return modelResult(), nil
		},
	}
	_, err := resolver.Resolve(context.Background(), Input{Tenant: 14, Room: 29, Plan: 4, Text: "发哪个快递", Facts: "- [物流] 京东", Prompt: "v1"})
	if err != nil || judges != 0 || completes != 1 || repo.upserted != 1 {
		t.Fatalf("enabled miss should store a bucket: judge=%d complete=%d writes=%d err=%v", judges, completes, repo.upserted, err)
	}
}

func TestBucketPolicyDisabledDirectWriteIsIgnored(t *testing.T) {
	repo := &bucketRepo{}
	resolver := Resolver{Repo: repo}
	resolver.storeBucket(context.Background(), Input{Tenant: 14, Room: 29, Plan: 4, Text: "发哪个快递"}, Result{Confidence: .96, Source: "model"})
	if repo.upserted != 0 {
		t.Fatal("disabled policy wrote a bucket")
	}
}
