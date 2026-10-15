package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

// productCategory 是方案级商品品类。Code 用于程序校验与存储，Label 用于展示与注入母版。
// 品类清单与 configs/prompts/anchor-style/category-selling-dimensions.md 对齐：该表是给模型的
// 完整维度表述（内容真相），这里只维护 code 与中文名的枚举（程序逻辑）。
type productCategory struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

var productCategoryCatalog = []productCategory{
	{"food", "食品/餐饮"},
	{"fresh", "生鲜/农产品"},
	{"beverage", "酒水饮料"},
	{"apparel", "服装/鞋帽"},
	{"beauty", "美妆/个护"},
	{"appliance", "电器/数码"},
	{"home", "家居/日用"},
	{"maternal", "母婴"},
	{"building", "建材/家装"},
	{"book", "图书/文教"},
	{"pet", "宠物"},
}

func productCategoryLabel(code string) string {
	for _, c := range productCategoryCatalog {
		if c.Code == code {
			return c.Label
		}
	}
	return ""
}

func validProductCategoryCode(code string) bool {
	return productCategoryLabel(code) != ""
}

// detectProductCategory 用卖点挖掘模型判定方案商品品类。判定失败、返回空串或非法 code 时，
// 一律返回空串，调用方回退到母版自判，不阻塞卖点挖掘初始化。
func (s *Server) detectProductCategory(ctx context.Context, tenant, plan int64, profileID, facts string) (string, error) {
	if strings.TrimSpace(profileID) == "" {
		return "", nil
	}
	prompt, _, err := loadCategoryDetectionPrompt()
	if err != nil {
		return "", err
	}
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	payload, _ := json.Marshal(map[string]string{"fact_package": facts})
	result, _, callErr := s.completeAnchorStyleWorkbench(ctx, profileID, "category_detection", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(payload)}}, 300, false)
	if callErr != nil {
		return "", nil
	}
	var out struct {
		Code       string `json:"code"`
		Label      string `json:"label"`
		Confidence string `json:"confidence"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &out); err != nil {
		return "", nil
	}
	code := strings.TrimSpace(out.Code)
	if code == "" || !validProductCategoryCode(code) {
		return "", nil
	}
	return code, nil
}
