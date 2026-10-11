// Offline semantic audit of frozen first drafts. Reads configuration only and
// retains local evidence; never writes plans, roles, exchanges, TTS or queues.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type sample struct {
	Name           string                `json:"name"`
	Layer          int                   `json:"layer"`
	Repeat         int                   `json:"repeat"`
	Body           string                `json:"body"`
	SourceRequest  agentgateway.Request  `json:"source_request"`
	SourceResponse agentgateway.Response `json:"source_response"`
	SourceVerdict  string                `json:"source_verdict"`
}
type input struct {
	P0   string   `json:"p0"`
	Rows []sample `json:"rows"`
}

func hash(v any) string { raw, _ := json.Marshal(v); return fmt.Sprintf("%x", sha256.Sum256(raw)) }
func verdict(s string) string {
	s = strings.TrimLeft(strings.TrimSpace(s), "#*【[ ")
	if strings.HasPrefix(s, "通过") {
		return "pass"
	}
	if strings.HasPrefix(s, "有问题") {
		return "problem"
	}
	if strings.HasPrefix(s, "不确定") {
		return "uncertain"
	}
	return "undetermined"
}
func main() {
	if os.Getenv("FIRST_DRAFT_AUDIT_REPLAY") != "1" {
		panic("explicit offline replay required")
	}
	raw, err := os.ReadFile(os.Getenv("FIRST_DRAFT_AUDIT_INPUT"))
	if err != nil {
		panic("input unavailable")
	}
	var in input
	if json.Unmarshal(raw, &in) != nil || len(in.Rows) != 147 || in.P0 == "" {
		panic("invalid frozen inputs")
	}
	effective, err := policy.LoadGeneration()
	if err != nil || in.P0 != effective.PromptText {
		panic("current full P0 differs from frozen audit")
	}
	for _, s := range in.Rows {
		r := s.SourceRequest
		if r.Model != "qwen3.7-flash" || r.Temperature == nil || *r.Temperature != 0.1 || r.MaxTokens != 550 || r.EnableThinking || r.ResponseFormat != agentgateway.ResponseText || r.Stage != "offline_first_draft_gradual_semantic_review" || len(r.Messages) != 2 || !strings.Contains(r.Messages[0].Content, in.P0) || !strings.Contains(r.Messages[1].Content, s.Body) {
			panic("unexpected source audit inputs")
		}
	}
	path := os.Getenv("FIRST_DRAFT_AUDIT_OUTPUT")
	if path == "" {
		panic("output required")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		panic("archive directory unavailable")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		panic("output already exists or cannot be checked; preserve prior evidence")
	}
	target := os.Getenv("FIRST_DRAFT_AUDIT_MODEL")
	if target == "" {
		target = "qwen3.8-flash"
	}
	switch target {
	case "qwen3.8-flash", "qwen3.8-max", "deepseek-v4-flash", "deepseek-v4-flash-0731":
	default:
		panic("unsupported offline comparison model")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		panic("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		panic("inventory unavailable")
	}
	var selected *agentgateway.SpeechModel
	for _, p := range models.Profiles {
		if p.Enabled && p.Model == target {
			selected, err = store.ResolveSpeechModel(ctx, p.ID)
			break
		}
	}
	if err != nil || selected == nil || !selected.Enabled || selected.APIKey == "" {
		panic("requested enabled model profile unavailable")
	}
	p := *selected
	fmt.Printf("offline audit selected model=%s profile=%s protocol=%s\n", p.Model, p.ID, p.Protocol)
	metadata := map[string]any{"started_at": time.Now().UTC().Format(time.RFC3339Nano), "model": p.Model, "profile_id": p.ID, "profile_protocol": p.Protocol, "input_sha": hash(in), "p0_sha": hash(in.P0), "temperature": 0.1, "max_tokens": 550, "enable_thinking": false, "baseline_model": "qwen3.7-flash", "baseline_is_archived": true, "configured_review_id": models.ReviewDefaultID, "database_writes": false, "role_routing_changed": false, "generation": false, "repair": false, "tts": false, "playback": false}
	rows := []map[string]any{}
	var mu sync.Mutex
	save := func() {
		data, e := json.MarshalIndent(map[string]any{"metadata": metadata, "rows": rows}, "", "  ")
		if e != nil {
			panic("serialization failed")
		}
		tmp := path + ".tmp"
		if os.WriteFile(tmp, data, 0600) != nil || os.Rename(tmp, path) != nil {
			panic("archive failed")
		}
	}
	save()
	jobs := make(chan sample)
	var wg sync.WaitGroup
	for worker := 0; worker < 3; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				req := s.SourceRequest
				req.Model = p.Model
				prepared, e := agentgateway.PrepareTextRequest(p, req)
				if e != nil || hash(prepared.Messages) != hash(s.SourceRequest.Messages) {
					panic("unexpected extra prompt stack")
				}
				start := time.Now()
				response, e := agentgateway.CompleteSpeechEndpoint(ctx, p, req)
				result := map[string]any{"name": s.Name, "layer": s.Layer, "repeat": s.Repeat, "body_sha": hash(s.Body), "messages_sha": hash(req.Messages), "source_messages_sha": hash(s.SourceRequest.Messages), "request": req, "response": response, "completed": e == nil, "wall_ms": time.Since(start).Milliseconds(), "source_response": s.SourceResponse, "source_verdict": s.SourceVerdict}
				if e == nil {
					result["model_verdict"] = verdict(response.Text)
				} else {
					result["model_verdict"] = "incomplete"
					result["error"] = e.Error()
				}
				mu.Lock()
				rows = append(rows, result)
				save()
				fmt.Printf("completed=%d/147 case=%s layer=%d repeat=%d verdict=%v\n", len(rows), s.Name, s.Layer, s.Repeat, result["model_verdict"])
				mu.Unlock()
			}
		}()
	}
	for _, s := range in.Rows {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	metadata["finished_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	metadata["row_count"] = len(rows)
	latest, e := store.SpeechModels(ctx)
	if e == nil {
		metadata["configured_review_id_after"] = latest.ReviewDefaultID
		metadata["review_role_unchanged_during_replay"] = models.ReviewDefaultID == latest.ReviewDefaultID
	}
	save()
	fmt.Println("done; raw model conclusions require independent interpretation")
}
