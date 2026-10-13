package httpapi

import (
	"testing"

	"livecompanion/management/internal/contentpools"
)

func TestPoolAngleOverlapRate(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool // want true 表示判定为高度重叠（应去重）
	}{
		{"几乎一样应去重", "不用你费劲去蒸去煮也不用二次加工撕开包装就能直接吃", "不用你再去蒸去煮也不用二次加工撕开包装就能直接吃", true},
		{"完全一样应去重", "老卤汤慢炖4小时两遍油炸裹秘制红油锁汁定型", "老卤汤慢炖4小时两遍油炸裹秘制红油锁汁定型", true},
		{"合理不同角度保留", "打开袋子就能直接吃", "当零食直接吃也可以搭配主食当下酒菜", false},
		{"不同主题保留", "老卤汤慢炖4小时两遍油炸裹秘制红油锁汁", "抽真空常温保存6天不抽真空3天", false},
		{"空文本保留", "", "打开袋子就能直接吃", false},
	}
	for _, c := range cases {
		rate := poolAngleOverlapRate(c.a, c.b)
		got := rate >= 0.55
		if got != c.want {
			t.Errorf("%s: overlapRate=%.2f, 判定重复=%v, 期望=%v", c.name, rate, got, c.want)
		}
	}
}

func TestPoolAngleDuplicated(t *testing.T) {
	existing := []contentpools.Task{
		{Angle: "不用你费劲去蒸去煮也不用二次加工撕开包装就能直接吃"},
	}
	// 高度重叠的候选应命中
	if got := poolAngleDuplicated(existing, "不用你再去蒸去煮也不用二次加工撕开包装就能直接吃"); got == "" {
		t.Error("高度重叠的角度应被判为重复，实际未命中")
	}
	// 合理不同角度不应命中
	if got := poolAngleDuplicated(existing, "抽真空常温保存6天不抽真空3天"); got != "" {
		t.Errorf("不同主题角度不应判为重复，实际命中：%s", got)
	}
	// 空候选不应命中
	if got := poolAngleDuplicated(existing, ""); got != "" {
		t.Error("空角度不应判为重复")
	}
	// 空池不应命中
	if got := poolAngleDuplicated(nil, "打开袋子就能直接吃"); got != "" {
		t.Error("空池不应判为重复")
	}
}
