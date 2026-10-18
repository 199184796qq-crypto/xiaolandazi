package anchorcycle

import "testing"

// 专属事实=0（只有公共事实+身份占位）的角度，materialAvailable 应返回 false（料不足，不单列）。
func TestMaterialAvailableZeroDedicated(t *testing.T) {
	scope := &TaskFactScope{
		Facts: []HookFact{
			{ID: "identity", Data: "商品名称=10人餐"},
			{ID: "common-addr", Data: "- [用户指定公共；location/地址] 顺庆区滨江中路"},
			{ID: "common-park", Data: "- [用户指定公共；location/停车] 免费停车"},
		},
	}
	task := Task{Key: "K", SellingPoint: "卖点", Angle: "角度", FactScope: scope}
	if materialAvailable(task) {
		t.Fatalf("只有公共事实+身份占位的角度应判定料不足，got 可用")
	}
	if got := taskDedicatedFactCount(task); got != 0 {
		t.Fatalf("专属事实数应为 0，got %d", got)
	}
}

// 有商品专属事实的角度，materialAvailable 应返回 true（料足）。
func TestMaterialAvailableWithDedicated(t *testing.T) {
	scope := &TaskFactScope{
		Facts: []HookFact{
			{ID: "identity", Data: "商品名称=10人餐"},
			{ID: "package", Data: `- [用户指定商品="1号链接"；package/包含特色菜品] 产品包含大刀腰片和脑花。`},
			{ID: "common-addr", Data: "- [用户指定公共；location/地址] 顺庆区滨江中路"},
		},
	}
	task := Task{Key: "K", SellingPoint: "卖点", Angle: "角度", FactScope: scope}
	if !materialAvailable(task) {
		t.Fatalf("有商品专属事实的角度应判定料足")
	}
	if got := taskDedicatedFactCount(task); got != 1 {
		t.Fatalf("专属事实数应为 1，got %d", got)
	}
}

// 公共段任务（Common）不受专属事实约束，恒可用。
func TestMaterialAvailableCommonAlways(t *testing.T) {
	scope := &TaskFactScope{
		Facts: []HookFact{
			{ID: "identity", Data: "商品名称=10人餐"},
			{ID: "common-addr", Data: "- [用户指定公共；location/地址] 顺庆区滨江中路"},
		},
	}
	task := Task{Key: "K", Common: true, ProductMode: "common", FactScope: scope}
	if !materialAvailable(task) {
		t.Fatalf("公共段任务应恒可用")
	}
}

// 无 FactScope 的旧任务（legacy）走 ProductFocus/s.Facts 兜底路径，不误杀。
func TestMaterialAvailableLegacy(t *testing.T) {
	// lecture 豁免
	if !materialAvailable(Task{Key: "K", ProductMode: "lecture"}) {
		t.Fatalf("lecture 任务应豁免")
	}
	// 无 FactScope 的普通商品任务：走 legacy 兜底路径，不判料不足
	if !materialAvailable(Task{Key: "K", ProductMode: "single"}) {
		t.Fatalf("无 FactScope 的 legacy 任务应放行（走 ProductFocus/s.Facts 兜底）")
	}
	// taskDedicatedFactCount 无 FactScope 返回 -1（表示无绑定，不是 0）
	if got := taskDedicatedFactCount(Task{Key: "K"}); got != -1 {
		t.Fatalf("无 FactScope 应返回 -1，got %d", got)
	}
}
