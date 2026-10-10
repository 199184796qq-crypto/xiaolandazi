// Paid text-only boundary probe. Each model response contains speech only;
// all sample identities and expectations are held by the caller.
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
	"strings"
	"sync"
	"time"
)

type sample struct {
	Name      string `json:"name"`
	Candidate string `json:"candidate"`
	Expected  string `json:"expected"`
}
type input struct {
	System  string   `json:"system_prompt"`
	Facts   []string `json:"facts"`
	Style   string   `json:"style_meta"`
	Samples []sample `json:"samples"`
}

func main() {
	live := flag.Bool("live", false, "authorize paid model requests")
	in := flag.String("input", "", "input")
	out := flag.String("output", "", "archive")
	flag.Parse()
	if !*live || *in == "" || *out == "" {
		panic("explicit live arguments required")
	}
	raw, err := os.ReadFile(*in)
	var packet input
	if err != nil || json.Unmarshal(raw, &packet) != nil || len(packet.Samples) == 0 || len(packet.Samples) > 24 {
		panic("invalid cases")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("configuration unavailable")
	}
	defer store.Close()
	id, err := store.ResolveTextRole(ctx, "mainline_text")
	if err != nil {
		panic("writer role unavailable")
	}
	profile, err := store.ResolveSpeechModel(ctx, id)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		panic("model unavailable")
	}
	rows := make([]map[string]any, len(packet.Samples))
	var mu sync.Mutex
	save := func() {
		mu.Lock()
		defer mu.Unlock()
		b, _ := json.MarshalIndent(map[string]any{"model": profile.Model, "text_only": true, "production_adopted": false, "rows": rows}, "", "  ")
		if os.WriteFile(*out+".tmp", b, 0600) != nil || os.Rename(*out+".tmp", *out) != nil {
			panic("archive failed")
		}
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < 3; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				s := packet.Samples[i]
				content := map[string]any{"待处理候选口播（不是事实依据）": s.Candidate, "当前有效事实资料（唯一客观依据）": packet.Facts}
				if packet.Style != "" {
					content["主播风格元提示词"] = packet.Style
				}
				body, _ := json.Marshal(content)
				req := agentgateway.Request{Stage: "content_boundary_probe", Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: packet.System}, {Role: "user", Content: string(body)}}, ResponseFormat: agentgateway.ResponseText, EnableThinking: false, MaxTokens: 300, Timeout: 40 * time.Second}
				if profile.SystemPrompt != "" {
					req.Messages = []agentgateway.Message{{Role: "system", Content: packet.System}, {Role: "system", Content: profile.SystemPrompt}, {Role: "user", Content: string(body)}}
				}
				req, err := agentgateway.PrepareTextRequest(*profile, req)
				row := map[string]any{"program_sample_id": fmt.Sprintf("boundary-%02d", i+1), "name": s.Name, "candidate": s.Candidate, "expected": s.Expected, "requested_at": time.Now().UTC(), "request": req, "success": false}
				if err == nil {
					resp, e := agentgateway.CompleteSpeechEndpoint(ctx, *profile, req)
					row["response"] = resp
					row["success"] = e == nil && strings.TrimSpace(resp.Text) != ""
					if e != nil {
						row["error"] = "model request failed"
					}
				} else {
					row["error"] = "request preparation failed"
				}
				mu.Lock()
				rows[i] = row
				mu.Unlock()
				save()
				fmt.Printf("sample=%02d success=%v\n", i+1, row["success"])
			}
		}()
	}
	for i := range packet.Samples {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	save()
}
