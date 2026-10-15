package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func TestSupplementProductSelectionAndBenefitIsolation(t *testing.T) {
	for _, names := range [][2]string{{"食品礼盒", "单只食品"}, {"十人餐", "双人餐"}, {"A型号配件", "B型号配件"}} {
		links := []model.LiveAgentPlanProductLink{
			{ID: 11, TenantID: 14, PlanID: 4, LinkKey: "1", ProductName: names[0], Status: "active", Spec: "规格A", DailyPrice: "87.6"},
			{ID: 12, TenantID: 14, PlanID: 4, LinkKey: "2", ProductName: names[1], Status: "active", Spec: "规格B", DailyPrice: "337"},
		}
		benefits := []model.LiveAgentPlanBenefit{
			{ID: 1, TenantID: 14, PlanID: 4, Key: "共用", Status: "active"},
			{ID: 2, TenantID: 14, PlanID: 4, Key: "另一个商品", LinkKey: "2", ActivityPrice: "123", Status: "active"},
			{ID: 3, TenantID: 14, PlanID: 4, Key: "显式单商品范围", LinkKey: "1", Status: "active"},
			{ID: 4, TenantID: 14, PlanID: 4, Key: "显式全部", LinkKey: "2", Status: "active"},
		}
		scopes := map[int64]appdb.BenefitScope{3: {Products: []int64{12}}, 4: {All: true}}
		chosen, applicable, err := selectSupplementProduct(14, 4, 11, links, benefits, scopes)
		if err != nil || len(chosen) != 1 || chosen[0].ID != 11 || len(applicable) != 2 || applicable[0].ID != 1 || applicable[1].ID != 4 {
			t.Fatal(chosen, applicable, err)
		}
		text := renderAnchorStyleWorkbenchFacts(model.LiveAgentPlan{Name: "方案"}, nil, applicable, chosen)
		if !strings.Contains(text, names[0]) || !strings.Contains(text, "87.6") || strings.Contains(text, names[1]) || strings.Contains(text, "337") || strings.Contains(text, "123") {
			t.Fatal("another product or price leaked", text)
		}
		for _, scope := range [][3]int64{{14, 4, 99}, {15, 4, 11}, {14, 5, 11}} {
			if _, _, err = selectSupplementProduct(scope[0], scope[1], scope[2], links, benefits, scopes); !errors.Is(err, errSupplementProductNotFound) {
				t.Fatal("invalid ownership accepted", scope, err)
			}
		}
		links[0].Status = "disabled"
		if _, _, err = selectSupplementProduct(14, 4, 11, links, benefits, scopes); !errors.Is(err, errSupplementProductNotFound) {
			t.Fatal("disabled product accepted", err)
		}
	}
}

func TestSupplementCheckPayloadRetainsP0AndExplicitTarget(t *testing.T) {
	p0 := "完整有效P0\n适用条件、例外均保留。"
	target := &supplementProductTarget{Link: "2", Name: "双人餐"}
	for _, scope := range []*supplementProductTarget{nil, target} {
		payload := factGapCheckPayload("限定事实", p0, scope)
		if payload["effective_p0"] != p0 || payload["fact_package"] != "限定事实" || payload["target_product"] != scope {
			t.Fatal(payload)
		}
	}
	for _, path := range []string{"configs/prompts/anchor-style/fact-gap-checklist.md", "configs/prompts/anchor-style/product-supplement-chat.md"} {
		prompt, _, err := loadReplaceablePromptAsset("", path, "补全母版")
		if err != nil || !strings.Contains(prompt, "effective_p0") || !strings.Contains(prompt, "target_product") {
			t.Fatal(path, err)
		}
	}
}

func TestSupplementScopeRequiresAuthenticationAndHidesInternalErrors(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, path := range []string{"fact-gap-check", "product-supplement/chat"} {
		r := httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/"+path, strings.NewReader(`{"product_link_id":11}`))
		r.SetPathValue("planID", "4")
		w := httptest.NewRecorder()
		if path == "fact-gap-check" {
			s.factGapCheck(w, r)
		} else {
			s.speechMinerGuide(w, r)
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatal(path, w.Code)
		}
	}
	for _, item := range []struct {
		err  error
		code int
	}{{errSupplementProductNotFound, 404}, {errors.New("private db error"), 500}} {
		w := httptest.NewRecorder()
		supplementScopeError(w, item.err)
		if w.Code != item.code || strings.Contains(w.Body.String(), "private db error") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
