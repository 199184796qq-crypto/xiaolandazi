package audioout

import "testing"

func TestReturnCoverageIsExactAndAdjacent(t *testing.T) {
	for _, c := range []struct {
		answer, unit string
		want         bool
	}{
		{"一只85.5元。", "一只85.5元。", true},
		{"一只85.5元。接着说规格。", "一只85.5元。", false},
		{"接着说规格。一只85.5元。", "一只85.5元。", true},
		{"一只85.5元。", "一只85.5元！", false},
		{"一只85.5元。", "一只855元。", false},
		{"欢迎老乡。", "欢迎朋友。", false},
	} {
		if got := CompleteUnitCovered(c.answer, c.unit); got != c.want {
			t.Fatalf("%+v got=%v", c, got)
		}
	}
	plan := InteractionReturnSnapshot{ReplyText: "已回答发货安排。", Units: []ReturnUnit{{Text: "怕买整只吃不完浪费，这个半只装刚好。半只450g。"}}}
	if !plan.DuplicatesBridge("怕买整只吃不完浪费，这个半只装刚好。") {
		t.Fatal("actual live duplicate not removed")
	}
	plan.AllowConsecutiveRepeat = true
	if plan.DuplicatesBridge("怕买整只吃不完浪费，这个半只装刚好。") {
		t.Fatal("explicit repeat removed")
	}
}
