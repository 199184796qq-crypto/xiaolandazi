// One explicit, scoped legacy migration; content and provenance remain intact.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/lecturetask"
)

func main() {
	specPath := flag.String("spec", "", "program-owned scope and expected records")
	apply := flag.Bool("apply", false, "apply explicit user-approved hierarchy")
	flag.Parse()
	if *specPath == "" || !*apply {
		panic("explicit migration arguments required")
	}
	var spec struct {
		Tenant    int64              `json:"tenant_id"`
		Plan      int64              `json:"plan_id"`
		Actor     int64              `json:"actor_id"`
		Task      int64              `json:"task_id"`
		Revision  int64              `json:"revision"`
		Subject   string             `json:"subject"`
		SourceSHA string             `json:"source_sha"`
		Root      lecturetask.Fact   `json:"root"`
		Details   []lecturetask.Fact `json:"details"`
	}
	raw, err := os.ReadFile(*specPath)
	if err != nil || json.Unmarshal(raw, &spec) != nil {
		panic("migration specification invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic("database unavailable")
	}
	defer store.Close()
	lib, err := store.LectureLibrary(ctx, spec.Tenant, spec.Plan, true)
	if err != nil {
		panic("lecture library unavailable")
	}
	var chosen *lecturetask.Task
	for i := range lib.Tasks {
		if lib.Tasks[i].ID == spec.Task {
			chosen = &lib.Tasks[i]
		}
	}
	if chosen == nil || !chosen.Valid || chosen.Revision != spec.Revision || chosen.RootFact != nil {
		panic("lecture state changed")
	}
	digest := sha256.Sum256([]byte(chosen.Source))
	if hex.EncodeToString(digest[:]) != spec.SourceSHA {
		panic("original source changed")
	}
	found := false
	for _, f := range chosen.Facts {
		if f.ID == spec.Root.ID && f.Version == spec.Root.Version && f.Text == spec.Root.Text {
			found = true
		}
	}
	if !found {
		panic("root changed")
	}
	ordinary, err := store.ListLiveAgentPlanFacts(ctx, spec.Tenant, spec.Plan)
	if err != nil {
		panic("ordinary facts unavailable")
	}
	detailIDs := []int64{}
	for _, expected := range spec.Details {
		found = false
		for _, f := range ordinary {
			if f.ID == expected.ID && f.VersionNo == expected.Version && f.Value == expected.Text && f.Key == expected.Name {
				found = true
			}
		}
		if !found {
			panic("additional source fact changed")
		}
		detailIDs = append(detailIDs, expected.ID)
	}
	if err = store.AttachLectureSubject(ctx, spec.Tenant, spec.Plan, spec.Actor, spec.Task, spec.Revision, spec.Root.ID, spec.Subject, detailIDs...); err != nil {
		panic("subject binding failed: " + err.Error())
	}
	lib, err = store.LectureLibrary(ctx, spec.Tenant, spec.Plan, true)
	if err != nil {
		panic("verification failed")
	}
	ordinary, err = store.ListLiveAgentPlanFacts(ctx, spec.Tenant, spec.Plan)
	if err != nil {
		panic("verification failed")
	}
	for _, f := range ordinary {
		for _, id := range detailIDs {
			if f.ID == id {
				panic("private details leaked")
			}
		}
	}
	for _, task := range lib.Tasks {
		if task.ID == spec.Task {
			if !task.Valid || task.RootFact == nil || task.RootFact.ID != spec.Root.ID || task.Subject != spec.Subject || task.Revision != spec.Revision+1 {
				panic("subject binding verification failed")
			}
			b, _ := json.Marshal(map[string]any{"tenant_id": spec.Tenant, "plan_id": spec.Plan, "task_id": task.ID, "revision": task.Revision, "subject": task.Subject, "root_fact_id": task.RootFact.ID, "secondary_count": len(task.Facts), "ordinary_count": len(ordinary), "valid": task.Valid, "source_preserved": task.Source == chosen.Source})
			fmt.Println(string(b))
			return
		}
	}
	panic("verified task missing")
}
