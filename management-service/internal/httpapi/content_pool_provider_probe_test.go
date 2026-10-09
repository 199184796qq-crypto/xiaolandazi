package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
)

// Explicit paid protocol verification with synthetic 20-product source data.
// Only model traces are recorded; no content set, plan, voice or session changes.
func TestContentPoolProviderProbe(t *testing.T) {
	if os.Getenv("CONTENT_POOL_PROVIDER_PROBE") != "1" {
		t.Skip("explicit synthetic provider probe required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14, PlanID: 4})
	cfg, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source := contentpools.Sources{Products: []contentpools.Product{}, Rows: []contentpools.SourceRow{{ID: "knowledge:probe", Area: "knowledge"}}}
	facts := []contentpools.Fact{}
	all := []int64{}
	for i := int64(1); i <= 20; i++ {
		all = append(all, i)
		source.Products = append(source.Products, contentpools.Product{ID: i, Link: fmt.Sprint(i), Name: fmt.Sprintf("测试鸭子%d", i)})
		id := fmt.Sprintf("attributes:probe:%d", i)
		source.Rows = append(source.Rows, contentpools.SourceRow{ID: id})
		flavour := "麻辣"
		if i <= 2 {
			flavour = "五香"
		}
		facts = append(facts, contentpools.Fact{ID: id, Text: "- [商品事实] 口味=" + flavour, Sources: []string{id}, Owner: i, Products: []int64{i}})
	}
	facts = append(facts, contentpools.Fact{ID: "knowledge:probe", Text: "- [product/口味说明] 口味=五香", Sources: []string{"knowledge:probe"}, Products: all}, contentpools.Fact{ID: "price:probe", Text: "- [商品事实] 日常价=19元；仅适用于当前单件商品", Sources: []string{"products:probe"}, Owner: 1, Private: true, Products: []int64{1}})
	source.Rows = append(source.Rows, contentpools.SourceRow{ID: "products:probe"})
	outputs := []map[string]any{}
	defer func() {
		if path := os.Getenv("CONTENT_POOL_PROBE_RESULT"); path != "" {
			raw, _ := json.MarshalIndent(map[string]any{"passed": !t.Failed(), "outputs": outputs}, "", "  ")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	scoped, err := s.classifyContentFacts(ctx, cfg.SellingPointProfileID, source, facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range scoped {
		if f.ID == "knowledge:probe" {
			if len(f.Products) != 2 || !contentpools.Contains(f.Products, 1) || !contentpools.Contains(f.Products, 2) {
				t.Fatal("five-spice fact leaked to spicy products", f.Products)
			}
		}
	}
	outputs = append(outputs, map[string]any{"stage": "scope", "facts": scoped})
	t.Log("20-product classification: shared five-spice applies to 1/2, excludes 3..20")
	gen, _, err := loadReplaceablePromptAsset("CONTENT_POOL_TASK_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-task-generation.md", "内容池卖点母版")
	if err != nil {
		t.Fatal(err)
	}
	review, _, err := loadReplaceablePromptAsset("CONTENT_POOL_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-task-review.md", "内容池任务核对母版")
	if err != nil {
		t.Fatal(err)
	}
	pools := contentpools.GroupPools(scoped, source.Products, derivedHash(gen+review))
	testedCommon, testedPrivate := false, false
	for _, p := range pools {
		if (p.Owner == 0 && len(p.Products) == 2 && !testedCommon) || (p.Owner == 1 && !testedPrivate) {
			data, _ := json.Marshal(map[string]any{"pool": p, "products": source.Products})
			started := time.Now()
			result, _, e := s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "content_pool_protocol_probe", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: gen}, {Role: "user", Content: string(data)}}, 12000, false)
			if e != nil {
				t.Fatal(e)
			}
			outputs = append(outputs, map[string]any{"stage": "generated", "pool": p.Key, "raw": result.Text})
			p.Tasks, e = decodePoolTaskResponse(result.Text, p, nil, source.Products)
			if e != nil {
				t.Fatal("generated protocol", e)
			}
			data, _ = json.Marshal(map[string]any{"pool": p, "products": source.Products})
			result, _, e = s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "content_pool_review_probe", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: review}, {Role: "user", Content: string(data)}}, 12000, false)
			if e != nil {
				t.Fatal(e)
			}
			outputs = append(outputs, map[string]any{"stage": "reviewed", "pool": p.Key, "raw": result.Text})
			p.Tasks, e = decodePoolTaskResponse(result.Text, p, p.Tasks, source.Products)
			if e != nil {
				t.Fatal("reviewed protocol", e)
			}
			t.Logf("pool=%s tasks=%d elapsed_ms=%d source_refs_valid=true thinking=false", p.Key, len(p.Tasks), time.Since(started).Milliseconds())
			if p.Owner == 0 {
				testedCommon = true
			} else {
				testedPrivate = true
			}
		}
	}
	if !testedCommon || !testedPrivate {
		t.Fatal("missing common/private protocol coverage")
	}
}
