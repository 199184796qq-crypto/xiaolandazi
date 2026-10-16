package anchorcycle

import "testing"

// ExpandProductSellingPoints 生成的 SlotKey 必须跨场稳定：卖点用 DB 主键 ID、
// 角度用卖点内序号，卖点/角度措辞变化不影响 SlotKey。这是修复「跨场话术库复用
// 因卖点措辞变化导致 Slot 失联、命中率归零」的回归锁。
func TestExpandProductSellingPointsSlotKeyStableAcrossWording(t *testing.T) {
	mk := func(name string) FocusSellingPoint {
		return FocusSellingPoint{
			ID:     7, // 模拟 DB 稳定主键
			Name:   name,
			Weight: 1,
			Angles: []FocusSellingAngle{
				{Angle: "角度甲", Weight: 1},
				{Angle: "角度乙", Weight: 1},
			},
		}
	}
	policy := &ProductPolicy{Version: "v1"}

	// 同一商品链接，卖点措辞从「口感酥脆」改成「外皮酥脆」，ID 不变。
	a := ExpandProductSellingPoints([]FocusProduct{{LinkKey: "1", SellingPoints: []FocusSellingPoint{mk("口感酥脆")}}}, policy)
	b := ExpandProductSellingPoints([]FocusProduct{{LinkKey: "1", SellingPoints: []FocusSellingPoint{mk("外皮酥脆")}}}, policy)

	if len(a) != 2 || len(b) != 2 {
		t.Fatalf("展开任务数异常：a=%d b=%d", len(a), len(b))
	}
	// Key 会随措辞变化（调度键，正常），但 SlotKey 必须稳定（跨场复用键）。
	for i := range a {
		if a[i].SlotKey == "" || b[i].SlotKey == "" {
			t.Fatalf("SlotKey 不应为空")
		}
		if a[i].SlotKey != b[i].SlotKey {
			t.Fatalf("卖点措辞变化不应改变 SlotKey：%q vs %q", a[i].SlotKey, b[i].SlotKey)
		}
		if a[i].Key == b[i].Key {
			t.Fatalf("Key 应随措辞变化（用于区分本场调度）：%q", a[i].Key)
		}
	}
	// SlotKey 应含稳定卖点主键而非措辞。
	if a[0].SlotKey != "1×#7×#1" || a[1].SlotKey != "1×#7×#2" {
		t.Fatalf("SlotKey 格式不符：%q %q", a[0].SlotKey, a[1].SlotKey)
	}
}

// 卖点无 ID（旧数据/无主键）时，SlotKey 退回卖点名，保证兼容不崩溃。
func TestExpandProductSellingPointsSlotKeyFallbackWithoutID(t *testing.T) {
	policy := &ProductPolicy{Version: "v1"}
	sp := FocusSellingPoint{Name: "口感", Weight: 1, Angles: []FocusSellingAngle{{Angle: "肉紧", Weight: 1}}}
	tasks := ExpandProductSellingPoints([]FocusProduct{{LinkKey: "1", SellingPoints: []FocusSellingPoint{sp}}}, policy)
	if len(tasks) != 1 {
		t.Fatalf("展开任务数异常：%d", len(tasks))
	}
	if tasks[0].SlotKey != "1×口感×#1" {
		t.Fatalf("无 ID 时应退回卖点名：%q", tasks[0].SlotKey)
	}
}
