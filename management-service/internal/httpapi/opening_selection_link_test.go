package httpapi

import (
	"testing"

	"livecompanion/management/internal/anchorcycle"
)

// 选材候选必须自带商品归属：队列任务此时还没 focusTask，AllowedLinksJSON 为空，
// 只读 ProductLinks() 会让所有候选的链接都是空，写话术时就按错商品给事实。
func TestOpeningSelectionPoolCarriesProductLink(t *testing.T) {
	state := &anchorcycle.State{Queue: []anchorcycle.Task{
		{LinkKey: "1号链接", SellingPoint: "价格与套餐内容", Angle: "核对1号链接团购价337元与原价547元的差额", Key: "1号链接×价格与套餐内容×核对差额"},
		{LinkKey: "2号链接", SellingPoint: "6人餐价格与价值", Angle: "直接事实：当前2号链接团购价为187元", Key: "2号链接×6人餐价格与价值×187"},
		{Common: true, SellingPoint: "地理位置与停车", Angle: "说明好停车", Key: "common:park"},
	}}
	pool := openingSelectionPool(state)
	price := pool["价格与套餐内容"]
	if len(price) != 1 || price[0].LinkKey != "1号链接" {
		t.Fatalf("product angle must carry its link: %#v", price)
	}
	value := pool["6人餐价格与价值"]
	if len(value) != 1 || value[0].LinkKey != "2号链接" {
		t.Fatalf("second product angle must carry its link: %#v", value)
	}
	park := pool["地理位置与停车"]
	if len(park) != 1 || park[0].LinkKey != "" || !park[0].Common {
		t.Fatalf("common angle must stay link-free: %#v", park)
	}
}
