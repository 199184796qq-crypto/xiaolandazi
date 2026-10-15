package httpapi

import (
	"encoding/json"
	"testing"
)

func TestAssignSellingPointTiers(t *testing.T) {
	raw := `{"卖点列表":[
		{"卖点名":"物流发货与时效","角度":["京东物流"]},
		{"卖点名":"独家老卤工艺","角度":["老卤慢炖两遍油炸"]},
		{"卖点名":"口感风味","角度":["肉紧有嚼头"]},
		{"卖点名":"价格算账","角度":["日常价87.6"]}
	]}`
	out, err := assignSellingPointTiers(raw)
	if err != nil {
		t.Fatal(err)
	}
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(out), &wheel); err != nil {
		t.Fatal(err)
	}
	tier := map[string]string{}
	for _, item := range wheel.Items {
		tier[item.Name] = item.Tier
	}
	if tier["物流发货与时效"] != "trust" {
		t.Fatalf("物流类应判 trust，got %q", tier["物流发货与时效"])
	}
	if tier["独家老卤工艺"] != "core" {
		t.Fatalf("差异化亮点应判 core，got %q", tier["独家老卤工艺"])
	}
	if tier["口感风味"] != "secondary" {
		t.Fatalf("无关键词应判 secondary，got %q", tier["口感风味"])
	}
	if tier["价格算账"] != "secondary" {
		t.Fatalf("价格算账在已有更强 core 时应降 secondary，got %q", tier["价格算账"])
	}
	// 全轮盘只能有一个 core。
	cores := 0
	for _, item := range wheel.Items {
		if item.Tier == "core" {
			cores++
		}
	}
	if cores != 1 {
		t.Fatalf("应恰好 1 个 core，got %d", cores)
	}
}

func TestAssignSellingPointTiersKeepsConfirmedTier(t *testing.T) {
	raw := `{"卖点列表":[
		{"卖点名":"物流发货","角度":["京东物流"],"tier":"core"},
		{"卖点名":"独家工艺","角度":["老卤慢炖"]}
	]}`
	out, err := assignSellingPointTiers(raw)
	if err != nil {
		t.Fatal(err)
	}
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(out), &wheel); err != nil {
		t.Fatal(err)
	}
	if wheel.Items[0].Tier != "core" {
		t.Fatalf("用户已确认的 tier 不应被覆盖，got %q", wheel.Items[0].Tier)
	}
	// 已有一个 core 时，第二个候选不应再抢 core。
	if wheel.Items[1].Tier == "core" {
		t.Fatalf("已有 core 时第二个候选不应再判 core，got %q", wheel.Items[1].Tier)
	}
}
