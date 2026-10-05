package db

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func TestSpeechModelsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	t.Setenv("MODEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("isolated-fixture-", 3))
	if err := s.MigrateSpeechModels(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateSpeechModels(ctx); err != nil {
		t.Fatal(err)
	}
	p := agentgateway.SpeechModel{ID: "fixture", Name: "主播模型", Protocol: "openai_chat", BaseURL: "https://api.example.com/v1", Model: "fixture-model", Enabled: true, TimeoutMS: 20000, MaxTokens: 16000}
	input := agentgateway.SpeechModelsInput{Profiles: []agentgateway.SpeechModelInput{{SpeechModel: p, NewAPIKey: "fixture-only-secret"}}, DefaultID: p.ID}
	if err := s.SaveSpeechModels(ctx, input, 1); err != nil {
		t.Fatal(err)
	}
	c, err := s.SpeechModels(ctx)
	if err != nil || len(c.Profiles) != 1 || !c.Profiles[0].HasAPIKey || c.Revision != 1 || c.Profiles[0].APIKey != "" {
		t.Fatalf("public config: %+v %v", c, err)
	}
	resolved, err := s.ResolveSpeechModel(ctx, "")
	if err != nil || resolved.APIKey != "fixture-only-secret" {
		t.Fatal("resolve failed", err)
	}
	var configRaw, keyRaw string
	if err := s.db.QueryRowContext(ctx, `SELECT config_json,keys_json FROM mgmt_anchor_model_settings WHERE id=1`).Scan(&configRaw, &keyRaw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(configRaw+keyRaw, "fixture-only-secret") {
		t.Fatal("plaintext credential persisted")
	}
	if err := s.SaveSpeechModels(ctx, input, 1); err == nil {
		t.Fatal("stale revision accepted")
	}
	input.Revision = 1
	input.Profiles[0].NewAPIKey = ""
	if err := s.SaveSpeechModels(ctx, input, 1); err != nil {
		t.Fatal("blank edit deleted key", err)
	}
	input.Revision = 2
	input.Profiles[0].BaseURL = "https://other.example.com/v1"
	if err := s.SaveSpeechModels(ctx, input, 1); err == nil {
		t.Fatal("retained key sent to changed endpoint")
	}
	if err := s.SaveSpeechModels(ctx, agentgateway.SpeechModelsInput{Revision: 2}, 1); err != nil {
		t.Fatal(err)
	}
	if p, err := s.ResolveSpeechModel(ctx, ""); err != nil || p != nil {
		t.Fatal("delete did not preserve environment fallback")
	}
	if _, err := s.ResolveSpeechModel(ctx, "fixture"); err == nil {
		t.Fatal("deleted pinned model silently fell back")
	}
}
