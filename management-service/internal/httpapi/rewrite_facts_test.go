package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
	"livecompanion/management/internal/scriptchain"
)

func TestRewriteFactsProjection(t *testing.T) {
	catalog := model.MaterialObjectCatalog{Directory: model.MaterialObjectDirectory{
		GlobalDisplayName: "通用政策",
		Objects:           []model.MaterialNamedObject{{ID: 11, DisplayName: "家庭套餐"}, {ID: 12, DisplayName: "双人券"}},
		RemovedLinkIDs:    []int64{100},
		LinkDisplayNames:  map[int64]string{101: "单人券"},
	}}
	links := []db.QALink{{ID: 100, LinkKey: "1"}, {ID: 101, LinkKey: "2"}}
	claim := func(a, v, kind string, id int64) model.PlanMaterialClaim {
		c := model.PlanMaterialClaim{Status: "active", Attribute: a, Topic: a, Value: v, ScopeKind: kind}
		if kind == "object" {
			c.ObjectTargets = []model.MaterialObjectTarget{{Kind: kind, ObjectID: id}}
		}
		if kind == "product" {
			c.ProductLinkID = id
		}
		return c
	}
	double := claim("容量", "500mL（两瓶）", "object", 11)
	double.ObjectTargets = append(double.ObjectTargets, model.MaterialObjectTarget{Kind: "object", ObjectID: 12})
	claims := []model.PlanMaterialClaim{
		claim("售价", "87.60", "object", 11),
		claim("售价", "92.5元", "object", 12),
		claim("售价", "48.8", "product", 101),
		claim("售价", "111元", "object", 999),
		claim("售价", "222元", "product", 100),
		claim("售价", "333元", "unresolved", 0),
		claim("出行方式", "任选其一", "plan", 0),
		claim("适用时段", "分时,周末", "object", 11),
		claim("颜色", "[\"红\",\"蓝\",\"红\"]", "object", 11),
		claim("清洗工序", "清水预洗5分钟，两次漂洗，热风干燥", "object", 11),
		double, claim("容量", "500mL（两瓶）", "object", 12),
	}
	blocked := claim("维修承诺", "永久有效", "plan", 0)
	blocked.P0Hits = []map[string]any{{"rule": "absolute_claim"}}
	claims = append(claims, blocked)
	facts, skipped := projectRewriteFacts(claims, catalog, links)
	if skipped != 4 || len(facts) != 8 {
		t.Fatalf("facts=%+v skipped=%d", facts, skipped)
	}
	for _, want := range []struct{ object, attr, value string }{
		{"家庭套餐", "售价", "\"87.60\""}, {"双人券", "售价", "\"92.5元\""}, {"单人券", "售价", "\"48.8\""},
		{"通用政策", "出行方式", "\"任选其一\""}, {"家庭套餐", "适用时段", "\"分时,周末\""},
		{"家庭套餐", "颜色", "[\"红\",\"蓝\"]"}, {"双人券、家庭套餐", "容量", "\"500mL（两瓶）\""},
		{"家庭套餐", "清洗工序", "[\"清水预洗5分钟\",\"两次漂洗\",\"热风干燥\"]"},
	} {
		if !slices.ContainsFunc(facts, func(f rewriteFact) bool {
			return f.Object == want.object && f.Attribute == want.attr && string(f.Value) == want.value
		}) {
			t.Errorf("missing %+v in %+v", want, facts)
		}
	}
}

