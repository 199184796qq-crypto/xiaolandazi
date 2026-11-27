package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarketingCampaignTimeLimitInput(t *testing.T) {
	start := "2026-11-23T15:00:00+08:00"
	end := "2026-11-24T15:00:00+08:00"
	tests := []struct {
		name     string
		mode     any
		start    string
		end      string
		audience string
		message  string
	}{
		{name: "unlimited", mode: true},
		{name: "limited with both dates", mode: false, start: start, end: end},
		{name: "limited with missing start", mode: false, end: end, message: "开始时间和结束时间"},
		{name: "limited with missing end", mode: false, start: start, message: "开始时间和结束时间"},
		{name: "limited ending at start", mode: false, start: start, end: start, message: "结束时间必须晚于开始时间"},
		{name: "limited invalid date", mode: false, start: "2026-02-30T11:00:00+08:00", end: end, message: "开始时间格式不正确"},
		{name: "unlimited with stale dates", mode: true, start: start, end: end, message: "不限时间的活动请清空"},
		{name: "new-account eligibility requires a start", mode: true, audience: "new_since_start", message: "新开户活动必须设置开始时间"},
		{name: "legacy start only", start: start},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{
				"name":      "营销活动",
				"items":     []map[string]any{{"target_type": "time_card", "target_id": 1, "pricing_mode": "free"}},
				"starts_at": tt.start, "ends_at": tt.end,
				"controls": map[string]any{"audience": tt.audience},
			}
			if tt.mode != nil {
				payload["unlimited_time"] = tt.mode
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/commercial/marketing-campaigns", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			input, ok := readMarketingCampaignInput(w, r)
			if tt.message != "" {
				if ok || w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tt.message) {
					t.Fatalf("want HTTP 400 with %q, got %d %s", tt.message, w.Code, w.Body.String())
				}
				return
			}
			if !ok || input.StartsAt != tt.start || input.EndsAt != tt.end {
				t.Fatalf("valid schedule rejected or changed: %+v %s", input, w.Body.String())
			}
		})
	}
}
