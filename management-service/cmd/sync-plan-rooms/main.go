// Explicit operator repair requested by the plan owner. No audio/session writes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"os"
	"strings"
	"time"
)

func main() {
	tenant := flag.Int64("tenant-id", 0, "exact owner tenant")
	planID := flag.Int64("plan-id", 0, "exact owner plan")
	apply := flag.Bool("apply", false, "unify references to existing saved content")
	patch := flag.String("dispatch-patch", "", "optional JSON file with user-approved wording guidance")
	flag.Parse()
	if *tenant <= 0 || *planID <= 0 {
		panic("exact tenant and plan required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("database unavailable")
	}
	defer store.Close()
	plan, err := store.GetLiveAgentPlan(ctx, *tenant, *planID)
	if err != nil {
		panic("plan unavailable")
	}
	versions, err := store.ListLiveAgentPlanVersionsForPlan(ctx, *tenant, *planID)
	if err != nil {
		panic("version list unavailable")
	}
	out := map[string]any{"tenant": *tenant, "plan": *planID, "plan_name": plan.Name, "rooms": plan.RoomIDs, "apply": *apply, "version_rows_before": len(versions)}
	if *apply {
		if err = store.UnifyLiveAgentPlanSavedVersions(ctx, *tenant, *planID); err != nil {
			panic("reference unification failed")
		}
	}
	if *patch != "" {
		var rule struct {
			FactID          int64  `json:"fact_id"`
			ExpectedVersion int64  `json:"expected_version"`
			Forbidden       string `json:"forbidden"`
			Rewrite         string `json:"rewrite"`
		}
		raw, e := os.ReadFile(*patch)
		if e != nil || json.Unmarshal(raw, &rule) != nil || rule.FactID <= 0 || rule.ExpectedVersion <= 0 {
			panic("wording patch invalid")
		}
		facts, e := store.ListLiveAgentPlanFacts(ctx, *tenant, *planID)
		if e != nil {
			panic("facts unavailable")
		}
		found := false
		for _, f := range facts {
			if f.ID != rule.FactID {
				continue
			}
			found = true
			if f.VersionNo != rule.ExpectedVersion {
				panic("fact version changed; no overwrite")
			}
			out["dispatch_before"] = f
			if *apply {
				actor := int64(0)
				if f.UpdatedByUserID != nil {
					actor = *f.UpdatedByUserID
				} else if f.CreatedByUserID != nil {
					actor = *f.CreatedByUserID
				}
				if actor <= 0 {
					panic("plan owner unavailable")
				}
				forbidden := strings.TrimSpace(f.ForbiddenWording + "\n" + rule.Forbidden)
				rewrite := strings.TrimSpace(f.SafeRewrite + "\n" + rule.Rewrite)
				f, e = store.UpdateLiveAgentPlanFactWithExpectedVersion(ctx, *tenant, *planID, f.ID, actor, rule.ExpectedVersion, f.Category, f.Key, f.Value, forbidden, rewrite)
				if e != nil {
					panic("fact wording save failed")
				}
				out["dispatch_after"] = f
			}
		}
		if !found {
			panic("specified fact missing")
		}
	}
	rooms := []map[string]any{}
	for _, room := range plan.RoomIDs {
		v, e := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, *tenant, room)
		if e != nil {
			rooms = append(rooms, map[string]any{"room": room, "available": false})
			continue
		}
		rooms = append(rooms, map[string]any{"room": room, "version_id": v.ID, "version": v.VersionNo, "saved_from_room": v.SavedFromRoomID, "content_pools": v.GenerationContext["content_pools"]})
	}
	out["shared_references"] = rooms
	raw, _ := json.Marshal(out)
	fmt.Println(string(raw))
}
