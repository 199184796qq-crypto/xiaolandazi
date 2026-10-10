package contentstrategy

import "testing"

func TestConsecutiveSpeechOnlyDeletesLiteralAdjacentText(t *testing.T) {
	for _, c := range []struct {
		text, want string
		allow      bool
		removed    int
	}{
		{"怕买整只吃不完浪费，这个半只装刚好。怕买整只吃不完浪费，这个半只装刚好。", "怕买整只吃不完浪费，这个半只装刚好。", false, 1},
		{"一只85.5元。\n接着说规格。\n一只85.5元。\n接着说规格。", "一只85.5元。\n接着说规格。\n一只85.5元。\n接着说规格。", false, 0},
		{"一只85.5元。接着说规格。\n一只85.5元。接着说规格。", "一只85.5元。接着说规格。", false, 1},
		{"一只85.5元。 一只85.5元。 接着说规格。", "一只85.5元。 接着说规格。", false, 1},
		{"那没听过的牌子，到底靠不靠谱？没听过的牌子，到底靠不靠谱？", "那没听过的牌子，到底靠不靠谱？没听过的牌子，到底靠不靠谱？", false, 0},
		{"一只85.5元。一只95.5元。不到90元。", "一只85.5元。一只95.5元。不到90元。", false, 0},
		{"欢迎回来。欢迎回来！", "欢迎回来。欢迎回来！", false, 0},
		{"欢迎回来。接着说规格。欢迎回来。", "欢迎回来。接着说规格。欢迎回来。", false, 0},
		{"欢迎回来。欢迎回来。", "欢迎回来。欢迎回来。", true, 0},
	} {
		got, count := RemoveConsecutiveSpeech(c.text, c.allow)
		if got != c.want || count != c.removed {
			t.Fatalf("input=%q got=%q count=%d", c.text, got, count)
		}
	}
}

func TestBridgeDuplicatesOnlyImmediateSentence(t *testing.T) {
	for _, c := range []struct {
		bridge, reply, entry string
		allow, want          bool
	}{
		{"怕买整只吃不完浪费，这个半只装刚好。", "已回答发货安排。", "怕买整只吃不完浪费，这个半只装刚好。半只450g。", false, true},
		{"感谢老乡的支持。", "答案已说明。感谢老乡的支持。", "回到规格选择。", false, true},
		{"感谢老乡的支持。", "感谢老乡的支持。答案已说明。", "回到规格选择。", false, false},
		{"那没听过的牌子，到底靠不靠谱？", "谢谢支持。", "没听过的牌子，到底靠不靠谱？会不会是临时店？", false, false},
		{"不到90块。", "一只85块5。", "不到90块。", true, false},
	} {
		if got := BridgeDuplicatesAdjacent(c.bridge, c.reply, c.entry, c.allow); got != c.want {
			t.Fatalf("%+v got=%v", c, got)
		}
	}
}
