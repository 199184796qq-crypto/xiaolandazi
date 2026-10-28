// Command question-gap-probe appends an uncovered question twice and reports
// whether dedupe works.
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
	tenant := flag.Int64("tenant", 0, "tenant")
	plan := flag.Int64("plan", 0, "plan")
	qtype := flag.String("type", "fact", "question type")
	question := flag.String("question", "", "question text")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		fmt.Println("db error:", err)
		return
	}
	defer store.Close()
	first, err := store.AppendQuestionGap(ctx, *tenant, *plan, *qtype, *question)
	fmt.Printf("first inserted=%t err=%v\n", first, err)
	second, err := store.AppendQuestionGap(ctx, *tenant, *plan, *qtype, *question)
	fmt.Printf("second inserted=%t err=%v\n", second, err)
}
