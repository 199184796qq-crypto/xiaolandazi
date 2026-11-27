package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTimeCardInputRequiresNameAndPositiveTerms(t *testing.T) {
	for _, workspace := range []bool{true, false} {
		for _, tc := range []struct {
			name     string
			title    string
			seconds  int
			cents    int
			wantText string
		}{
			{"blank name", "   ", 36000, 10000, "请输入时长卡名称"},
			{"below one hour", "测试时长卡", 3599, 10000, "时长必须大于 1 小时"},
			{"one hour", "测试时长卡", 3600, 10000, "时长必须大于 1 小时"},
			{"zero price", "测试时长卡", 36000, 0, "价格必须大于 0 元"},
			{"negative price", "测试时长卡", 36000, -1, "请求格式错误"},
			{"minimum valid duration and price", " 测试时长卡 ", 3601, 1, ""},
			{"default terms", " 测试时长卡 ", 36000, 10000, ""},
		} {
			t.Run(fmt.Sprintf("workspace=%t/%s", workspace, tc.name), func(t *testing.T) {
				body := fmt.Sprintf(`{"name":%q,"duration_seconds":%d,"price_cents":%d,"validity_days":7,"activation_deadline_days":20}`,
					tc.title, tc.seconds, tc.cents)
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
				response := httptest.NewRecorder()
				input, ok := readTimeCardInput(response, req, workspace)
				if tc.wantText != "" {
					if ok || response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), tc.wantText) {
						t.Fatalf("accepted=%t status=%d response=%s", ok, response.Code, response.Body)
					}
					return
				}
				if !ok || input.Name != "测试时长卡" || input.DurationSeconds != uint64(tc.seconds) || input.PriceCents != uint64(tc.cents) {
					t.Fatalf("accepted=%t input=%+v response=%s", ok, input, response.Body)
				}
			})
		}
	}
}
