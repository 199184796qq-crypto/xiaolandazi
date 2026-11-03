package scriptplay

import "testing"

func fixturePool() Pool {
	return Pool{Versions: []Version{
		{ID: "1", PieceID: "p1", Fingerprint: "f1", VoiceID: "A", Model: "qwen", Volume: 70, Kind: KindOriginal, Seq: 1, AudioRef: "a1"},
		{ID: "2", PieceID: "p1", Fingerprint: "f1", VoiceID: "A", Model: "qwen", Volume: 70, Kind: KindRewrite, Seq: 2, AudioRef: "a2"},
		{ID: "3", PieceID: "p1", Fingerprint: "f1", VoiceID: "B", Model: "volc", Volume: 90, Kind: KindOriginal, Seq: 1, AudioRef: "b1"},
	}}
}

func TestPoolVoiceVersionsAndNextKind(t *testing.T) {
	pool := fixturePool()
	if got := pool.VoiceVersions("A"); len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("A 音色版本不对: %+v", got)
	}
	if got := pool.VoiceVersions("B"); len(got) != 1 {
		t.Fatalf("B 音色版本不对: %+v", got)
	}
	if pool.NextKind("A") != KindRewrite {
		t.Fatal("已有原始版本，下一次应是改写")
	}
	if pool.NextKind("C") != KindOriginal {
		t.Fatal("新音色第一次应出原始")
	}
	if pool.NextSeq("A") != 3 {
		t.Fatalf("A 下一个序号应为 3，实际 %d", pool.NextSeq("A"))
	}
	if got := pool.Voices(); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("音色列表不对: %v", got)
	}
}

func TestPoolCapAndAdd(t *testing.T) {
	pool := fixturePool()
	if !pool.CanGenerate("A", 3) {
		t.Fatal("A 现在 2 个版本，上限 3 应还能生成")
	}
	if pool.CanGenerate("A", 2) {
		t.Fatal("A 到上限后不应继续生成")
	}
	if err := pool.Add(Version{ID: "4", PieceID: "p1", Fingerprint: "f1", VoiceID: "A", Kind: KindRewrite, AudioRef: "a3"}); err != nil {
		t.Fatalf("追加失败: %v", err)
	}
	if pool.NextSeq("A") != 4 {
		t.Fatalf("追加后序号应为 4，实际 %d", pool.NextSeq("A"))
	}
	if err := pool.Add(Version{ID: "5"}); err == nil {
		t.Fatal("缺字段应报错")
	}
}

func TestPoolInvalidateByFingerprint(t *testing.T) {
	pool := fixturePool()
	stale := pool.Invalidate("f2")
	if len(stale) != 3 {
		t.Fatalf("旧指纹的版本应全部失效，实际 %d", len(stale))
	}
	if len(pool.Versions) != 0 {
		t.Fatalf("失效后池子应为空，实际 %d", len(pool.Versions))
	}
	// 指纹没变：什么都不动。
	pool2 := fixturePool()
	if stale := pool2.Invalidate("f1"); len(stale) != 0 {
		t.Fatalf("指纹不变时不应失效，实际 %d", len(stale))
	}
}

func TestBeginSwitchNeedsReadyAssets(t *testing.T) {
	if s := BeginSwitch("A", "B", 3, 12); s.State != VoicePreparing {
		t.Fatalf("没备齐应进入准备中: %+v", s)
	}
	if s := BeginSwitch("A", "B", 12, 12); s.State != VoiceActive {
		t.Fatalf("备齐应直接生效: %+v", s)
	}
	if s := BeginSwitch("A", "A", 0, 12); s.State != VoiceActive {
		t.Fatalf("切到自己应保持激活: %+v", s)
	}
}

func TestVoicePlannerKeepsCurrentUntilBlockEnds(t *testing.T) {
	p := VoicePlanner{Current: "A", BlockLeft: 2}
	// 块内两轮都用 A。
	if voice, prep := p.Tick("B", 12, 12, 2); voice != "A" || prep {
		t.Fatalf("块内应继续用 A: %s preparing=%v", voice, prep)
	}
	if voice, _ := p.Tick("B", 12, 12, 2); voice != "A" {
		t.Fatalf("块没走完应继续用 A: %s", voice)
	}
	// 块走完后切到已备齐的 B。
	if voice, _ := p.Tick("B", 12, 12, 2); voice != "B" {
		t.Fatalf("块结束后应切到 B: %s", voice)
	}
}

func TestVoicePlannerWaitsWhenTargetNotReady(t *testing.T) {
	p := VoicePlanner{Current: "A", BlockLeft: 1}
	voice, preparing := p.Tick("B", 3, 12, 2)
	if voice != "A" {
		t.Fatalf("B 没备齐应继续用 A: %s", voice)
	}
	_ = preparing
	// 一直没备齐，就一直用 A。
	for i := 0; i < 5; i++ {
		if v, _ := p.Tick("B", 3, 12, 2); v != "A" {
			t.Fatalf("未备齐期间应一直用 A，实际 %s", v)
		}
	}
	// 备齐后，最迟走完当前这一块就会切到 B。
	switched := false
	for i := 0; i < 3; i++ {
		if v, _ := p.Tick("B", 12, 12, 2); v == "B" {
			switched = true
			break
		}
	}
	if !switched {
		t.Fatal("备齐后应在一个音色块内切到 B")
	}
}
