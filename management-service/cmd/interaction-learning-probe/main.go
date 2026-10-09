// Read-only prompt probe: uses the configured speech API and never records or
// adopts a correction, starts a live session, or creates audio.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionlearning"
)

func main() {
	var input struct {
		Cases []struct {
			ID         string `json:"id"`
			System     string `json:"system"`
			Message    string `json:"message"`
			Simulation bool   `json:"simulation"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Fprintln(os.Stderr, "invalid input")
		os.Exit(1)
	}
	cfg := config.Load()
	s, err := db.Open(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database unavailable")
		os.Exit(1)
	}
	defer s.Close()
	g := agentgateway.NewFromEnv().WithSpeechModels(s)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	for _, c := range input.Cases {
		ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
		tokens := 1000
		if c.Simulation {
			tokens = 3500
		}
		started := time.Now()
		system := c.System
		if !c.Simulation {
			system = s.AgentPromptValue(ctx, "live.answer.system", "") + "\n\n" + system
		}
		stage := "speech_generation"
		if c.Simulation {
			stage = "interaction_learning"
		}
		response, err := g.Complete(ctx, agentgateway.Request{Stage: stage, Messages: []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: c.Message}}, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: tokens, EnableThinking: false, Timeout: 45 * time.Second})
		cancel()
		failure := ""
		if err != nil {
			failure = err.Error()
		}
		protocol := ""
		final := ""
		if err == nil && !c.Simulation {
			a, e := interactionlearning.DecodeAnswer(response.Text)
			if e != nil {
				protocol = e.Error()
			} else {
				final = a.Reply
				protocol = a.MetadataError
			}
		}
		_ = encoder.Encode(map[string]any{"id": c.ID, "provider": response.Provider, "model": response.Model, "latency_ms": time.Since(started).Milliseconds(), "input": c.Message, "system": system, "raw_output": response.Text, "error": failure, "protocol_error": protocol, "final_speech": final})
	}
}
