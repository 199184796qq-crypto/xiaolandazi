package httpapi

import (
	"encoding/json"
	"testing"
)

func TestRepairSellingPointWheelExtraQuote(t *testing.T) {
	raw := `{"卖点列表":[{"卖点名":"地址","角度":["说明地址"],"},{"卖点名":"停车","角度":["说明停车"]}]}`
	repaired := repairSellingPointWheelJSON(raw)
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(repaired), &got); err != nil {
		t.Fatalf("repaired JSON still invalid: %v\n%s", err, repaired)
	}
	var items []map[string]any
	if err := json.Unmarshal(got["卖点列表"], &items); err != nil || len(items) != 2 {
		t.Fatalf("unexpected items: %v %+v", err, items)
	}
	if items[0]["卖点名"] != "地址" || items[1]["卖点名"] != "停车" {
		t.Fatalf("content changed: %+v", items)
	}
}
