package interactionpurpose

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/semantic"
	"testing"
)

type memoryRepo struct {
	documents map[string]semantic.StoredDocument
}

func docKey(tenant, room, plan int64, id string) string {
	return fmt.Sprintf("%d:%d:%d:%s", tenant, room, plan, id)
}
func (r *memoryRepo) GetInteractionPurpose(_ context.Context, t, room, p int64, id string) (string, error) {
	d, ok := r.documents[docKey(t, room, p, id)]
	if !ok {
		return "", semantic.ErrDocumentNotFound
	}
	return d.Text, nil
}
func (r *memoryRepo) UpsertSemanticDocument(_ context.Context, d semantic.StoredDocument) error {
	r.documents[docKey(d.TenantID, d.RoomID, d.PlanID, d.SourceID)] = d
	return nil
}
func (r *memoryRepo) GetSemanticDocument(_ context.Context, l semantic.Lookup) (semantic.StoredDocument, error) {
	return semantic.StoredDocument{}, semantic.ErrDocumentNotFound
}
func (r *memoryRepo) ListSemanticDocuments(_ context.Context, q semantic.Query) ([]semantic.StoredDocument, error) {
	result := []semantic.StoredDocument{}
	for _, d := range r.documents {
		if d.TenantID == q.TenantID && d.RoomID == q.RoomID && d.PlanID == q.PlanID && d.Model == q.Model {
			result = append(result, d)
		}
	}
	return result, nil
}
func (r *memoryRepo) ListPurposeBuckets(context.Context, int64, int64, int64, int) ([]semantic.PurposeBucket, error) {
	return nil, nil
}
func (r *memoryRepo) UpsertPurposeBucket(context.Context, int64, int64, int64, string, string, string) error {
	return nil
}
func (r *memoryRepo) GetPurposeBucketConclusion(context.Context, int64, int64, int64, string) (string, error) {
	return "", semantic.ErrDocumentNotFound
}

type constantEmbedder struct{ calls *int }

func (e constantEmbedder) Enabled() bool { return true }
func (e constantEmbedder) Model() string { return "test-vector" }
func (e constantEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	*e.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0}
	}
	return out, nil
}

func modelResult() string {
	return `{"question":true,"relevant":true,"confidence":0.96,"purpose_id":"","purpose":"了解快递公司","scope":"当前油","task_angles":[]}`
}
func TestExactPurposeKnowledgeReusesAcrossSessionsAndIsolatesScope(t *testing.T) {
	repo := &memoryRepo{documents: map[string]semantic.StoredDocument{}}
	calls := 0
	resolver := Resolver{Repo: repo, Complete: func(context.Context, string) (string, error) { calls++; return modelResult(), nil }}
	input := Input{Tenant: 14, Room: 15, Plan: 2, Text: "发哪个快递？", Facts: "- [物流] 京东", Prompt: "v1"}
	first, err := resolver.Resolve(context.Background(), input)
	if err != nil || first.Source != "model" {
		t.Fatal(first, err)
	}
	input.Text = "发哪个快递"
	second, err := resolver.Resolve(context.Background(), input)
	if err != nil || second.Source != "exact" || calls != 1 || second.Topic != first.Topic {
		t.Fatal("exact repeat called model", second, calls, err)
	}
	for _, different := range []Input{{Tenant: 13, Room: 15, Plan: 2, Text: input.Text, Facts: input.Facts, Prompt: input.Prompt}, {Tenant: 14, Room: 16, Plan: 2, Text: input.Text, Facts: input.Facts, Prompt: input.Prompt}, {Tenant: 14, Room: 15, Plan: 4, Text: input.Text, Facts: input.Facts, Prompt: input.Prompt}, {Tenant: 14, Room: 15, Plan: 2, Text: input.Text, Facts: "- [物流] 已变化", Prompt: input.Prompt}} {
		if _, err := resolver.Resolve(context.Background(), different); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 5 {
		t.Fatal("cross-scope knowledge reused", calls)
	}
}
func TestSemanticReuseRequiresConfidenceMarginAndPrecisePurposeFamily(t *testing.T) {
	repo := &memoryRepo{documents: map[string]semantic.StoredDocument{}}
	embedCalls, modelCalls := 0, 0
	sem := semantic.NewService(constantEmbedder{&embedCalls}, repo)
	resolver := Resolver{Repo: repo, Semantic: sem, Complete: func(context.Context, string) (string, error) { modelCalls++; return modelResult(), nil }}
	input := Input{Tenant: 14, Room: 15, Plan: 2, Text: "发哪个快递", Facts: "- [物流] 京东", Prompt: "v1"}
	first, err := resolver.Resolve(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	input.Text = "用哪个快递"
	reused, err := resolver.Resolve(context.Background(), input)
	if err != nil || reused.Source != "semantic" || modelCalls != 1 || embedCalls == 0 {
		t.Fatal("high confidence reuse failed", reused, err, modelCalls)
	}
	competitor := first
	competitor.Topic = "PURPOSE:other"
	competitor.Input = "什么快递"
	raw, _ := json.Marshal(competitor)
	repo.UpsertSemanticDocument(context.Background(), semantic.StoredDocument{Document: semantic.Document{TenantID: 14, RoomID: 15, PlanID: 2, ContentType: ContentType, SourceID: "competitor", Text: string(raw), Status: "active"}, Model: "test-vector", Vector: []float32{1, 0}})
	input.Text = "哪家快递"
	ambiguous, err := resolver.Resolve(context.Background(), input)
	if err != nil || ambiguous.Source != "model" || modelCalls != 2 {
		t.Fatal("similar purposes were hard matched", ambiguous, err)
	}
	input.Text = "哪里发货"
	other, err := resolver.Resolve(context.Background(), input)
	if err != nil || other.Source != "model" {
		t.Fatal("courier reused for shipment origin", other, err)
	}
}
func TestMalformedAndLowConfidencePurposesAreNotPersisted(t *testing.T) {
	for _, raw := range []string{`{"question":true,"relevant":true,"confidence":0.4,"purpose":"快递","purpose_id":"","scope":"","task_angles":[]}`, `{"question":true,"relevant":true,"confidence":1,"purpose":"快递","purpose_id":"unknown","scope":"","task_angles":[]}`, `{"question":true,"relevant":true,"confidence":1,"purpose":"快递","purpose_id":"","scope":"","task_angles":[],"invented":1}`} {
		repo := &memoryRepo{documents: map[string]semantic.StoredDocument{}}
		resolver := Resolver{Repo: repo, Complete: func(context.Context, string) (string, error) { return raw, nil }}
		if _, err := resolver.Resolve(context.Background(), Input{Tenant: 1, Room: 1, Plan: 1, Text: "快递？", Facts: "- [物流] 京东"}); err == nil || len(repo.documents) > 0 {
			t.Fatal("invalid knowledge saved", raw, err)
		}
	}
}
