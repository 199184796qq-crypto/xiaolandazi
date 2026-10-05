// Command wechatpay-preflight performs no-charge credential checks only.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/validators"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/wechatpay"
)

type report struct {
	ConfigValid              bool   `json:"configValid"`
	MerchantAuthentication   bool   `json:"merchantAuthentication"`
	MerchantHTTPStatus       int    `json:"merchantHTTPStatus,omitempty"`
	MerchantCode             string `json:"merchantCode,omitempty"`
	WechatPublicKeyVerified  bool   `json:"wechatPublicKeyVerified"`
	SignedErrorResponse      bool   `json:"signedErrorResponse"`
	OfficialAccountAuth      bool   `json:"officialAccountAuthentication"`
	OfficialAccountCode      int    `json:"officialAccountCode,omitempty"`
	FailureStage             string `json:"failureStage,omitempty"`
	ProductionPaymentEnabled bool   `json:"productionPaymentEnabled"`
	CreatedPaymentOrder      bool   `json:"createdPaymentOrder"`
	ForceRefreshedToken      bool   `json:"forceRefreshedToken"`
	SensitiveValuesPrinted   bool   `json:"sensitiveValuesPrinted"`
}

func main() {
	result := run()
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if !result.ConfigValid || !result.MerchantAuthentication || !result.OfficialAccountAuth {
		os.Exit(1)
	}
}

func run() report {
	result := report{}
	cfg := config.Load().WechatPay
	if cfg.Enabled {
		result.ProductionPaymentEnabled = true
		result.FailureStage = "production_payment_must_be_disabled"
		return result
	}
	// This flag changes only this diagnostic process, not the env file or service.
	cfg.Enabled = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	service, err := wechatpay.New(ctx, cfg)
	if err != nil {
		result.FailureStage = "configuration"
		return result
	}
	result.ConfigValid = true
	nonce := make([]byte, 10)
	if _, err := rand.Read(nonce); err != nil {
		result.FailureStage = "randomness"
		return result
	}
	// A random non-business order number: query only, never prepay or close.
	_, queryError := service.Query(ctx, "WXPREFLIGHT"+hex.EncodeToString(nonce))
	var apiError *core.APIError
	if errors.As(queryError, &apiError) {
		result.MerchantHTTPStatus = apiError.StatusCode
		if regexp.MustCompile(`^[A-Z_]{1,64}$`).MatchString(apiError.Code) {
			result.MerchantCode = apiError.Code
		}
		result.MerchantAuthentication = apiError.StatusCode == 404 && apiError.Code == "ORDER_NOT_EXIST"
		// SDK v0.2.21 checks status before signature on non-2xx replies.
		// Explicitly verify signed errors too; don't imply an unsigned error tests the public key.
		if apiError.Header.Get("Wechatpay-Signature") != "" {
			result.SignedErrorResponse = true
			publicKey, err := utils.LoadPublicKeyWithPath(cfg.WechatPayPublicKeyPath)
			if err == nil {
				validator := validators.NewWechatPayResponseValidator(
					verifiers.NewSHA256WithRSAPubkeyVerifier(cfg.WechatPayPublicKeyID, *publicKey))
				response := &http.Response{Header: apiError.Header, Body: io.NopCloser(bytes.NewBufferString(apiError.Body))}
				result.WechatPublicKeyVerified = validator.Validate(ctx, response) == nil
			}
			if !result.WechatPublicKeyVerified {
				result.MerchantAuthentication = false
				result.FailureStage = "signed_response_verification"
			}
		}
	}
	if !result.MerchantAuthentication && result.FailureStage == "" {
		result.FailureStage = "merchant_authentication"
	}
	result.OfficialAccountAuth, result.OfficialAccountCode = checkOfficialAccount(ctx, cfg)
	if !result.OfficialAccountAuth && result.FailureStage == "" {
		result.FailureStage = "official_account_authentication"
	}
	return result
}

// https://developers.weixin.qq.com/doc/service/api/base/api_getstableaccesstoken.html
// Ordinary mode does not force-refresh/invalidate any existing token.
func checkOfficialAccount(ctx context.Context, cfg wechatpay.Config) (bool, int) {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return checkOfficialAccountWithClient(ctx, cfg, client)
}

func checkOfficialAccountWithClient(ctx context.Context, cfg wechatpay.Config, client *http.Client) (bool, int) {
	body, err := json.Marshal(map[string]any{
		"grant_type": "client_credential", "appid": cfg.AppID,
		"secret": cfg.OfficialAccountAppSecret, "force_refresh": false,
	})
	if err != nil {
		return false, 0
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.weixin.qq.com/cgi-bin/stable_token", bytes.NewReader(body))
	if err != nil {
		return false, 0
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return false, 0
	}
	defer response.Body.Close()
	var reply struct {
		Token     string `json:"access_token"`
		ExpiresIn int    `json:"expires_in"`
		ErrorCode int    `json:"errcode"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 16384)).Decode(&reply) != nil {
		return false, 0
	}
	return response.StatusCode == 200 && reply.ErrorCode == 0 && reply.Token != "" && reply.ExpiresIn > 0, reply.ErrorCode
}
