// Read-only deployment check by default. Explicit resource-default flags only
// merge the two resource IDs/region; never select models or decrypt API keys.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"livecompanion/management/internal/agentstudio"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"os"
	"time"
)

func main() {
	workspace := flag.String("set-default-workspace", "", "explicit default Workspace ID")
	agent := flag.String("set-default-agent", "", "explicit default Agent ID")
	region := flag.String("region", "cn-beijing", "Agent Studio region")
	flag.Parse()
	if *workspace != "" || *agent != "" {
		if _, err := agentstudio.New(*workspace, *region, "validation-only", time.Minute); err != nil || !agentstudio.ValidResourceID(*agent, "agent_") {
			fmt.Println(`{"ready":false,"error":"默认资源ID无效"}`)
			os.Exit(1)
		}
	}
	store, err := db.Open(config.Load())
	if err != nil {
		fmt.Println(`{"ready":false,"error":"数据库连接不可用"}`)
		os.Exit(1)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if *workspace != "" {
		if err = store.SetAnchorGenerationDefaultResources(ctx, *workspace, *agent, *region); err != nil {
			fmt.Println(`{"ready":false,"error":"默认资源保存失败"}`)
			os.Exit(1)
		}
	}
	cfg, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		fmt.Println(`{"ready":false,"error":"生成配置读取失败"}`)
		os.Exit(1)
	}
	problem := ""
	if err = cfg.Validate(); err != nil {
		problem = err.Error()
	}
	out, _ := json.Marshal(map[string]any{"ready": err == nil, "error": problem, "roles_configured": map[string]bool{"style": cfg.StyleProfileID != "", "scorecard": cfg.ScorecardProfileID != "", "wheel": cfg.SellingPointProfileID != "", "ali_credential": cfg.Cycle.CredentialID != "", "feedback": cfg.Cycle.FeedbackID != ""}, "workspace_configured": cfg.Cycle.WorkspaceID != "", "agent_configured": cfg.Cycle.AgentID != "", "mainline_buffer_seconds": cfg.MainlineBufferSeconds})
	fmt.Println(string(out))
	if err != nil {
		os.Exit(2)
	}
}
