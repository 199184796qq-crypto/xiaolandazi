package scriptchain

import (
	"encoding/json"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

func TestParseAnalysisAcceptsWrappedJSON(t *testing.T) {
	raw := "```json\n{\"开场方式\":\"叫停式\",\"逻辑链\":[{\"名称\":\"A\",\"段序\":[\"停场\",\"报价\",\"催单\"],\"理由\":\"r\"},{\"名称\":\"B\",\"段序\":[\"痛点\",\"场景\",\"催单\"],\"理由\":\"r\"}],\"收口逻辑\":\"c\",\"语言风格\":\"s\"}\n```"
	a, err := ParseAnalysis(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(a.Chains) != 2 || a.Chains[0].Nodes[0] != "停场" {
		t.Fatalf("unexpected analysis: %+v", a)
	}
}

func TestParseAnalysisRejectsShortChain(t *testing.T) {
	raw := `{"开场方式":"x","逻辑链":[{"名称":"A","段序":["停场"],"理由":"r"}],"收口逻辑":"c","语言风格":"s"}`
	if _, err := ParseAnalysis(raw); err == nil {
		t.Fatal("expected error for short chain")
	}
}

func TestParseAnalysisCombinesShortRoutesFromSameReply(t *testing.T) {
	raw := `{"逻辑链":[{"名称":"悬念自答","段序":["抛出疑问","自我作答"]},{"名称":"实力背书","段序":["规模铺陈","同价承诺"]},{"名称":"现场实证","段序":["当面演示","感官还原"]},{"名称":"工艺拆解","段序":["关键数字","成本归位"]},{"名称":"新鲜承诺","段序":["现做现发","零库存说明"]},{"名称":"场景代入","段序":["生活场景","对号入座"]},{"名称":"价格锚定","段序":["门店价","直播价"]},{"名称":"规格分层","段序":["按需分号","选择引导"]},{"名称":"送礼情感","段序":["送礼场景","异地寄情"]},{"名称":"服务兜底","段序":["增值服务","售后保障","复述催付"]}]}`
	got, err := ParseAnalysis(raw)
	if err != nil || len(got.Chains) != 10 {
		t.Fatalf("got %d routes, err=%v", len(got.Chains), err)
	}
	var original Analysis
	if err := json.Unmarshal([]byte(raw), &original); err != nil {
		t.Fatal(err)
	}
	for i, c := range got.Chains {
		if len(c.Nodes) != 3 || c.Name != original.Chains[i].Name || !slices.Equal(c.Nodes[:len(original.Chains[i].Nodes)], original.Chains[i].Nodes) {
			t.Errorf("route %d lost its order or has wrong length: %+v", i, c)
		}
	}
	if want := []string{"抛出疑问", "自我作答", "规模铺陈"}; !slices.Equal(got.Chains[0].Nodes, want) {
		t.Fatalf("first route = %v, want %v", got.Chains[0].Nodes, want)
	}
}

func TestLoadAnalysisCompletesCachedRoutesWithoutDuplicatingNodes(t *testing.T) {
	store := NewStore(NewMemoryKV(), 0)
	piece := Piece{Tenant: 1, Session: 2, Plan: 3, ID: 4, ContentHash: ContentHash("原始话术")}
	stored := Analysis{Chains: []Chain{
		{Name: "空路线", Nodes: []string{" ", ""}},
		{Name: "A", Nodes: []string{" 甲 ", "甲"}},
		{Name: "B", Nodes: []string{"甲", "乙"}},
		{Name: "C", Nodes: []string{"丙"}},
	}}
	if err := store.Save(t.Context(), piece, stored); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Load(t.Context(), piece)
	if err != nil || !ok || len(got.Chains) != 3 {
		t.Fatalf("load got %+v, ok=%t, err=%v", got, ok, err)
	}
	if c := got.Chains[0]; c.Name != "A" || !slices.Equal(c.Nodes, []string{"甲", "乙", "丙"}) {
		t.Fatalf("unexpected first useful route: %+v", c)
	}
}

func TestPickAvoidsRecent(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		idx := Pick(10, []int{0, 1, 2}, r)
		if idx < 3 {
			t.Fatalf("picked recently used chain %d", idx)
		}
	}
	if got := Pick(3, []int{0, 1, 2}, r); got < 0 || got > 2 {
		t.Fatalf("expected fallback pick, got %d", got)
	}
}

func TestTargetLengthWithinRange(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 200; i++ {
		if v := TargetLength(r); v < 400 || v > 1000 {
			t.Fatalf("target out of range: %d", v)
		}
	}
}

func TestSessionChainCacheIsolatesContentAndClearsOnlyEndedSession(t *testing.T) {
	cache := NewStore(NewMemoryKV(), 0)
	piece := Piece{Tenant: 14, Session: 21, Plan: 3, ID: 9, ContentHash: ContentHash("方案原文")}
	next := piece
	next.Session = 22
	changed := piece
	changed.ContentHash = ContentHash("改过的方案原文")
	analysis := Analysis{Chains: []Chain{{Name: "路线", Nodes: []string{"甲", "乙", "丙"}}}}
	for _, key := range []Piece{piece, next} {
		if err := cache.Save(t.Context(), key, analysis); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			cache.MarkUsed(t.Context(), key, cache.RecentUsed(t.Context(), key), i)
		}
	}
	if _, hit, _ := cache.Load(t.Context(), changed); hit {
		t.Fatal("changed original reused an old analysis")
	}
	if err := cache.ClearSession(t.Context(), piece.Tenant, piece.Session); err != nil {
		t.Fatal(err)
	}
	if _, hit, _ := cache.Load(t.Context(), piece); hit || len(cache.RecentUsed(t.Context(), piece)) > 0 {
		t.Fatal("ended session still contains chain data")
	}
	if _, hit, _ := cache.Load(t.Context(), next); !hit || len(cache.RecentUsed(t.Context(), next)) != 3 {
		t.Fatal("next session was cleared or recent chain history is wrong")
	}
}

func TestNormalizeScriptSingleParagraph(t *testing.T) {
	in := "宝宝们 先停一下。\r\n\r\n  今天只讲一只鸭子 ，\n900 g 一整只。\u3000好。"
	got := NormalizeScript(in)
	want := "宝宝们先停一下。今天只讲一只鸭子，900 g一整只。好。"
	if got != want {
		t.Fatalf("normalize mismatch\n got=%q\nwant=%q", got, want)
	}
	if strings.ContainsAny(got, "\r\n") {
		t.Fatal("newlines left")
	}
}

func TestRenderRequirementCarriesChainAndTarget(t *testing.T) {
	a := Analysis{Opening: "叫停式", Closing: "先兜底再催单", Style: "短句"}
	c := Chain{Name: "停场亮品", Nodes: []string{"停场", "亮品", "催单"}}
	got := RenderRequirement(a, c, 700)
	for _, want := range []string{"叫停式", "停场 → 亮品 → 催单", "目标字数：700"} {
		if !strings.Contains(got, want) {
			t.Fatalf("requirement missing %q: %s", want, got)
		}
	}
}
