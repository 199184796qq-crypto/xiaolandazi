// Explicit text-only provider probe. No plan, queue, TTS or playback writes.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionlearning"
	"os"
	"strings"
	"time"
)

func main() {
	profileID := flag.String("profile", "", "explicit encrypted credential profile")
	modelID := flag.String("model", "", "explicit model to test with same authorized endpoint")
	input := flag.String("input", "", "compiled request packets")
	output := flag.String("output", "", "text-only result archive")
	configured := flag.Bool("configured-roles", false, "read actual mainline/interaction role routing")
	isolated := flag.Bool("isolated-cases", false, "do not carry context between fixtures")
flag.Parse()
	if *profileID == "" || *modelID == "" || *input == "" || *output == "" {
		panic("explicit probe arguments required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("probe database unavailable")
	}
	defer store.Close()
	profile, err := store.ResolveSpeechModel(ctx, *profileID)
	if err != nil || profile == nil || profile.APIKey == "" || !profile.Enabled {
		panic("probe credential unavailable")
	}
	profile.Model = *modelID
	var cases []struct {
		Name    string               `json:"name"`
		Request agentgateway.Request `json:"request"`
	}
	raw, err := os.ReadFile(*input)
	if err != nil || json.Unmarshal(raw, &cases) != nil {
		panic("probe packets invalid")
	}
	rows := []map[string]any{}
	recent := []string{}
	for _, c := range cases {
		if c.Request.Stage == "mainline_text" && !*isolated {
			last := len(c.Request.Messages) - 1
			var dynamic map[string]any
			if json.Unmarshal([]byte(c.Request.Messages[last].Content), &dynamic) != nil {
				panic("dynamic task invalid")
			}
			context := []rune(strings.Join(recent, "\n"))
			if len(context) > 2400 {
				context = context[len(context)-2400:]
			}
			dynamic["近期已采用内容仅供避重"] = string(context)
			dynamic["开头模式"] = "address_or_content"
			if len(recent)%3 != 0 {
				dynamic["开头模式"] = "content_first"
			}
			raw, _ := json.Marshal(dynamic)
			c.Request.Messages[last].Content = string(raw)
		}
		c.Request.Model = *modelID
		c.Request.EnableThinking = false
		c.Request.Timeout = 45 * time.Second
		raw, _ := json.Marshal(c.Request)
		started := time.Now()
		var result agentgateway.Response
		var callErr error
		if *configured {
			c.Request.Model = ""
			c.Request.Provider = ""
			result, callErr = agentgateway.NewFromEnv().WithSpeechModels(store).Complete(ctx, c.Request)
			if callErr == nil && (result.Model != *modelID || result.Provider != "anchor:"+*profileID) {
				callErr = fmt.Errorf("configured role/model mismatch")
			}
		} else {
			result, callErr = agentgateway.CompleteSpeechEndpoint(ctx, *profile, c.Request)
		}
		row := map[string]any{"name": c.Name, "request": c.Request, "request_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "response": result, "latency_ms": time.Since(started).Milliseconds(), "success": callErr == nil, "text_only": true}
		if callErr != nil {
			row["error"] = "model request failed"
		}
		if c.Request.ResponseFormat == agentgateway.ResponseJSON && callErr == nil {
			decoded, e := interactionlearning.DecodeAnswer(result.Text)
			row["structure_valid"] = e == nil
			row["decoded"] = decoded
		}
		if callErr == nil && c.Request.Stage == "mainline_text" {
			recent = append(recent, result.Text)
		}
		rows = append(rows, row)
		raw, _ = json.MarshalIndent(rows, "", "  ")
		if os.WriteFile(*output, raw, 0600) != nil {
			panic("probe archive failed")
		}
		fmt.Printf("case=%s success=%v input=%d output=%d latency_ms=%d\n", c.Name, callErr == nil, result.InputTokens, result.OutputTokens, result.LatencyMS)
		if callErr != nil {
			os.Exit(2)
		}
	}
}
