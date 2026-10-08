package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Explicit opt-in: paid ordinary API calls; no TTS, live session or plan edit.
// The installation branch saves only the separately scoped strategy asset.
func TestContentStrategyProviderProbe(t *testing.T) {
	if os.Getenv("CONTENT_STRATEGY_PROBE") != "1" {
		t.Skip("explicit operational probe required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := strconv.ParseInt(os.Getenv("CONTENT_STRATEGY_TENANT"), 10, 64)
	room, _ := strconv.ParseInt(os.Getenv("CONTENT_STRATEGY_INSTALL_ROOM"), 10, 64)
	if room > 0 {
		if tenant <= 0 {
			t.Fatal("explicit tenant required")
		}
		version, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenant, room)
		if err != nil {
			t.Fatal(err)
		}
		ref := asSupportMap(version.GenerationContext["derived_safety"])
		hash, _ := ref["source_sha256"].(string)
		safety, err := store.GetLiveDerivedSafety(ctx, tenant, version.PlanID, hash)
		if err != nil {
			t.Fatal(err)
		}
		before := s.loadContentStrategy(ctx, tenant, version.PlanID, safety.SafeFacts)
		if before != nil {
			t.Logf("existing_strategy tenant=%d room=%d plan=%d enabled=%v rows=%d", tenant, room, version.PlanID, before.Enabled, len(before.Rows))
			return
		}
		snapshot, err := s.prepareContentStrategy(ctx, tenant, version.PlanID, safety.SafeFacts, settings.SellingPointProfileID, false)
		if err != nil {
			t.Fatal(err)
		}
		latest, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenant, room)
		if err != nil || latest.ID != version.ID {
			t.Fatal("published version changed; no strategy saved", err)
		}
		if err = s.saveContentStrategy(ctx, tenant, version.PlanID, snapshot); err != nil {
			t.Fatal(err)
		}
		loaded := s.loadContentStrategy(ctx, tenant, version.PlanID, safety.SafeFacts)
		if loaded == nil || !loaded.Enabled || len(loaded.Rows) != len(snapshot.Rows) {
			t.Fatal("strategy readback mismatch")
		}
		t.Logf("installed_strategy tenant=%d room=%d plan=%d published_version=%d facts_sha=%s purposes=%d rows=%d model=%s; existing plan/wheel/voice/session unchanged", tenant, room, version.PlanID, version.ID, snapshot.FactsSHA, len(snapshot.Purposes), len(snapshot.Rows), snapshot.Model)
		return
	}
	facts := "# 正式事实包\n方案：绸都杨鸭子\n- [商品卡] 名称=绸都杨鸭子；简称=杨鸭子；商品=油炸鸭；工艺=现卤现炸；一只85.5元\n- [other/算账口径] 一只85块5，折下来一斤47块5左右；买一只够一桌人吃一顿，鸭子这道菜不到90块。；禁说=不要按1克多少钱算账，比如900克算成1克9分5，这里的900克是反例不是规格；安全改写=按只算，或折到斤就停，也可以用一顿多少钱的生活场景\n- [other/制作与新鲜] 每天新鲜制作，现卤现炸，没有隔夜货、没有存货。\n边界：未提供物流时效和收货温度保证。"
	snapshot, err := s.prepareContentStrategy(ctx, 0, 0, facts, settings.SellingPointProfileID, true)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []map[string]any{}
	cases := []struct{ mode, input string }{
		{"主线", "当前已选卖点：价格；当前角度：按只报价和理解预算。主动塑价，不是假装观众提问。约80至130字，不必每次带饭桌场景。"},
		{"主线", "当前已选卖点：每天新鲜制作；当前角度：对没有隔夜货建立理解与信任。约80至130字，可以自然展开，不把制作日承诺成发货日。"},
		{"互动", "观众原话：85.5贵部贵（理解为贵不贵）。先接住观众真实顾虑并正面回答，再用合适事实解释。约60至100字。当前正在卖杨鸭子。"},
		{"互动", "观众原话：西瓜多少钱？当前正在讲杨鸭子，这个直播间未提供西瓜商品信息。自然简短回应，不能编造西瓜报价，约20至50字。"},
	}
	for _, tc := range cases {
		guidance, err := snapshot.Render(tc.mode, facts)
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "speech_generation", agentgateway.ResponseText, []agentgateway.Message{{Role: "system", Content: guidance}, {Role: "user", Content: tc.input}}, 650, false)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, map[string]any{"path": tc.mode, "input": tc.input, "text": result.Text, "model": result.Model, "elapsed_ms": time.Since(started).Milliseconds()})
		t.Logf("%s: %s", tc.mode, result.Text)
	}
	if path := os.Getenv("CONTENT_STRATEGY_RESULT_PATH"); path != "" {
		raw, _ := json.MarshalIndent(map[string]any{"strategy": snapshot, "outputs": outputs}, "", "  ")
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