func TestRewriteFactsRestrictions(t *testing.T) {
	makeClaim := func(a, v, source, cond, ex string) model.PlanMaterialClaim {
		return model.PlanMaterialClaim{Attribute: a, Value: v, ScopeKind: "plan", SourceSentence: source,
			Conditions: json.RawMessage(cond), MaterialClaimDetails: model.MaterialClaimDetails{Exceptions: json.RawMessage(ex)}}
	}
	claims := []model.PlanMaterialClaim{
		makeClaim("预约方式", "可预约", "", `[{"field":"amount","op":"gte","values":[99]}]`, `{"field":"channel","op":"not_in","values":["柜台"],"universe":"指定渠道"}`),
		makeClaim("服务时段", "当日", "高新区当日，其他地区次日", `[{"field":"地区","op":"in","values":["高新区"],"raw":"高新区"}]`, ""),
		makeClaim("服务时段", "次日", "高新区当日，其他地区次日", `[{"field":"region","op":"等于","values":["其他地区"],"raw":"其他地区"}]`, ""),
		makeClaim("费用", "退回", "不相干的其他地区", `[{"field":"地区","op":"in","values":["其他地区"],"raw":"其他地区"}]`, ""),
		makeClaim("核验方式", "由人工处理", "", `{"any":[{"field":"包装状态","op":"eq","value":"封口完好"},{"raw":"已人工核实"}]}`, ""),
		makeClaim("服务方式", "到店核销", "", "", ""),
	}
	claims[5].Polarity = "negative"
	facts, skipped := projectRewriteFacts(claims, model.MaterialObjectCatalog{}, nil)
	if skipped != 1 || len(facts) != 5 {
		t.Fatalf("facts=%+v skipped=%d", facts, skipped)
	}
	if facts[0].Condition != "金额不少于99" || facts[0].Exception != "渠道不属于柜台（指定渠道范围内）" {
		t.Errorf("lost predicates: %+v", facts[0])
	}
	if facts[2].Condition != "收货地区不属于高新区" || string(facts[2].Value) != "\"次日\"" {
		t.Errorf("lost complement: %+v", facts[2])
	}
	if facts[3].Condition != "任一满足（包装方式为封口完好；已人工核实）" {
		t.Errorf("lost compound predicate: %+v", facts[3])
	}
	if facts[4].Polarity != "否定" || string(facts[4].Value) != "\"到店核销\"" {
		t.Errorf("lost polarity: %+v", facts[4])
	}
}

func TestRewriteFactsSpecificDuplicateEvidence(t *testing.T) {
	const source = "城区分店位于南街12号。"
	claim := model.PlanMaterialClaim{
		ClaimKey: "branch_address", Attribute: "门店地址", Topic: "门店位置", Value: "南街12号",
		SourceSentence: source, SourceQuote: "南街12号", ScopeKind: "plan",
	}
	branch := claim
	branch.Attribute, branch.ScopeKind, branch.StoreName = "地址", "store", "城区分店"
	another := claim
	another.SourceSentence = "所有线下订单统一从南街12号发出。"
	another.Attribute, another.Topic = "发货地址", "发货地址"
	facts, _ := projectRewriteFacts([]model.PlanMaterialClaim{claim, branch, another}, model.MaterialObjectCatalog{}, nil)
	if len(facts) != 2 || facts[0].Object != "城区分店" || facts[1].Object != "全店通用" {
		t.Fatalf("%+v", facts)
	}
}

func TestChainRewritePromptUsesOnlyBlueprintRedLinesAndFacts(t *testing.T) {
	s := planprompt.Sections{PersonaMD: "说话自然", FactsMD: "【商品卡】\n旧价87.6\n\n【用词消歧】\n保留术语\n\n【数据使用规则】\n遵循价格规则", RedLinesMD: "禁止保证"}
	prompt := planprompt.CompileRewritePlanning(s, "保留写作规划")
	doc := scriptRewriteSnapshot{Prompt: prompt, Facts: s.FactsMD}
	pack := `[{"对象":"双人券","属性":"价格","值":"92.5"}]`
	redlines, ok := backgroundRewriteRedLines(doc)
	if !ok || redlines != "禁止保证" {
		t.Fatalf("legacy snapshot redlines: %q", redlines)
	}
	messages := scriptRewriteMessages(renderChainRewritePrompt("", scriptchain.DefaultRequirement(700), "", redlines, pack))
	if messages[0].Content != planprompt.RewriteOutputContract {
		t.Fatal("changed output contract")
	}
	body := messages[1].Content
	if !strings.HasPrefix(body, "【写作要求】") || !strings.HasSuffix(body, "【事实清单】\n"+pack) || strings.Count(body, pack) != 1 || !strings.Contains(body, "【红线禁止】\n禁止保证") {
		t.Fatalf("unexpected writer input: %q", body)
	}
	for _, forbidden := range []string{"旧价87.6", "说话自然", "保留术语", "遵循价格规则", "样本话术", "保留写作规划"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("old input leaked: %q", forbidden)
		}
	}
}

