package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
)

// Read-only provider replay: no job, draft adoption, publication or audio.
func TestSimplePoolProvider(t *testing.T) {
	if os.Getenv("FACT_SPEECH_PREVIEW_PROBE") != "1" {
		t.Skip("explicit provider replay required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stop()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14, PlanID: 4, RunID: "simple-pool-content-replay"})
	cfg, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	gen, _, err := loadReplaceablePromptAsset("CONTENT_POOL_SIMPLE_GENERATION_PATH", "configs/prompts/anchor-style/content-pool-simple-generation.md", "内容池简洁生成")
	if err != nil {
		t.Fatal(err)
	}
	review, _, err := loadReplaceablePromptAsset("CONTENT_POOL_SIMPLE_REVIEW_PATH", "configs/prompts/anchor-style/content-pool-simple-review.md", "内容池单条核对")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, fact, good, bad string }{
		{"熟食", "熟食开袋即可直接吃，未确认其他烹饪方法。", "开袋直接食用，省去烹饪步骤。", "开袋即可吃，也可拿来煮汤炖菜。"},
		{"杯子", "杯身钢材牌号为304，未提供保温时长。", "杯身使用304，拿着沉甸甸，质感扎实。", "杯身304，可以保温12小时。"},
		{"服务", "购买课包7天内且未使用任何课时可以申请退款。", "未使用课时且在购买7天内可以申请退款。", "用了不满意随时退款。"},
	}
	for _, tc := range cases {
		p := contentpools.Pool{Key: "probe:" + tc.name, Label: tc.name, Owner: 1, Products: []int64{1}, Facts: []contentpools.Fact{{ID: "program-origin", Text: tc.fact, Products: []int64{1}}}}
		products := []contentpools.Product{{ID: 1, Link: "1", Name: tc.name}}
		good, reason, e := s.reviewSimplePoolContent(ctx, cfg.SellingPointProfileID, review, p0, p, products, nil, poolContent{Point: "当前说明", Angle: tc.good})
		if e != nil || reason != "" {
			t.Fatal("valid content blocked", tc.name, reason, e)
		}
		bad, reason, e := s.reviewSimplePoolContent(ctx, cfg.SellingPointProfileID, review, p0, p, products, nil, poolContent{Point: "待核对说明", Angle: tc.bad})
		// A repaired candidate is also acceptable; the bad assertion must disappear.
		if e != nil {
			t.Fatal(tc.name, e)
		}
		if reason == "" && bad.Angle == tc.bad {
			t.Fatal("unsupported claim unchanged", tc.name, bad)
		}
		t.Log("PAIRED_REVIEW", tc.name, good.Angle, "invalid rejected or repaired", reason, bad.Angle)
	}
	p := contentpools.Pool{Key: "probe:gift", Label: "礼品装", Owner: 2, Products: []int64{2}, Facts: []contentpools.Fact{{ID: "price", Text: "礼品装960g整只，日常价92.6元", Products: []int64{2}}, {ID: "taste", Text: "麻辣和五香两种口味可选", Products: []int64{2}}}}
	source := contentpools.Sources{Products: []contentpools.Product{{ID: 2, Link: "2", Name: "礼品装"}}}
	old := []contentpools.Task{{ID: "old-empty", Point: "包邮福利", Angle: "", Sources: nil}, {ID: "old-price", Point: "规格", Angle: "960g整只日常价92.6元", Sources: []string{"price"}}}
	notes, e := s.generateSimplePool(ctx, cfg.SellingPointProfileID, gen, review, p0, &p, old, source, func() error { return nil })
	if e != nil || len(p.Tasks) == 0 {
		t.Fatal("approved siblings lost", p, notes, e)
	}
	for _, task := range p.Tasks {
		if len(task.Sources) == 0 || task.Angle == "" {
			t.Fatal("program provenance missing", task)
		}
	}
	if len(p.Tasks) < 3 || p.Tasks[0].ID != "old-price" {
		t.Fatal("valid previous content or selected units lost", p.Tasks, notes)
	}
	raw, _ := json.Marshal(map[string]any{"tasks": p.Tasks, "notices": notes})
	t.Log("SIMPLE_POOL", string(raw))
	scoped, notes, e := s.classifySimplePoolFacts(ctx, cfg.SellingPointProfileID, contentpools.Sources{Products: []contentpools.Product{{ID: 1, Link: "1", Name: "燕麦饼干"}, {ID: 2, Link: "2", Name: "保温杯"}}}, []contentpools.Fact{{ID: "grain", Text: "燕麦饼干使用燕麦原料"}, {ID: "steel", Text: "保温杯杯身使用304不锈钢"}}, p0)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range scoped {
		if len(f.Products) != 1 || f.ID == "grain" && f.Products[0] != 1 || f.ID == "steel" && f.Products[0] != 2 {
			t.Fatal("cross-product association", f, notes)
		}
	}
	t.Log("SCOPES", scoped, notes)
	for _, raw := range []string{"讲清960g整只日常价92.6元，麻辣与五香可选。", "- 960g整只，日常价92.6元。\n- 麻辣和五香可选。", `{"任意外壳":"960g整只，日常价92.6元，麻辣和五香可选。"}`, `{"tasks":[{"angle":"960g整只，日常价92.6元，麻辣和五香可选。","source_ids":["forged"]}]}`, `{"任意外壳":"960g整只，日常价92.6元，麻辣和五香可选。"`} {
		candidate := poolContent{Point: "程序维护标题", Angle: poolReplyContent(raw)}
		_, reason, e := s.reviewSimplePoolContent(ctx, cfg.SellingPointProfileID, review, p0, p, source.Products, nil, candidate)
		if e != nil || reason != "" {
			t.Fatal("valid content blocked by layout", raw, reason, e)
		}
		t.Log("CONTENT_LAYOUT_APPROVED", candidate.Angle)
	}
}
