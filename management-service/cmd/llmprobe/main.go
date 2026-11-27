// 通用探针：从 /tmp/ping.json 读取 messages，调用模型池，打印返回。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

type payload struct {
	Stage     string                   `json:"stage"`
	Messages  []agentgateway.Message   `json:"messages"`
	MaxTokens int                      `json:"max_tokens"`
	Effort    string                   `json:"effort"`
}

func main() {
	cfg := config.Load()
	store, err := appdb.Open(cfg)
	if err != nil {
		fmt.Println("open db:", err)
		os.Exit(1)
	}
	defer store.Close()
	gw := agentgateway.NewFromEnv().WithSpeechModels(store)

	raw, err := os.ReadFile("/tmp/ping.json")
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	var in payload
	if err := json.Unmarshal(raw, &in); err != nil {
		fmt.Println("parse:", err)
		os.Exit(1)
	}
	if in.Stage == "" {
		in.Stage = "probe"
	}
	if in.MaxTokens == 0 {
		in.MaxTokens = 1500
	}
	resp, err := gw.Complete(context.Background(), agentgateway.Request{
		Stage:          in.Stage,
		Messages:       in.Messages,
		MaxTokens:      in.MaxTokens,
		ResponseFormat: agentgateway.ResponseText,
		EnableThinking: false,
		Timeout:        60 * time.Second,
		ReasoningEffort: in.Effort,
	})
	if err != nil {
		fmt.Println("complete:", err)
		os.Exit(1)
	}
	fmt.Println(resp.Text)
}
