// Explicit maintenance command; never starts, publishes or restores a run.
package main

import (
	"context"
	"flag"
	"fmt"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"os"
	"time"
)

func main() {
	tenant := flag.Int64("tenant", 0, "explicit tenant")
	room := flag.Int64("room", 0, "explicit room")
	apply := flag.Bool("apply", false, "apply authorized snapshot retention")
	flag.Parse()
	if *tenant <= 0 || *room <= 0 || !*apply {
		fmt.Fprintln(os.Stderr, "explicit tenant, room and apply are required")
		os.Exit(1)
	}
	store, err := db.Open(config.Load())
	if err != nil {
		fmt.Fprintln(os.Stderr, "database unavailable")
		os.Exit(1)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = store.PruneSavedPlanVersionsForRoom(ctx, *tenant, *room); err != nil {
		fmt.Fprintln(os.Stderr, "retention failed:", err)
		os.Exit(1)
	}
	v, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, *tenant, *room)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read current saved plan failed")
		os.Exit(1)
	}
	fmt.Printf("tenant=%d room=%d current_saved_id=%d current_saved_no=%d; protected live snapshots and shared media unchanged\n", *tenant, *room, v.ID, v.VersionNo)
}
