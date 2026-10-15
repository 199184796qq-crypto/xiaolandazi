package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
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

// resolveProductCategory 返回方案当前生效的商品品类 code：优先读已保存（自动或人工），
// 未判定时自动检测并落盘。检测失败或不确定返回空串，调用方回退母版自判。
func (s *Server) resolveProductCategory(ctx context.Context, tenant, plan int64, profileID, facts string) (string, error) {
	if saved, err := s.store.GetPlanProductCategory(ctx, tenant, plan); err == nil && strings.TrimSpace(saved) != "" {
		return saved, nil
	}
	detected, err := s.detectProductCategory(ctx, tenant, plan, profileID, facts)
	if err != nil {
		return "", err
	}
	if detected == "" {
		return "", nil
	}
	// 落盘失败不阻塞：本次仍用检测结果，下次重新检测。
	_ = s.store.SetPlanProductCategory(ctx, tenant, plan, detected, "auto", 0)
	return detected, nil
}

type planProductCategoryResponse struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Source   string `json:"source,omitempty"`
}

func (s *Server) planProductCategoryGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	category, source := "", ""
	if saved, err := s.store.GetPlanProductCategory(r.Context(), tenantID, planID); err == nil {
		category = saved
		source = "saved"
	}
	writeJSON(w, http.StatusOK, planProductCategoryResponse{Category: category, Label: productCategoryLabel(category), Source: source})
}

func (s *Server) planProductCategorySet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	var input struct {
		Category string `json:"category"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "品类设置请求格式错误")
		return
	}
	category := strings.TrimSpace(input.Category)
	// 允许空串 = 清除手动指定，回到自动判定。
	if category != "" && !validProductCategoryCode(category) {
		writeError(w, http.StatusBadRequest, "品类不在支持清单内")
		return
	}
	if err := s.store.SetPlanProductCategory(r.Context(), tenantID, planID, category, "manual", actor.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "品类保存失败")
		return
	}
	writeJSON(w, http.StatusOK, planProductCategoryResponse{Category: category, Label: productCategoryLabel(category), Source: "manual"})
}
