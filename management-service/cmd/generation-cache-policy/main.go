// Explicit operator helper: read/disable finished-content reuse without starting
// sessions, issuing model/TTS requests or deleting saved assets.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"os"
	"time"
)

func main() {
	disable := flag.Bool("disable", false, "disable mainline and interaction finished-content reuse")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("configuration store unavailable")
	}
	defer store.Close()
	read := func() map[string]bool {
		values := map[string]bool{}
		for _, key := range []string{model.MainlineGenerationCacheReuseSetting, model.InteractionGenerationCacheReuseSetting} {
			value, err := store.GenerationCacheReuseEnabled(ctx, key)
			if err != nil {
				panic("cache policy read failed")
			}
			values[key] = value
		}
		return values
	}
	before := read()
	if *disable {
		if err = store.MigrateSystemSettings(ctx); err != nil {
			panic("cache setting initialization failed")
		}
		if err = store.UpdateSystemSettings(ctx, []model.SystemSettingUpdate{{Key: model.MainlineGenerationCacheReuseSetting, Value: "false"}, {Key: model.InteractionGenerationCacheReuseSetting, Value: "false"}}, 0); err != nil {
			panic("cache policy update failed")
		}
	}
	hours, err := store.AnchorVoiceCacheRetentionHours(ctx)
	if err != nil {
		panic("cache retention read failed")
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"checked_at": time.Now().UTC(), "write_requested": *disable, "before": before, "after": read(), "retention_hours": hours, "assets_deleted": false, "live_sessions_started": false})
}
