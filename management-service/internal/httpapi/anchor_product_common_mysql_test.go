package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type productCommonTestTransport struct{ target *url.URL }

func (r productCommonTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u := *req.URL
	copy.URL = &u
	copy.URL.Scheme = r.target.Scheme
	copy.URL.Host = r.target.Host
	return (&http.Transport{}).RoundTrip(copy)
}

// Always creates a new random local schema. Never selects the configured
// business database, and never makes paid model or playback calls.
func TestProductCommonInitializationAndVersionMySQL(t *testing.T) {
	if os.Getenv("PRODUCT_COMMON_DB_TEST") != "1" || os.Getenv("PRODUCT_COMMON_FAKE_TRANSPORT") != "1" {
		t.Skip("explicit isolated local MySQL test with fixture-only transport overlay")
	}
	cfg := config.Load()
	if cfg.DBHost != "127.0.0.1" && cfg.DBHost != "localhost" {
		t.Fatal("local database required")
	}
	dsn := mysql.NewConfig()
	dsn.User = cfg.DBUser
	dsn.Passwd = cfg.DBPassword
	dsn.Net = "tcp"
	dsn.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	dsn.ParseTime = true
	admin, e := sql.Open("mysql", dsn.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	schema := "product_common_test_" + strings.ToLower(anchorcycle.ID())
	if _, e = admin.Exec("CREATE DATABASE `" + schema + "` CHARACTER SET utf8mb4"); e != nil {
		t.Fatal(e)
	}
	defer func() { _, _ = admin.Exec("DROP DATABASE `" + schema + "`") }()
	cfg.DBName = schema
	store, e := appdb.Open(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec("CREATE TABLE `" + schema + "`.core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED,status VARCHAR(32))"); e != nil {
		t.Fatal(e)
	}
	// Live-runtime migration also upgrades the commercial quota table. This
	// isolated schema has no commercial data; supply its required base table.
	if _, e = admin.Exec("CREATE TABLE `" + schema + "`.quota_buckets (id BIGINT UNSIGNED PRIMARY KEY, remaining_seconds BIGINT UNSIGNED NOT NULL)"); e != nil {
		t.Fatal(e)
	}
	for _, migrate := range []func(context.Context) error{store.MigrateSystemSettings, store.MigrateLiveRuntime, store.MigrateContentPools, store.MigrateLectureTasks, store.MigrateContentCorrections} {
		if e = migrate(ctx); e != nil {
			t.Fatal(e)
		}
	}
	dsn.DBName = schema
	rawDB, e := sql.Open("mysql", dsn.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	defer rawDB.Close()
	t.Setenv("MODEL_CONFIG_ENCRYPTION_KEY", strings.Repeat("test-only-", 5))
	if e = store.MigrateSpeechModels(ctx); e != nil {
		t.Fatal(e)
	}
	base, _, e := loadSellingPointExtractionPrompt()
	if e != nil {
		t.Fatal(e)
	}
	pack, e := loadIndustryPack("local_service", "restaurant")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(base, "{{CATEGORY_MUST_CHECK}}") {
		t.Fatal("base missing industry injection")
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []agentgateway.Message `json:"messages"`
			Thinking *bool                  `json:"enable_thinking"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Thinking == nil || *req.Thinking {
			t.Error("wire request not non-thinking")
			http.Error(w, "invalid", 400)
			return
		}
		var packet map[string]any
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &packet)
		if packet["effective_p0"] == nil && !strings.Contains(req.Messages[0].Content, "effective_p0") {
			t.Error("effective P0 missing")
		}
		answer := `{"归属":"公共"}`
		if packet["operation"] == nil {
			if !strings.Contains(req.Messages[0].Content, strings.TrimSpace(pack.SellingDimensions)) {
				t.Error("selected industry MD not injected")
			}
			answer = `{"卖点列表":[{"卖点名":"公共停车","归属":"公共","目标人群":"到店客人","痛点":"停车疑问","角度":["说明门店免费停车"]},{"卖点名":"套餐内容","归属":"商品","目标人群":"选购套餐的人","痛点":"不清楚内容","角度":["核对当前套餐内容"]}],"事实补充建议":[]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": answer}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10}})
	}))
	defer provider.Close()
	target, _ := url.Parse(provider.URL)
	beforeTransport := http.DefaultTransport
	http.DefaultTransport = productCommonTestTransport{target}
	defer func() { http.DefaultTransport = beforeTransport }()
	profile := agentgateway.SpeechModel{ID: "fixture", Name: "fixture", Protocol: "openai_chat", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3.7-flash", Enabled: true, TimeoutMS: 10000, MaxTokens: 12000}
	models, e := store.SpeechModels(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.SaveSpeechModels(ctx, agentgateway.SpeechModelsInput{Revision: models.Revision, DefaultID: "fixture", SystemDefaultID: "fixture", ReviewDefaultID: "fixture", Profiles: []agentgateway.SpeechModelInput{{SpeechModel: profile, NewAPIKey: "fixture-only"}}}, 1); e != nil {
		t.Fatal(e)
	}
	plan, e := store.CreateLiveAgentPlan(ctx, 14, 1, model.CreateLiveAgentPlanInput{Name: "隔离验证"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = rawDB.Exec("UPDATE live_agent_plans SET biz_domain='local_service',local_service_industry='restaurant' WHERE id=?", plan.ID); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"1号链接", "2号链接"} {
		if _, e = store.AdoptLiveAgentPlanProductLink(ctx, 14, plan.ID, 1, model.LiveAgentPlanProductLinkCandidate{LinkKey: key, ProductName: key + "套餐", Spec: "一份", DailyPrice: "99", ReviewBucket: "safe", RoomRoles: []string{"ordinary"}}, "manual:test"); e != nil {
			t.Fatal(e)
		}
	}
	for _, fact := range []model.LiveAgentPlanFactCandidate{{Category: "other", Key: "停车", Value: "门店免费停车", ReviewBucket: "safe"}, {Category: "other", Key: "用券", Value: "每桌限用一张券", SafeRewrite: "【必说】每桌限用一张券\n【可说】不能叠加使用", ReviewBucket: "safe"}} {
		if _, e = store.AdoptLiveAgentPlanFact(ctx, 14, plan.ID, 1, fact, "manual:test"); e != nil {
			t.Fatal(e)
		}
	}
	server := &Server{store: store}
	if _, _, e = server.initializeSellingPointWheel(ctx, 14, plan.ID, 1); e != nil {
		t.Fatal("initialize", e)
	}
	p0, p1, p2, e := loadRiskAnnotationMothers()
	if e != nil {
		t.Fatal(e)
	}
	source, e := server.loadDerivedInput(ctx, 14, plan.ID, p0, p1, p2)
	if e != nil {
		t.Fatal(e)
	}
	asset, e := store.GetActiveLiveSellingPointWheel(ctx, 14, plan.ID)
	if e != nil {
		t.Fatal(e)
	}
	links, e := store.ListLiveAgentPlanProductLinks(ctx, 14, plan.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, link := range links {
		if len(link.SellingPoints) != 1 || link.SellingPoints[0].SellingPoint != "套餐内容" {
			t.Fatal("public duplicate saved into product", link)
		}
	}
	if _, e = store.GetPlanAnalysisAsset(ctx, 15, plan.ID, "product_common_ready", productCommonReadyKey(asset, links)); e == nil {
		t.Fatal("cross tenant initialization leaked")
	}
	safety := model.LiveDerivedSafetyAsset{SafeFacts: source.Facts, SafeWheelJSON: asset.EffectiveWheelJSON()}
	scheme := liveAnchorScheme{Version: 1, StyleID: 1, ScorecardID: 1, WheelID: asset.ID, Config: anchorcycle.Config{Rolling: true, MaxChars: 300}}
	input := model.CreateLiveAgentPlanVersionInput{GenerationContext: map[string]any{"content_mode": model.LiveContentAIDynamic, "anchor_generation": scheme}}
	if e = server.captureRealtimeProductFocus(ctx, 14, plan.ID, &input, safety); e != nil {
		t.Fatal("capture", e)
	}
	version, e := store.CreateLiveAgentPlanVersion(ctx, 14, plan.ID, 1, input)
	if e != nil {
		t.Fatal("durable version", e)
	}
	loaded, e := realtimeScheme(version.GenerationContext)
	if e != nil {
		t.Fatal(e)
	}
	state, e := server.prepareSavedAnchorTaskState(ctx, 14, plan.ID, nil, loaded, safety)
	if e != nil {
		t.Fatal(e)
	}
	if len(state.Queue) != 3 || len(state.ProductFocus.Facts) != 4 || len(state.ProductFocus.Products[0].Facts) != 3 {
		t.Fatal("save/reload lost material", state.ProductFocus)
	}
	frozen, _ := json.Marshal(loaded)
	if _, e = rawDB.Exec("UPDATE live_agent_plans SET local_service_industry='education' WHERE id=?", plan.ID); e != nil {
		t.Fatal(e)
	}
	if e = server.captureRealtimeProductFocus(ctx, 14, plan.ID, &input, safety); e == nil {
		t.Fatal("industry switch reused previous industry initialization")
	}
	if _, e = rawDB.Exec("UPDATE live_agent_plans SET local_service_industry='restaurant' WHERE id=?", plan.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = store.AdoptLiveAgentPlanFact(ctx, 14, plan.ID, 1, model.LiveAgentPlanFactCandidate{Category: "other", Key: "新增", Value: "周末营业", ReviewBucket: "safe"}, "manual:new"); e != nil {
		t.Fatal(e)
	}
	if e = server.captureRealtimeProductFocus(ctx, 14, plan.ID, &input, safety); e == nil {
		t.Fatal("changed facts reused stale initialization")
	}
	after, _ := json.Marshal(loaded)
	if string(frozen) != string(after) {
		t.Fatal("old saved version changed")
	}
	t.Log(fmt.Sprintf("isolated initialize → classify → product/common pools → capture → durable version %d → actual preparation passed; schema removed on exit", version.ID))
}
