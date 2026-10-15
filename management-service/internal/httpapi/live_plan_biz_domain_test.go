package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/auth"
	appdb "livecompanion/management/internal/db"
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
