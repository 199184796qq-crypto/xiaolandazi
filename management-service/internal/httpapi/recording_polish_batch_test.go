package httpapi

import "testing"

func TestRecordingPolishMarkerParsing(t *testing.T) {
	rewritten := "好的，稍等。\n<<1>>第一句改写。\n<<2>>第二句改写。\n<<3>>第三句改写。"
	matches := recordingPolishMarker.FindAllStringSubmatchIndex(rewritten, -1)
	if len(matches) != 3 {
		t.Fatalf("expect 3 markers, got %d", len(matches))
	}
	pieces := make([]string, 3)
	for i, match := range matches {
		number := int(rewritten[match[2]] - '0')
		end := len(rewritten)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		text := rewritten[match[1]:end]
		pieces[number-1] = text
	}
	if pieces[0] == "" || pieces[1] == "" || pieces[2] == "" {
		t.Fatalf("all pieces should be filled: %#v", pieces)
	}
}

func TestSameNumbers(t *testing.T) {
	cases := []struct {
		before, after string
		want          bool
	}{
		{"原价 245，现在 187。", "原价 245，现在 187。", true},
		{"原价 245，现在 187。", "原价 245，现在 189。", false},
		{"有 6 人餐和 30 元券。", "6 人餐，30 元券。", true},
		{"全场 5 折。", "全场五折。", false},
		{"没有数字的句子。", "改写后也没有数字。", true},
	}
	for _, item := range cases {
		if got := sameNumbers(item.before, item.after); got != item.want {
			t.Fatalf("sameNumbers(%q, %q) = %v, want %v", item.before, item.after, got, item.want)
		}
	}
}

func TestNumbersKeyIgnoresPunctuation(t *testing.T) {
	if numbersKey("245元、187元") != numbersKey("245 187") {
		t.Fatal("punctuation must not change the number set")
	}
}
