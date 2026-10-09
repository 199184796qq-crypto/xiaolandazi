// Explicit operator bootstrap for the user-authorized Plus text rollout.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"os"
	"time"
)

func main() {
	source := flag.String("copy-credential-profile", "", "authorized existing credential to copy")
	model := flag.String("model", "", "model ID")
	id := flag.String("new-profile-id", "", "profile ID")
	flag.Parse()
	if *source == "" || *model == "" || *id == "" {
		panic("explicit text rollout arguments required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("configuration database unavailable")
	}
	defer store.Close()
	profiles, err := store.SpeechModels(ctx)
	if err != nil {
		panic("model configuration unavailable")
	}
	var base *agentgateway.SpeechModel
	selected := ""
	for _, p := range profiles.Profiles {
		if p.ID == *source {
			copy := p
			base = &copy
		}
		if p.Model == *model && p.Enabled && p.HasAPIKey {
			selected = p.ID
		}
	}
	if selected == "" {
		if base == nil || !base.Enabled || !base.HasAPIKey {
			panic("authorized credential unavailable")
		}
		next := agentgateway.SpeechModelsInput{DefaultID: profiles.DefaultID, Revision: profiles.Revision}
		for _, p := range profiles.Profiles {
			next.Profiles = append(next.Profiles, agentgateway.SpeechModelInput{SpeechModel: p})
		}
		added := *base
		added.ID = *id
		added.Name = "Qwen3.7Plus · 文字生成"
		added.Model = *model
		added.SystemPrompt = ""
		next.Profiles = append(next.Profiles, agentgateway.SpeechModelInput{SpeechModel: added, CopyFromID: *source})
		if err = store.SaveSpeechModels(ctx, next, 0); err != nil {
			panic("save new encrypted text profile failed")
		}
		selected = *id
	}
	if err = store.SetTextGenerationRoles(ctx, selected, selected); err != nil {
		panic("save text role references failed")
	}
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil || settings.Validate() != nil {
		panic("text roles failed readiness validation")
	}
	_, sha, _, err := agentgateway.TextPromptContract(*model)
	if err != nil {
		panic("text profile mother unavailable")
	}
	raw, _ := json.Marshal(map[string]any{"model": *model, "profile_id": selected, "mainline_text_profile": settings.Cycle.TextProfileID, "interaction_text_profile": settings.InteractionProfileID, "text_prompt_sha256": sha, "voice_configuration_changed": false, "shared_model_default_changed": false})
	fmt.Println(string(raw))
	_ = os.Stdout.Sync()
}
