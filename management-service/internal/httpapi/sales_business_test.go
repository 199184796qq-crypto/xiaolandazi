package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestSalesBusinessValidation(t *testing.T) {
	valid := model.SalesLeadInput{BusinessName: "Test shop", Phone: "13800001234", Stage: "new"}
	tests := []struct {
		name    string
		change  func(*model.SalesLeadInput)
		wantErr bool
	}{
		{"valid", func(*model.SalesLeadInput) {}, false},
		{"empty business", func(p *model.SalesLeadInput) { p.BusinessName = " " }, true},
		{"missing contact", func(p *model.SalesLeadInput) { p.Phone = "" }, true},
		{"wechat only", func(p *model.SalesLeadInput) { p.Phone = ""; p.Wechat = "testwechat" }, false},
		{"cannot mark won through create", func(p *model.SalesLeadInput) { p.Stage = "won" }, true},
		{"cannot mark lost through edit", func(p *model.SalesLeadInput) { p.Stage = "lost" }, true},
		{"field length", func(p *model.SalesLeadInput) { p.ContactName = strings.Repeat("a", 129) }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.change(&p)
			if (validateSalesLeadInput(&p) != nil) != tc.wantErr {
				t.Fatal("unexpected validation result")
			}
		})
	}
	p := model.SalesLeadActivityInput{ActivityType: "visit", Content: "visit notes", Stage: "contacted"}
	if err := validateSalesActivity(&p); err != nil {
		t.Fatal(err)
	}
	p.Stage = "won"
	if validateSalesActivity(&p) == nil {
		t.Fatal("activity bypassed conversion workflow")
	}
	p.Stage = "contacted"
	p.Content = " "
	if validateSalesActivity(&p) == nil {
		t.Fatal("empty visit notes accepted")
	}
	c := model.ConvertSalesLeadInput{Username: "customer01", DisplayName: "Test", Phone: "13800001234", Province: "P", City: "C", District: "D", HandoffSummary: "Needs onboarding"}
	if err := validateSalesConversion(&c); err != nil {
		t.Fatal(err)
	}
	c.District = ""
	if validateSalesConversion(&c) == nil {
		t.Fatal("conversion accepted missing district")
	}
	c.District = "D"
	c.HandoffSummary = ""
	if err := validateSalesConversion(&c); err != nil {
		t.Fatal("registration must not require automatic operations handoff", err)
	}
	c.HandoffSummary = "notes"
	c.DeliveryMethod = "email"
	if validateSalesConversion(&c) == nil {
		t.Fatal("email delivery without email")
	}
}

func TestSalesBusinessUnknownOwnerFieldsRejected(t *testing.T) {
	for _, body := range []string{`{"business_name":"shop","owner_sales_staff_id":999}`, `{"business_name":"shop","converted_user_id":999}`, `{"to_sales_staff_id":2,"created_by_user_id":999}`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		var p model.SalesLeadInput
		if err := readJSON(w, r, &p); err == nil {
			t.Fatalf("unknown authority field accepted: %s", body)
		}
	}
}

func TestSalesBusinessAuditActions(t *testing.T) {
	tests := map[string]string{
		"/api/v1/sales/leads":                 "sales.lead.create",
		"/api/v1/sales/leads/5/activities":    "sales.lead.activity",
		"/api/v1/sales/leads/5/lost":          "sales.lead.lost",
		"/api/v1/sales/leads/5/convert":       "sales.lead.convert",
		"/api/v1/admin/sales/5/handover":      "sales.portfolio.handover",
		"/api/v1/liveops/customer-handoffs/5": "liveops.handoff.update",
	}
	for path, want := range tests {
		if got := auditAction(http.MethodPost, path); got != want {
			t.Fatalf("%s: %s want %s", path, got, want)
		}
	}
}
