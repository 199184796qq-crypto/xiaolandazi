package scriptplay

import "testing"

func TestOriginalGeneratorReturnsOriginal(t *testing.T) {
	got, err := OriginalGenerator{}.Generate("这只卤鹅四十五块五")
	if err != nil || got.Text != "这只卤鹅四十五块五" || got.Kind != KindOriginal {
		t.Fatalf("应原样返回: %+v %v", got, err)
	}
}

func TestPlanGenerationOriginalFirstThenRewrite(t *testing.T) {
	var pool Pool
	req, ok := PlanGeneration(pool, "A", 6)
	if !ok || req.Kind != KindOriginal || req.Seq != 1 {
		t.Fatalf("空池第一次应出原始: %+v ok=%v", req, ok)
	}
	if err := pool.Add(NewVersion("p", "f", "A", "qwen", 70, req, "a1")); err != nil {
		t.Fatal(err)
	}
	req, ok = PlanGeneration(pool, "A", 6)
	if !ok || req.Kind != KindRewrite || req.Seq != 2 {
		t.Fatalf("已有原始后应出改写: %+v ok=%v", req, ok)
	}
}

func TestPlanGenerationStopsAtCap(t *testing.T) {
	pool := Pool{Versions: []Version{
		{VoiceID: "A", Kind: KindOriginal, Seq: 1, Fingerprint: "f", AudioRef: "x"},
		{VoiceID: "A", Kind: KindRewrite, Seq: 2, Fingerprint: "f", AudioRef: "y"},
	}}
	if _, ok := PlanGeneration(pool, "A", 2); ok {
		t.Fatal("到上限后不应再规划生成")
	}
	if _, ok := PlanGeneration(pool, "B", 2); !ok {
		t.Fatal("另一个音色的配额应独立计算")
	}
}

func TestPlanFallbackGeneration(t *testing.T) {
	// 没原始版本 + 生成失败 → 兜底出改写。
	var empty Pool
	fb := PlanFallbackGeneration(empty, "A", 6)
	if fb == nil || fb.Kind != KindRewrite {
		t.Fatalf("原始失败应兜底改写: %+v", fb)
	}
	// 已有原始 → 本来就出改写，无需兜底。
	pool := Pool{Versions: []Version{{VoiceID: "A", Kind: KindOriginal, Seq: 1, Fingerprint: "f", AudioRef: "x"}}}
	if fb := PlanFallbackGeneration(pool, "A", 6); fb != nil {
		t.Fatalf("已有原始不该产生兜底请求: %+v", fb)
	}
}

func TestVoiceReadyCountsPieces(t *testing.T) {
	pools := map[string]Pool{
		"p1": {Versions: []Version{{VoiceID: "A", Kind: KindOriginal, Seq: 1, Fingerprint: "f", AudioRef: "x"}}},
		"p2": {},
		"p3": {Versions: []Version{{VoiceID: "A", Kind: KindOriginal, Seq: 1, Fingerprint: "f", AudioRef: "y"}}},
	}
	ready, total := VoiceReady(pools, "A")
	if ready != 2 || total != 3 {
		t.Fatalf("应 2/3 就绪，实际 %d/%d", ready, total)
	}
	if ready, total := VoiceReady(pools, "B"); ready != 0 || total != 3 {
		t.Fatalf("B 音色应为 0/3，实际 %d/%d", ready, total)
	}
}

func TestNewVersionFillsFields(t *testing.T) {
	req := GenerationRequest{Kind: KindRewrite, Seq: 3}
	v := NewVersion("p", "f", "A", "volc", 90, req, "a3")
	if v.PieceID != "p" || v.Kind != KindRewrite || v.Seq != 3 || v.Volume != 90 {
		t.Fatalf("字段不对: %+v", v)
	}
}
