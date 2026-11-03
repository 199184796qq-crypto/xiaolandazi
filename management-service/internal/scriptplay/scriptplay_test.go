package scriptplay

import (
	"math/rand"
	"testing"
)

func testPlan() []Tag {
	return []Tag{
		{ID: "intro", Title: "开场", Fixed: true, Pieces: []Piece{{ID: "a", Text: "开场一"}, {ID: "b", Text: "开场二"}}},
		{ID: "product", Title: "讲解", Pieces: []Piece{{ID: "c", Text: "讲解一"}, {ID: "d", Text: "讲解二"}, {ID: "e", Text: "讲解三"}}},
		{ID: "after", Title: "售后", Fixed: true, Pieces: []Piece{{ID: "f", Text: "售后一"}}},
		{ID: "welfare", Title: "福利", Pieces: []Piece{{ID: "g", Text: "福利一"}}},
	}
}

func TestFingerprintIgnoresWhitespaceAndWidth(t *testing.T) {
	same := []string{
		"这只鸭子 四十五块五",
		"这只鸭子四十五块五",
		"　这只鸭子　四十五块五　",
	}
	want := Fingerprint(same[0])
	for _, text := range same[1:] {
		if got := Fingerprint(text); got != want {
			t.Fatalf("%q 指纹 %s 与 %s 不一致", text, got, want)
		}
	}
	if Fingerprint("这只鸭子四十六块五") == want {
		t.Fatal("内容变了指纹必须变")
	}
	if Fingerprint("ＡＢＣ") != Fingerprint("ABC") {
		t.Fatal("全角半角应视为相同")
	}
}

func TestOrderTagsKeepsFixedPositions(t *testing.T) {
	tags := testPlan()
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 50; i++ {
		order := OrderTags(tags, nil, rng)
		if order[0] != 0 || order[2] != 2 {
			t.Fatalf("固定标签位置被改动: %v", order)
		}
		seen := map[int]bool{}
		for _, idx := range order {
			if seen[idx] {
				t.Fatalf("顺序里出现重复标签: %v", order)
			}
			seen[idx] = true
		}
		if len(order) != len(tags) {
			t.Fatalf("顺序长度不对: %v", order)
		}
	}
}

func TestOrderTagsAllFixedKeepsOriginal(t *testing.T) {
	tags := []Tag{{ID: "a", Fixed: true}, {ID: "b", Fixed: true}, {ID: "c", Fixed: true}}
	order := OrderTags(tags, nil, rand.New(rand.NewSource(3)))
	for i, idx := range order {
		if idx != i {
			t.Fatalf("全固定必须按配置顺序: %v", order)
		}
	}
}

func TestOrderTagsAvoidsSameAsPrevious(t *testing.T) {
	tags := []Tag{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	rng := rand.New(rand.NewSource(11))
	previous := []int{0, 1, 2, 3}
	order := OrderTags(tags, previous, rng)
	if sameOrder(order, previous) {
		t.Fatalf("不应与上一轮完全相同: %v", order)
	}
}

func TestPickPiecesFullReturnsAll(t *testing.T) {
	tags := testPlan()
	got := PickPieces(tags[1], ModeFull, NewTagState(), rand.New(rand.NewSource(5)))
	if len(got) != 3 {
		t.Fatalf("全读应返回全部片段，实际 %v", got)
	}
	seen := map[int]bool{}
	for _, idx := range got {
		if seen[idx] {
			t.Fatalf("全读重复片段: %v", got)
		}
		seen[idx] = true
	}
}

func TestPickPiecesSingleAvoidsRepeatUntilExhausted(t *testing.T) {
	tags := testPlan()
	tag := tags[1] // 3 个片段
	state := NewTagState()
	rng := rand.New(rand.NewSource(9))
	seen := map[string]int{}
	for i := 0; i < 3; i++ {
		picked := PickPieces(tag, ModeSingle, state, rng)
		if len(picked) != 1 {
			t.Fatalf("单选应只返回一个，实际 %v", picked)
		}
		id := tag.Pieces[picked[0]].ID
		if seen[id] > 0 {
			t.Fatalf("前 %d 轮内重复了片段 %s", 3, id)
		}
		seen[id]++
	}
	// 第四轮：全部用过，允许重新开始。
	picked := PickPieces(tag, ModeSingle, state, rng)
	if len(picked) != 1 {
		t.Fatalf("用尽后仍应返回一个，实际 %v", picked)
	}
}

func TestPickPiecesSingleTagWithOnePiece(t *testing.T) {
	tag := Tag{ID: "solo", Pieces: []Piece{{ID: "only", Text: "唯一"}}}
	state := NewTagState()
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 5; i++ {
		got := PickPieces(tag, ModeSingle, state, rng)
		if len(got) != 1 || got[0] != 0 {
			t.Fatalf("单片段标签应每轮都返回它，实际 %v", got)
		}
	}
}

func TestPickPiecesEmptyTag(t *testing.T) {
	tag := Tag{ID: "empty"}
	if got := PickPieces(tag, ModeFull, NewTagState(), nil); got != nil {
		t.Fatalf("空标签应返回 nil，实际 %v", got)
	}
}

func TestVoiceBlockLength(t *testing.T) {
	cases := []struct{ versions, min, want int }{
		{1, 4, 4},  // 只有原始版本：守住最少 4 轮
		{4, 4, 4},
		{6, 4, 6},
		{0, 4, 4},
		{3, 0, 3}, // 未配置下限时按 1 处理，取版本数
	}
	for _, c := range cases {
		got := VoiceBlockLength(c.versions, c.min)
		want := c.want
		if c.min == 0 && c.versions < 1 {
			want = 1
		}
		if got != want {
			t.Fatalf("versions=%d min=%d got=%d want=%d", c.versions, c.min, got, want)
		}
	}
}

func TestPickAndShuffleVersions(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	remaining := ShuffleVersions([]int{0, 1, 2, 3, 4, 5}, rng)
	if len(remaining) != 6 {
		t.Fatalf("洗牌后数量变化: %v", remaining)
	}
	used := map[int]bool{}
	for len(remaining) > 0 {
		v := PickVersion(remaining, rng)
		if used[v] {
			t.Fatalf("版本 %d 被重复选中", v)
		}
		used[v] = true
		for i, item := range remaining {
			if item == v {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	if PickVersion(nil, rng) != -1 {
		t.Fatal("池子空了应返回 -1")
	}
}

func TestVersionCapAndGenerate(t *testing.T) {
	if VersionCap(0) != 6 || VersionCap(-1) != 6 {
		t.Fatal("未配置应默认 6")
	}
	if VersionCap(99) != 50 {
		t.Fatal("上限应被钳到 50")
	}
	if !CanGenerate(5, 6) || CanGenerate(6, 6) {
		t.Fatal("到上限后不应继续生成")
	}
}
