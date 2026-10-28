// Command addressing-model-check prints which provider/model a stage resolves to
// under the production resolver, with and without the room pool scope.
package main

import (
	"context"
	"fmt"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		fmt.Println("db error:", err)
		return
	}
	defer store.Close()
	gateway := agentgateway.NewFromEnv().WithSpeechModels(store)
	stages := []string{"interaction_addressing_plan", "interaction_welcome_greeting", "interaction_single_direct_answer", "question_cluster", "mainline_text_rewrite"}
	scope := agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: 14, RoomID: 29, RuntimeID: 518, RequireLease: true})
	for _, stage := range stages {
		provider, model, err := gateway.ResolveStageModel(ctx, stage)
		provider2, model2, err2 := gateway.ResolveStageModel(scope, stage)
		fmt.Printf("%-34s plain=%s/%s (%v)  room=%s/%s (%v)\n", stage, provider, model, err, provider2, model2, err2)
	}
}