func TestRewriteFactsCacheEmptyFailureAndPlanIsolation(t *testing.T) {
	cache := &rewriteFactsCache{local: scriptchain.NewMemoryKV()}
	value := json.RawMessage(`[{"对象":"标准套餐","属性":"价格","值":"89.5"}]`)
	calls := 0
	var buildErr error
	build := func(context.Context, int64, int64) (json.RawMessage, error) {
		calls++
		return value, buildErr
	}
	check := func(tenant, plan int64, refresh bool, want string, wantCalls int, wantErr error) {
		t.Helper()
		got, err := cache.get(t.Context(), tenant, plan, refresh, build)
		if !errors.Is(err, wantErr) || string(got) != want || calls != wantCalls {
			t.Fatalf("cache get returned %s, %v with %d builds", got, err, calls)
		}
	}
	price := string(value)
	check(1, 2, false, price, 1, nil)
	value = json.RawMessage(`[]`)
	check(1, 2, false, price, 1, nil)
	check(2, 2, false, `[]`, 2, nil)
	check(1, 3, false, `[]`, 3, nil)
	buildErr = errors.New("mysql unavailable")
	value = nil
	check(1, 2, true, "", 4, buildErr)
	check(1, 2, false, price, 4, nil)
	buildErr = nil
	value = json.RawMessage(`[]`)
	check(1, 2, true, `[]`, 5, nil)
	check(1, 2, false, `[]`, 5, nil)
}

func TestRewriteFactsRedisAcrossManagementInstances(t *testing.T) {
	addr := os.Getenv("REWRITE_FACTS_TEST_REDIS")
	if addr == "" {
		t.Skip("requires an isolated test Redis")
	}
	ctx := t.Context()
	client := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	t.Cleanup(func() { _ = client.Close() })
	first := &rewriteFactsCache{redis: client}
	second := &rewriteFactsCache{redis: client}
	value := json.RawMessage(`[{"对象":"套装","属性":"价格","值":"125元"}]`)
	build := func(context.Context, int64, int64) (json.RawMessage, error) { return value, nil }
	neverBuild := func(context.Context, int64, int64) (json.RawMessage, error) {
		t.Fatal("Redis hit fell back to MySQL")
		return nil, nil
	}
	const tenant, plan int64 = 189021, 799159
	if _, err := first.get(ctx, tenant, plan, true, build); err != nil {
		t.Fatal(err)
	}
	if got, err := second.get(ctx, tenant, plan, false, neverBuild); err != nil || string(got) != string(value) {
		t.Fatalf("Redis cache after restart: %s, %v", got, err)
	}
	chainCache := scriptchain.NewStore(scriptchain.NewRedisKV(client), 12*time.Hour)
	old := scriptchain.Piece{Tenant: tenant, Plan: plan, Session: 6, ID: 3, ContentHash: scriptchain.ContentHash("原文")}
	next := old
	next.Session++
	for _, piece := range []scriptchain.Piece{old, next} {
		if err := chainCache.Save(ctx, piece, scriptchain.Analysis{Chains: make([]scriptchain.Chain, 10)}); err != nil {
			t.Fatal(err)
		}
		chainCache.MarkUsed(ctx, piece, nil, 2)
	}
	if err := chainCache.ClearSession(ctx, tenant, old.Session); err != nil {
		t.Fatal(err)
	}
	if _, hit, err := chainCache.Load(ctx, old); err != nil || hit || len(chainCache.RecentUsed(ctx, old)) > 0 {
		t.Fatalf("old session cache remains: hit=%t err=%v", hit, err)
	}
	if _, hit, err := chainCache.Load(ctx, next); err != nil || !hit {
		t.Fatalf("different session cache lost: hit=%t err=%v", hit, err)
	}
	if got, err := second.get(ctx, tenant, plan, false, neverBuild); err != nil || string(got) != string(value) {
		t.Fatalf("session cleanup removed plan facts: %s, %v", got, err)
	}
	value = json.RawMessage(`[]`)
	if _, err := first.get(ctx, tenant, plan, true, build); err != nil {
		t.Fatal(err)
	}
	if got, err := second.get(ctx, tenant, plan, false, neverBuild); err != nil || string(got) != `[]` {
		t.Fatalf("old facts reappeared after clear: %s, %v", got, err)
	}
}

