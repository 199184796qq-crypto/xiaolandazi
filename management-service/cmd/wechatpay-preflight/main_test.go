package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"livecompanion/management/internal/wechatpay"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestOfficialAccountCheckUsesNonRefreshingPOSTAndDoesNotReturnToken(t *testing.T) {
	cfg := wechatpay.Config{AppID: "test-application", OfficialAccountAppSecret: "test-secret"}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != "https://api.weixin.qq.com/cgi-bin/stable_token" {
			t.Fatal("unexpected endpoint or method")
		}
		var body struct {
			Grant  string `json:"grant_type"`
			AppID  string `json:"appid"`
			Secret string `json:"secret"`
			Force  *bool  `json:"force_refresh"`
		}
		if json.NewDecoder(request.Body).Decode(&body) != nil || body.Force == nil || *body.Force ||
			body.AppID != cfg.AppID || body.Secret != cfg.OfficialAccountAppSecret || body.Grant != "client_credential" {
			t.Fatal("unexpected authentication payload")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"test-sensitive-token","expires_in":7200}`))}, nil
	})}
	ok, code := checkOfficialAccountWithClient(context.Background(), cfg, client)
	if !ok || code != 0 {
		t.Fatal("authentication result not recognized")
	}
	encoded, err := json.Marshal(report{OfficialAccountAuth: ok})
	if err != nil || strings.Contains(string(encoded), "test-sensitive-token") || strings.Contains(string(encoded), "test-secret") {
		t.Fatal("unsafe report")
	}
}

func TestOfficialAccountFailuresOnlyReturnSafeCode(t *testing.T) {
	for _, body := range []string{`{"errcode":40164,"errmsg":"sensitive-details"}`, `{"expires_in":7200}`, `not-json`} {
		client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		ok, _ := checkOfficialAccountWithClient(context.Background(), wechatpay.Config{}, client)
		if ok {
			t.Fatal("invalid response accepted")
		}
	}
}

func TestEnabledProductionIsNeverUsedByPreflight(t *testing.T) {
	t.Setenv("WECHAT_PAY_ENABLED", "true")
	result := run()
	if !result.ProductionPaymentEnabled || result.ConfigValid || result.MerchantAuthentication || result.CreatedPaymentOrder || result.FailureStage != "production_payment_must_be_disabled" {
		t.Fatal("preflight did not refuse enabled production")
	}
}
