package decisionexecutor

import "testing"

func TestPickBridgePhraseAvoidsRecent(t *testing.T) {
	// 近期用过的句不该被重复选中。
	p1 := pickBridgePhrase(nil)
	if p1 == "" {
		t.Fatal("empty phrase from pool")
	}
	p2 := pickBridgePhrase([]string{p1})
	if p2 == p1 {
		t.Fatalf("recent phrase should be avoided, got %q twice", p1)
	}
	p3 := pickBridgePhrase([]string{p1, p2})
	if p3 == p1 || p3 == p2 {
		t.Fatalf("two recent phrases should be avoided, got %q", p3)
	}
}

func TestPickBridgePhraseExhaustsPool(t *testing.T) {
	// 所有池子句都用过时，按轮次取模，不 panic、返回非空。
	all := bridgePhrasePool
	got := pickBridgePhrase(all)
	if got == "" {
		t.Fatal("expected fallback phrase when pool exhausted")
	}
}

func TestBridgePhrasePoolIsPureTransition(t *testing.T) {
	// 池子里的句必须是纯承接，不能含话题词（这些词会预告主线内容）。
	forbidden := []string{"口感", "工艺", "价格", "规格", "材质", "产地", "口味", "包装", "发货", "售后", "链接", "福利"}
	for _, p := range bridgePhrasePool {
		for _, w := range forbidden {
			if containsRune(p, w) {
				t.Fatalf("phrase %q contains forbidden topic word %q", p, w)
			}
		}
	}
}

func containsRune(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len([]rune(sub)) <= len([]rune(s)); i++ {
		if string([]rune(s)[i:i+len([]rune(sub))]) == sub {
			return i
		}
	}
	return -1
}
