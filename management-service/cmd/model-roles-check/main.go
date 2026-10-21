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
	live := flag.Bool("live", false, "make one paid text-only connection request for each role")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration database unavailable")
		os.Exit(1)
	}
	defer store.Close()
	models, err := store.SpeechModels(ctx)
	if err != nil || models.SystemDefaultID == "" || models.DefaultID == "" || models.ReviewDefaultID == "" {
		fmt.Fprintln(os.Stderr, "model roles are not configured")
		os.Exit(1)
	}
	checks := []map[string]any{}
	for _, stage := range []string{"system_agent", "style_analysis", "selling_point_extraction", "mainline_angle_plan", "speech_generation", "interaction_bridge", "mainline_p0_context", "speech_review"} {
		id, err := store.ResolveTextRole(ctx, stage)
		if err != nil {
			fmt.Fprintln(os.Stderr, "role resolution failed")
			os.Exit(1)
		}
		checks = append(checks, map[string]any{"stage": stage, "profile_id": id})
	}
	results := []map[string]any{}
	if *live {
		gateway := agentgateway.NewFromEnv().WithSpeechModels(store)
		for _, stage := range []string{"model_role_connection", "speech_generation"} {
			response, err := gateway.Complete(ctx, agentgateway.Request{Stage: stage, Messages: []agentgateway.Message{{Role: "user", Content: "这是模型连接测试，本次不生成商品文案。请只输出OK。"}}, MaxTokens: 64, EnableThinking: false, Timeout: 60 * time.Second})
			if err != nil || response.Text == "" {
				fmt.Fprintln(os.Stderr, "selected model connection failed for "+stage)
				os.Exit(1)
			}
			results = append(results, map[string]any{"stage": stage, "model": response.Model, "provider": response.Provider, "input_tokens": response.InputTokens, "output_tokens": response.OutputTokens, "latency_ms": response.LatencyMS, "nonempty_response": true})
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"system_default_id": models.SystemDefaultID, "speech_default_id": models.DefaultID, "review_default_id": models.ReviewDefaultID, "roles": checks, "live_requests": results, "tts_generated": false, "live_sessions_started": false})
}
