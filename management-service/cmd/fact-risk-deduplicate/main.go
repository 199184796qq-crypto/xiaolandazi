package main

import (
	"context"
	"flag"
	"log"
	"time"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/httpapi"
)

func main() {
	apply := flag.Bool("apply", false, "rebuild risk registration only; retain and hide temporarily absent rows")
	tenant := flag.Int64("tenant-id", 0, "exact tenant for the requested cleanup")
	plan := flag.Int64("plan-id", 0, "exact plan for the requested cleanup")
	planName := flag.String("plan-name", "", "read-only plan name selector")
	semantic := flag.Bool("review-semantics", false, "with apply: recheck pending candidates using the configured model; never confirm for a user")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	count, pending, backup, err := store.LiveRiskReviewCounts(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("before rows=%d pending=%d recovery_rows=%d", count, pending, backup)
	if !*apply {
		plans, err := store.LiveRiskReviewPlans(ctx)
		if err != nil {
			log.Fatal(err)
		}
		for _, p := range plans {
			if (*planName == "" || p.Name == *planName) && (*tenant == 0 || p.TenantID == *tenant) && (*plan == 0 || p.PlanID == *plan) {
				log.Printf("candidate tenant_id=%d plan_id=%d name=%q", p.TenantID, p.PlanID, p.Name)
			}
		}
		return
	}
	if *tenant <= 0 || *plan <= 0 {
		log.Fatal("apply requires an exact tenant-id and plan-id; no global mutation")
	}
	server := httpapi.New(store, nil, nil, nil, nil, "production", "", "", nil, time.Minute)
	err = server.RebuildFactRiskReviews(ctx, *tenant, *plan)
	if err != nil {
		log.Fatal(err)
	}
	if *semantic {
		reviewed, reviewErr := server.RecheckPendingFactRiskReviews(ctx, *tenant, *plan)
		if reviewErr != nil {
			log.Fatal(reviewErr)
		}
		log.Printf("semantic_reviewed=%d user_confirmations_not_modified=true", reviewed)
	}
	count, pending, backup, err = store.LiveRiskReviewCounts(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("after tenant_id=%d plan_id=%d rows=%d pending=%d recovery_rows=%d", *tenant, *plan, count, pending, backup)
}
