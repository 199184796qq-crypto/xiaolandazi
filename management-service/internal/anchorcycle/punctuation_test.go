package anchorcycle

import (
	"strings"
	"testing"
	"unicode"
)

func TestPunctuationRepairOnlyChangesClearBoundaries(t *testing.T) {
	for _, row := range []struct{ raw, style, want string }{
		{"都买好了不，都拍到了不。", "高亢", "都买好了不？都拍到了不？"},
		{"哥哥姐姐们，1号链接直接拍就行了啊。", "高亢", "哥哥姐姐们，1号链接直接拍就行了啊！"},
		{"我们家39年老油坊啊。", "高亢", "我们家39年老油坊啊。"},
		{"不是陈油，不管多少都按129.9元，5L，9斤2两", "高亢", "不是陈油，不管多少都按129.9元，5L，9斤2两。"},
		{"都买好了不？都拍到了不！", "高亢", "都买好了不？都拍到了不！"},
		{"原话是“都买好了不。”", "高亢", "原话是“都买好了不。”"},
		{"1号链接直接拍就行了啊。", "平静", "1号链接直接拍就行了啊。"},
		{"都选好了不 \n", "高亢", "都选好了不？ \n"},
		{"", "高亢", ""},
	} {
		got, _ := RepairPunctuation(row.raw, row.style, 300)
		if got != row.want {
			t.Fatalf("%q became %q, want %q", row.raw, got, row.want)
		}
		again, edits := RepairPunctuation(got, row.style, 300)
		if again != got || len(edits) != 0 {
			t.Fatal("repair not idempotent")
		}
	}
}

func TestPunctuationRepairRespectsCharacterLimitWithoutCuttingWords(t *testing.T) {
	raw := strings.Repeat("啊", 300)
	got, edits := RepairPunctuation(raw, "高亢", 300)
	if got != raw || len(edits) != 0 {
		t.Fatal("repair exceeded limit or truncated text")
	}
	raw = strings.Repeat("啊", 299)
	got, _ = RepairPunctuation(raw, "高亢", 300)
	if len([]rune(got)) != 300 || got != raw+"。" {
		t.Fatal("available budget not used correctly")
	}
}

func TestPunctuationRepairPreservesEveryNonPunctuationCharacter(t *testing.T) {
	strip := func(text string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsPunct(r) {
				return -1
			}
			return r
		}, text)
	}
	for _, raw := range []string{"都买好了不，都拍到了不。", "我们家129.9元！9斤2两，5L", "老乡，直接拍啊", "原话“都买好了不。”都看明白了没"} {
		got, _ := RepairPunctuation(raw, "高亢", 300)
		if strip(got) != strip(raw) {
			t.Fatal("words/numbers modified")
		}
	}
}
