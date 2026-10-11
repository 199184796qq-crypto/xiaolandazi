package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentstudio"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Opt-in experimental sessions only. Database reads, model calls and a private
// result file; no model-exchange writes, Redis, plans, queues, TTS or live sessions.
func TestMainlinePurposeDiversityProbe(t *testing.T) {
	if os.Getenv("MAINLINE_DIVERSITY_PROBE") != "1" {
		t.Skip("explicit provider experiment required")
	}
	raw, err := os.ReadFile(os.Getenv("MAINLINE_DIVERSITY_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Facts        string           `json:"facts"`
		SellingPoint string           `json:"selling_point"`
		BroadAngle   string           `json:"broad_angle"`
		SeedHistory  []string         `json:"seed_history"`
		Briefs       []map[string]any `json:"briefs"`
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	mother, err := os.ReadFile(os.Getenv("MAINLINE_DIVERSITY_PROMPT"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cfg, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.ResolveSpeechModel(ctx, cfg.Cycle.CredentialID)
	if err != nil || p == nil || !p.Enabled {
		t.Fatal("mainline credentials unavailable")
	}
	client, err := agentstudio.New(cfg.Cycle.WorkspaceID, cfg.Cycle.Region, p.APIKey, 120*time.Second)
	if err != nil {
		t.Fatal("model client unavailable")
	}
	env := cfg.Cycle.EnvironmentID
	if env == "" {
		envs, e := client.ListEnvironments(ctx)
		if e != nil || len(envs) == 0 {
			t.Fatal("model environment unavailable")
		}
		env = envs[0].ID
	}
	rows := []map[string]any{}
	report := map[string]any{"experiment": "mainline-goal-angle-memory-v1", "production_changed": false, "playback_started": false, "groups": 3, "samples_per_group": len(f.Briefs), "outputs": rows}
	save := func() {
		report["outputs"] = rows
		out, _ := json.MarshalIndent(report, "", "  ")
		if e := os.WriteFile(os.Getenv("MAINLINE_DIVERSITY_RESULT"), out, 0600); e != nil {
			t.Error("result write failed")
		}
	}
	defer save()
	for _, arm := range []string{"A", "B", "C"} {
		session, e := client.CreateSession(ctx, cfg.Cycle.AgentID, env, "离线内容变化实验 "+arm, map[string]string{"experiment": "mainline-goal-angle-memory-v1", "group": arm})
		if e != nil {
			t.Fatal("isolated experimental session unavailable")
		}
		report["model"] = session.Agent.Model.ID
		recent := append([]string{}, f.SeedHistory...)
		func() {
			defer func() {
				clean, done := context.WithTimeout(context.Background(), 15*time.Second)
				defer done()
				if e := client.ArchiveSession(clean, session.ID); e != nil {
					t.Log("experimental session archive unavailable")
				}
			}()
			for i, b := range f.Briefs {
				input := map[string]any{"experiment_group": arm, "segment_number": i + 1, "allowed_link": "1", "complete_authorized_facts": f.Facts, "selling_point": f.SellingPoint, "simulated_recent_texts": recent}
				if arm == "C" {
					input["segment_brief"] = b
				} else {
					input["broad_angle"] = f.BroadAngle
					if arm == "B" {
						input["purpose"] = b["purpose"]
					}
				}
				data, _ := json.Marshal(input)
				prompt := string(mother) + "\n\n" + string(data)
				started := time.Now()
				result, e := client.SendMessageAndWait(ctx, session.ID, prompt)
				row := map[string]any{"group": arm, "segment": i + 1, "input": input, "text": result.Text, "elapsed_ms": time.Since(started).Milliseconds()}
				rows = append(rows, row)
				save()
				if e != nil {
					row["error"] = "provider generation failed"
					t.Fatal("experimental generation failed")
				}
				if strings.TrimSpace(result.Text) == "" {
					t.Fatal("empty experimental output")
				}
				recent = append(recent, result.Text)
				t.Logf("group=%s segment=%d elapsed_ms=%d chars=%d", arm, i+1, time.Since(started).Milliseconds(), len([]rune(result.Text)))
			}
		}()
	}
	report["transport_complete"] = true
}
