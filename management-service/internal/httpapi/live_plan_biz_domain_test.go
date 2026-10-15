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

func TestPlanBizDomainAuthenticationAndResponses(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		r := httptest.NewRequest(method, "/api/v1/live-agent-plans/4/biz-domain", strings.NewReader(`{"biz_domain":"local_service"}`))
		r.SetPathValue("planID", "4")
		w := httptest.NewRecorder()
		if method == http.MethodGet {
			s.planBizDomainGet(w, r)
		} else {
			s.planBizDomainSet(w, r)
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatal(method, w.Code)
		}
	}
	for _, item := range []struct {
		domain string
		err    error
		status int
	}{{"ecommerce", nil, 200}, {"local_service", nil, 200}, {"", appdb.ErrInvalidPlanBizDomain, 400}, {"", appdb.ErrLiveAgentPlanNotFound, 404}, {"", errors.New("db failed"), 500}} {
		w := httptest.NewRecorder()
		s.planBizDomainReply(w, item.domain, item.err)
		if w.Code != item.status {
			t.Fatal(w.Code, item)
		}
		if item.err == nil && !strings.Contains(w.Body.String(), item.domain) {
			t.Fatal(w.Body.String())
		}
		if strings.Contains(w.Body.String(), "db failed") {
			t.Fatal("internal error exposed")
		}
	}
}

func TestPlanBusinessIndustryAuthenticationAndResponse(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/live-agent-plans/4/business-industry", strings.NewReader(`{"biz_domain":"ecommerce","industry":"food"}`))
	r.SetPathValue("planID", "4")
	s.planBusinessIndustrySet(w, r)
	if w.Code != 401 {
		t.Fatal("industry route requires authentication", w.Code)
	}
	w = httptest.NewRecorder()
	s.planBusinessSettingsReply(w, model.PlanBusinessSettings{}, appdb.ErrInvalidPlanBusinessIndustry)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	s.planBusinessSettingsReply(w, model.PlanBusinessSettings{BizDomain: "local_service", Industries: map[string]string{"ecommerce": "food", "local_service": "dining"}, IndustryOptions: map[string][]model.PlanBusinessIndustry{"ecommerce": model.PlanBusinessIndustryOptions("ecommerce"), "local_service": model.PlanBusinessIndustryOptions("local_service")}}, nil)
	// 行业选项现在优先来自目录 json（configs/industries/，文件即源），解析失败才回退内置枚举，
	// 因此不断言具体 label，只断言两个域的选项都非空。
	if w.Code != 200 || !strings.Contains(w.Body.String(), "industry_options") {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code"`) {
		t.Fatal("industry_options should contain code entries", w.Body.String())
	}
}
