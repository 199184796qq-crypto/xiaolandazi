package contentblock

import (
	"strings"
	"testing"
)

// 屏蔽内容按「规范化后整句一致」比对：空格差异、大小写差异不产生第二条记录，
// 但内容不同就不能误命中。
func TestContentKeyNormalizesWhitespaceAndCase(t *testing.T) {
	cases := []string{
		"  求关注  ",
		"求关注",
		"求  关注",
		"求\t关注",
		"求\n关注",
		"求 关 注",
		"求　关注", // 全角空格
	}
	first := ContentKey(cases[0])
	if first == "" {
		t.Fatal("non-empty content must produce a key")
	}
	for _, value := range cases[1:] {
		if got := ContentKey(value); got != first {
			t.Fatalf("%q should share the key of %q, got %q vs %q", value, cases[0], got, first)
		}
	}

	english := ContentKey("Promo CODE")
	if english != ContentKey(" promo code ") {
		t.Fatal("english content must match case-insensitively")
	}
	if english == first {
		t.Fatal("different content must not collide")
	}
}

func TestContentKeyRejectsEmptyContent(t *testing.T) {
	for _, value := range []string{"", "   ", "\n\t "} {
		if key := ContentKey(value); key != "" {
			t.Fatalf("blank content %q must not be blockable, got %q", value, key)
		}
	}
}

func TestContentKeyIsStable(t *testing.T) {
	content := "1号链接今天还有7单"
	if ContentKey(content) != ContentKey(content) {
		t.Fatal("ContentKey must be deterministic")
	}
	if Normalize(content) != content {
		t.Fatalf("normalized chinese content must stay unchanged, got %q", Normalize(content))
	}
}

func TestLongContentKeepsStableKey(t *testing.T) {
	long := strings.Repeat("刷屏内容", 400)
	if ContentKey(long) == "" {
		t.Fatal("long content must still be blockable")
	}
	if ContentKey(long) != ContentKey(" "+long+"\n") {
		t.Fatal("surrounding whitespace must not change the key of long content")
	}
}
