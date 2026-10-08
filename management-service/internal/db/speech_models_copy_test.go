package db

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
)

func speechCopyFixture(t *testing.T) (agentgateway.SpeechModels, map[string][]byte, agentgateway.SpeechModelsInput, string) {
	t.Helper()
	secret := strings.Repeat("copy-fixture-", 4)
	p := agentgateway.SpeechModel{ID: "original", Name: "Qwen", Model: "fixture-model", Protocol: "openai_chat", BaseURL: "https://example.com/v1", Enabled: true, TimeoutMS: 9000, MaxTokens: 1234, ChatTokenField: "max_completion_tokens", SystemPrompt: "custom prompt", HasAPIKey: true}
	key, err := agentgateway.SealSpeechKey(secret, p.ID, "fixture-only-key")
	if err != nil {
		t.Fatal(err)
	}
	old := agentgateway.SpeechModels{Profiles: []agentgateway.SpeechModel{p}, DefaultID: p.ID, Revision: 8}
	copy := p
	copy.ID, copy.Name = "copy", "Qwen02"
	input := agentgateway.SpeechModelsInput{Profiles: []agentgateway.SpeechModelInput{{SpeechModel: p}, {SpeechModel: copy, CopyFromID: p.ID}}, DefaultID: p.ID, Revision: old.Revision}
	return old, map[string][]byte{p.ID: key}, input, secret
}

func TestSpeechModelCopyReencryptsIndependentKeyAndPreservesDefault(t *testing.T) {
	old, keys, input, secret := speechCopyFixture(t)
	before, _ := json.Marshal(old)
	keyBefore := append([]byte(nil), keys["original"]...)
	next, nextKeys, err := buildSpeechModelsUpdate(old, keys, input, secret)
	if err != nil {
		t.Fatal(err)
	}
	if next.DefaultID != old.DefaultID || next.Revision != 9 || len(next.Profiles) != 2 {
		t.Fatal("default or revision changed incorrectly")
	}
	if !reflect.DeepEqual(next.Profiles[1], input.Profiles[1].SpeechModel) {
		t.Fatal("copy parameters lost")
	}
	for _, id := range []string{"original", "copy"} {
		plain, err := agentgateway.OpenSpeechKey(secret, id, nextKeys[id])
		if err != nil || plain != "fixture-only-key" {
			t.Fatal("credential reuse failed")
		}
	}
	if bytes.Equal(nextKeys["copy"], nextKeys["original"]) {
		t.Fatal("ciphertext was linked instead of resealed")
	}
	if _, err := agentgateway.OpenSpeechKey(secret, "original", nextKeys["copy"]); err == nil {
		t.Fatal("copy ciphertext accepts original ID")
	}
	public, _ := next.PublicJSON()
	if strings.Contains(string(public), "fixture-only-key") || strings.Contains(string(public), "copy_from_id") {
		t.Fatal("private copy input leaked")
	}
	after, _ := json.Marshal(old)
	if !bytes.Equal(before, after) || !bytes.Equal(keyBefore, keys["original"]) {
		t.Fatal("source mutated")
	}
	// Subsequent edits and deleting the source do not need its credential again.
	copy := next.Profiles[1]
	copy.Name, copy.Model = "独立副本", "changed-model"
	updated, updatedKeys, err := buildSpeechModelsUpdate(next, nextKeys, agentgateway.SpeechModelsInput{Profiles: []agentgateway.SpeechModelInput{{SpeechModel: copy}}, Revision: next.Revision}, secret)
	if err != nil || len(updated.Profiles) != 1 {
		t.Fatal("independent edit failed", err)
	}
	plain, err := agentgateway.OpenSpeechKey(secret, "copy", updatedKeys["copy"])
	if err != nil || plain != "fixture-only-key" {
		t.Fatal("source removal lost copy key")
	}
}

func TestSpeechModelCopyRejectsUnsafeOrStaleInputs(t *testing.T) {
	cases := []struct {
		name   string
		change func(*agentgateway.SpeechModelsInput)
	}{
		{"changed endpoint", func(i *agentgateway.SpeechModelsInput) { i.Profiles[1].BaseURL = "https://other.example.com/v1" }},
		{"changed protocol", func(i *agentgateway.SpeechModelsInput) { i.Profiles[1].Protocol = "anthropic" }},
		{"missing source", func(i *agentgateway.SpeechModelsInput) { i.Profiles[1].CopyFromID = "missing" }},
		{"self copy", func(i *agentgateway.SpeechModelsInput) { i.Profiles[1].CopyFromID = "copy" }},
		{"invalid source", func(i *agentgateway.SpeechModelsInput) { i.Profiles[1].CopyFromID = "../original" }},
		{"overwrite existing ID", func(i *agentgateway.SpeechModelsInput) {
			i.Profiles = []agentgateway.SpeechModelInput{{SpeechModel: i.Profiles[0].SpeechModel, CopyFromID: "other"}}
		}},
		{"stale revision", func(i *agentgateway.SpeechModelsInput) { i.Revision-- }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			old, keys, input, secret := speechCopyFixture(t)
			tc.change(&input)
			if _, _, err := buildSpeechModelsUpdate(old, keys, input, secret); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
	old, keys, input, secret := speechCopyFixture(t)
	keys["original"] = []byte("corrupt")
	if _, _, err := buildSpeechModelsUpdate(old, keys, input, secret); err == nil {
		t.Fatal("broken credential accepted")
	}
}

func TestSpeechModelCopyAllowsNewExplicitCredentialAfterEndpointChange(t *testing.T) {
	old, keys, input, secret := speechCopyFixture(t)
	input.Profiles[1].BaseURL = "https://other.example.com/v1"
	input.Profiles[1].NewAPIKey = "new-fixture-only-key"
	_, nextKeys, err := buildSpeechModelsUpdate(old, keys, input, secret)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := agentgateway.OpenSpeechKey(secret, "copy", nextKeys["copy"])
	if err != nil || plain != "new-fixture-only-key" {
		t.Fatal("explicit credential not used")
	}
}
