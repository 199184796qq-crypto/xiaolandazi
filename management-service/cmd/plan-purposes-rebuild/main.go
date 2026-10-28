// Command plan-purposes-rebuild regenerates and prints one plan's purpose list.
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
)

func main() {
	tenant := flag.Int64("tenant", 0, "tenant id")
	plan := flag.Int64("plan", 0, "plan id")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		fmt.Println("db error:", err)
		return
	}
	defer store.Close()
	specs, err := store.BuildPlanPurposes(ctx, *tenant, *plan)
	if err != nil {
		fmt.Println("build error:", err)
		return
	}
	fmt.Printf("plan=%d purposes=%d\n", *plan, len(specs))
	for _, spec := range specs {
		fmt.Printf("%-22s %-20s in_out=%t sources=%v keywords=%v\n", spec.Key, spec.Name, spec.InOut, spec.Sources, spec.Keywords)
	}
}
