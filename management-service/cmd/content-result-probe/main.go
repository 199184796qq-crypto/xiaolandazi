// Explicit paid offline probe: content-only model results, program-owned
// correlations. No plan, queue, live session, review or TTS writes.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
)

type binding struct {
	CaseID         string   `json:"case_id"`
	SourceExchange int64    `json:"source_exchange"`
	TaskKey        string   `json:"task_key"`
	Products       []string `json:"product_ids"`
	Facts          []string `json:"input_fact_source_ids"`
}

type sample struct {
	Label   string         `json:"label"`
	Binding binding        `json:"program_binding"`
	Input   map[string]any `json:"content_input"`
}

type input struct {
	PlanPrompt   string   `json:"plan_prompt"`
	SpeechPrompt string   `json:"speech_prompt"`
	Cases        []sample `json:"cases"`
}

func main() {
	inPath := flag.String("input", "", "explicit input archive")
	outPath := flag.String("output", "", "offline result archive")
	live := flag.Bool("live", false, "authorize paid text-only model requests")
	flag.Parse()
	if !*live || *inPath == "" || *outPath == "" {
		panic("explicit live probe arguments required")
	}
	raw, err := os.ReadFile(*inPath)
	var data input
	if err != nil || json.Unmarshal(raw, &data) != nil || len(data.Cases) == 0 || len(data.Cases) > 6 {
		panic("invalid probe cases")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("model configuration unavailable")
	}
	defer store.Close()
	profiles := map[string]*agentgateway.SpeechModel{}
	for _, stage := range []string{"mainline_angle_plan", "mainline_text"} {
		id, err := store.ResolveTextRole(ctx, stage)
		if err != nil {
			panic("model role unavailable")
		}
		profile, err := store.ResolveSpeechModel(ctx, id)
		if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
			panic("enabled model connection unavailable")
		}
		profiles[stage] = profile
	}
	rows := make([]map[string]any, len(data.Cases))
	for i, c := range data.Cases {
		rows[i] = map[string]any{"label": c.Label, "program_binding": freezeBinding(c.Binding), "visits": []map[string]any{}}
	}
	cache := newPoolCache()
	var mu sync.Mutex
	save := func() {
		mu.Lock()
		defer mu.Unlock()
		result := map[string]any{"text_only": true, "live_sessions_started": false, "tts_generated": false, "plans_saved": false, "model_roles": map[string]string{"planner": profiles["mainline_angle_plan"].Model, "writer": profiles["mainline_text"].Model}, "rows": rows, "saved_direction_cache": cache.snapshot(), "refresh_after_plays": 3, "usage_confirmation_source": "simulated offline playback acknowledgements; not real live playback"}
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil || os.WriteFile(*outPath+".tmp", b, 0600) != nil || os.Rename(*outPath+".tmp", *outPath) != nil {
			panic("result archive failed")
		}
	}
	call := func(stage, prompt string, content map[string]any, owner binding, visit int) map[string]any {
		owner = freezeBinding(owner)
		profile := profiles[stage]
		// Assign correlation before any preparation or transport can fail.
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			panic("request identity unavailable")
		}
		at := time.Now().UTC()
		row := map[string]any{"program_request_id": hex.EncodeToString(id), "program_binding": owner, "visit": visit, "stage": stage, "requested_at": at, "success": false}
		body, _ := json.Marshal(content)
		messages := []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(body)}}
		if profile.SystemPrompt != "" {
			messages = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, messages...)
		}
		request := agentgateway.Request{Stage: stage, Model: profile.Model, Messages: messages, ResponseFormat: agentgateway.ResponseText, EnableThinking: false, MaxTokens: 750, Timeout: 40 * time.Second}
		if stage == "mainline_text" {
			var err error
			request, err = agentgateway.PrepareTextRequest(*profile, request)
			if err != nil {
				row["request"] = request
				row["error"] = "text contract unavailable"
				row["error_stage"] = "request_preparation"
				fmt.Printf("stage=%s case=%s visit=%d success=false error_stage=request_preparation\n", stage, owner.CaseID, visit)
				return row
			}
		}
		// Identity is assigned before sending. The model receives only Messages;
		// no response field is parsed or merged into this owner context.
		response, err := agentgateway.CompleteSpeechEndpoint(ctx, *profile, request)
		success := err == nil && strings.TrimSpace(response.Text) != ""
		row["request"], row["response"], row["success"] = request, response, success
		if err != nil {
			row["error"] = "model request failed"
			row["error_stage"] = "model_transport"
		}
		fmt.Printf("stage=%s case=%s visit=%d success=%v input=%d output=%d\n", stage, owner.CaseID, visit, success, response.InputTokens, response.OutputTokens)
		return row
	}
	save()
	var wg sync.WaitGroup
	for i, c := range data.Cases {
		wg.Add(1)
		go func(i int, c sample) {
			defer wg.Done()
			visits := []map[string]any{}
			previousPlan, previousText := "", ""
			scene := "offline-scene-1"
			fingerprintData, _ := json.Marshal(c.Input)
			fingerprint := fmt.Sprintf("%x", sha256.Sum256(append(fingerprintData, []byte(profiles["mainline_angle_plan"].ID+":"+profiles["mainline_angle_plan"].Model+":"+data.PlanPrompt)...)))
			// The fourth visit verifies a fresh plan after three simulated plays.
			for visit := 1; visit <= 4; visit++ {
				packet := map[string]any{}
				for k, v := range c.Input {
					packet[k] = v
				}
				pool, hit := cache.lookup(scene, c.Binding.TaskKey, fingerprint, 3)
				row := map[string]any{"visit": visit, "direction_cache_hit": hit, "program_binding": freezeBinding(c.Binding)}
				if !hit {
					packet["前次讲述方向"] = previousPlan
					planning := call("mainline_angle_plan", data.PlanPrompt, packet, c.Binding, visit)
					row["planning"] = planning
					if planning["success"].(bool) {
						previousPlan = planning["response"].(agentgateway.Response).Text
						items := directionItems(previousPlan, planning["program_request_id"].(string))
						if len(items) > 0 {
							pool = directionPool{Owner: c.Binding, Fingerprint: fingerprint, RequestID: planning["program_request_id"].(string), Items: items}
							cache.put(scene, pool)
						}
					}
				}
				// Save planning before speech starts, using an immutable copy.
				partial := map[string]any{}
				for k, v := range row {
					partial[k] = v
				}
				mu.Lock()
				rows[i] = map[string]any{"label": c.Label, "program_binding": freezeBinding(c.Binding), "visits": append(append([]map[string]any(nil), visits...), partial)}
				mu.Unlock()
				save()
				if len(pool.Items) > 0 {
					if bank, ok := cache.lookup(scene, c.Binding.TaskKey, fingerprint, 3); ok {
						pool = bank
					}
					selected := cache.choose(scene, c.Binding.TaskKey, pool)
					row["directions"] = pool.Items
					row["selected_direction_id"] = selected.ID
					packet = speechPacket(c.Input, selected.Text, previousText)
					writing := call("mainline_text", data.SpeechPrompt, packet, c.Binding, visit)
					row["writing"] = writing
					if writing["success"].(bool) {
						previousText = writing["response"].(agentgateway.Response).Text
						cache.confirmPlayed(scene, c.Binding.TaskKey, "offline-confirmation:"+writing["program_request_id"].(string), selected.ID)
					}
				}
				visits = append(visits, row)
				mu.Lock()
				rows[i] = map[string]any{"label": c.Label, "program_binding": c.Binding, "visits": visits}
				mu.Unlock()
				save()
			}
		}(i, c)
	}
	wg.Wait()
	save()
}
