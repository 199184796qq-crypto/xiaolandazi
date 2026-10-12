package httpapi

import "testing"

func TestNormalizeSellingPointWheelAngleAlias(t *testing.T) {
	// 模型偶尔把「角度」写成英文 "angle"，程序应归一为「角度」不判失败。
	mixed := `{"卖点列表":[{"卖点名":"品牌底蕴","角度":["官方直播间"]},{"卖点名":"核心工艺","angle":["老卤慢炖4小时"]},{"卖点名":"原料","目标人群":"关注食材的消费者","角度":["八个月土麻鸭"]}]}`
	canonical, points, angles, err := normalizeAnchorStyleSellingPointWheelJSON(mixed)
	if err != nil {
		t.Fatalf("中英文混用字段名不应失败: %v", err)
	}
	if points != 3 || angles != 3 {
		t.Errorf("期望 3 卖点 3 角度，实际 points=%d angles=%d", points, angles)
	}
	if canonical == "" {
		t.Error("归一后 canonical 不应为空")
	}
}