func TestRewriteFactsProductionReplay(t *testing.T) {
	path := os.Getenv("REWRITE_FACTS_REPLAY_FILE")
	if path == "" {
		t.Skip("no local replay file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Claims        []model.PlanMaterialClaim
		Catalog       model.MaterialObjectCatalog
		Links         []db.QALink
		Prompt, Facts string
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	facts, skipped := projectRewriteFacts(fixture.Claims, fixture.Catalog, fixture.Links)
	data, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ object, attr, value string }{
		{"1号链接", "价格", "\"87.6\""}, {"2号链接", "价格", "\"92.5\""}, {"3号链接", "价格", "\"48.8\""},
	} {
		if !slices.ContainsFunc(facts, func(f rewriteFact) bool {
			return f.Object == check.object && f.Attribute == check.attr && string(f.Value) == check.value
		}) {
			t.Fatalf("missing %+v", check)
		}
	}
	for _, check := range []struct{ attr, cond, except string }{
		{"包邮范围", "", "西藏"}, {"空运费", "西藏", ""},
		{"包装方式", "收货地区不属于川渝地区", ""}, {"配送时效·送达时效", "收货地区不属于川渝地区", ""},
	} {
		if !slices.ContainsFunc(facts, func(f rewriteFact) bool {
			return f.Attribute == check.attr && strings.Contains(f.Condition, check.cond) && strings.Contains(f.Exception, check.except)
		}) {
			t.Fatalf("restriction missing: %+v", check)
		}
	}
	if !slices.ContainsFunc(facts, func(f rewriteFact) bool {
		return f.Attribute == "核心工艺" && string(f.Value) == `["老卤汤慢炖4个小时","两遍油炸","出锅裹上秘制红油高温锁汁定型"]`
	}) {
		t.Fatal("process steps missing or reordered")
	}
	if strings.Count(string(data), "南充红花街一号") != 1 || strings.Contains(string(data), "总店位于南充红花街一号") || strings.Contains(string(data), "和刚出锅一模一样") {
		t.Fatal("duplicate value or leaked source sentence")
	}
	redlines, ok := backgroundRewriteRedLines(scriptRewriteSnapshot{Prompt: fixture.Prompt, Facts: fixture.Facts})
	prompt := renderChainRewritePrompt("", scriptchain.DefaultRequirement(700), "", redlines, string(data))
	if !ok || redlines == "" || strings.Count(prompt, string(data)) != 1 || strings.Contains(prompt, "价格：92.6") || strings.Contains(prompt, "【样本话术】") {
		t.Fatal("production replay has duplicate/old facts, sample text or missing redlines")
	}
	t.Logf("claims=%d facts=%d skipped=%d fact_pack_bytes=%d old_facts_section_bytes=%d rewrite_template_bytes_before=%d rewrite_template_bytes_after=%d", len(fixture.Claims), len(facts), skipped, len(data), len(fixture.Facts), len(fixture.Prompt), len(prompt))
}
