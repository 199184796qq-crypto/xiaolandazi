package httpapi

import "testing"

func TestRotateFirstAwayFromPrevious(t *testing.T) {
	// 撞上时应该把别的片段挪到第一位
	if got := rotateFirstAwayFrom([]int{2, 0, 1, 3}, 2); got[0] == 2 {
		t.Fatalf("第一条仍等于上轮最后一条: %v", got)
	}
	// 不撞就原样返回
	in := []int{0, 1, 2}
	if got := rotateFirstAwayFrom(in, 2); got[0] != 0 {
		t.Fatalf("不该改动: %v", got)
	}
	// 只有一条片段时不崩
	if got := rotateFirstAwayFrom([]int{0}, 0); len(got) != 1 {
		t.Fatalf("单片段异常: %v", got)
	}
}