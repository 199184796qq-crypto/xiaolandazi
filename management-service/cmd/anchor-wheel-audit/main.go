// Read-only audit: no model requests, migrations, confirmations or DB writes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
)

func main() {
	tenant := flag.Int64("tenant", 0, "tenant")
	plan := flag.Int64("plan", 0, "plan")
	id := flag.Int64("asset", 0, "asset")
	flag.Parse()
	if *tenant <= 0 || *plan <= 0 || *id <= 0 {
		panic("explicit scope required")
	}
	s, err := db.Open(config.Load())
	check(err)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a, err := s.GetLiveSellingPointWheel(ctx, *tenant, *plan, *id)
	check(err)
	fields := func(raw string) []string {
		var obj struct {
			Items []map[string]json.RawMessage `json:"卖点列表"`
		}
		clean := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "```json"), "```"))
		if json.Unmarshal([]byte(clean), &obj) != nil {
			return nil
		}
		out := []string{}
		if len(obj.Items) > 0 {
			for k := range obj.Items[0] {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return out
	}
	check(json.NewEncoder(os.Stdout).Encode(map[string]any{"asset_id": a.ID, "created_at": a.CreatedAt, "prompt_source": a.PromptSource, "request_requires_target_audience": strings.Contains(a.RequestUser, "目标人群"), "request_requires_pain_point": strings.Contains(a.RequestUser, "痛点"), "raw_first_item_fields": fields(a.RawResponse), "stored_first_item_fields": fields(a.OriginalWheelJSON)}))
}
func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
