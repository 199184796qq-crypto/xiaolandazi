package httpapi

import (
	"livecompanion/management/internal/auth"
	appdb "livecompanion/management/internal/db"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLiveCardCopyRequiresAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, handler := range []http.HandlerFunc{s.liveAgentPlanProductLinkCopy, s.liveAgentPlanBenefitCopy} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/live-agent-plans/4/product-links/7/copy", strings.NewReader(`{"expected_version_no":1}`))
		handler(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatal("copy without authentication", w.Code)
		}
	}
}
func TestLiveCardCopyErrorsDoNotReportFalseSuccess(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{appdb.ErrLiveAgentPlanNotFound, 404}, {appdb.ErrLiveAgentPlanProductLinkVersionConflict, 409}, {appdb.ErrLiveAgentPlanBenefitVersionConflict, 409}, {appdb.ErrLiveCardCopyInvalid, 400}} {
		w := httptest.NewRecorder()
		if !writeLiveCardCopyError(w, tc.err) || w.Code != tc.status {
			t.Fatal(tc, w.Code)
		}
	}
}
