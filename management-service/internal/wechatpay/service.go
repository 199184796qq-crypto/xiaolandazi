package wechatpay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/jsapi"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

const oauthEndpoint = "https://open.weixin.qq.com/connect/oauth2/authorize"
const oauthTokenEndpoint = "https://api.weixin.qq.com/sns/oauth2/access_token"

type Config struct {
	Enabled                   bool
	AppID                     string
	MchID                     string
	MerchantCertificateSerial string
	MerchantPrivateKeyPath    string
	APIV3Key                  string
	WechatPayPublicKeyID      string
	WechatPayPublicKeyPath    string
	OfficialAccountAppSecret  string
	NotifyURL                 string
	OAuthCallbackURL          string
}

type PrepayInput struct {
	Description string
	OutTradeNo  string
	OpenID      string
	ClientIP    string
	AmountCents uint64
	ExpiresAt   time.Time
}

type JSAPIPaymentParams struct {
	AppID     string `json:"appId"`
	TimeStamp string `json:"timeStamp"`
	NonceStr  string `json:"nonceStr"`
	Package   string `json:"package"`
	SignType  string `json:"signType"`
	PaySign   string `json:"paySign"`
	PrepayID  string `json:"-"`
}

type Transaction struct {
	NotificationID   string
	EventType        string
	AppID            string
	MchID            string
	OutTradeNo       string
	TransactionID    string
	TradeState       string
	TradeType        string
	Currency         string
	AmountCents      uint64
	PayerAmountCents uint64
	PayerAmountKnown bool
	SuccessTime      time.Time
	PayerOpenID      string
}

type Service struct {
	cfg           Config
	jsapi         jsapi.JsapiApiService
	notifyHandler *notify.Handler
	httpClient    *http.Client
}

func New(ctx context.Context, cfg Config) (*Service, error) {
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.MchID = strings.TrimSpace(cfg.MchID)
	cfg.MerchantCertificateSerial = strings.TrimSpace(cfg.MerchantCertificateSerial)
	cfg.MerchantPrivateKeyPath = strings.TrimSpace(cfg.MerchantPrivateKeyPath)
	cfg.WechatPayPublicKeyID = strings.TrimSpace(cfg.WechatPayPublicKeyID)
	cfg.WechatPayPublicKeyPath = strings.TrimSpace(cfg.WechatPayPublicKeyPath)
	cfg.NotifyURL = strings.TrimSpace(cfg.NotifyURL)
	cfg.OAuthCallbackURL = strings.TrimSpace(cfg.OAuthCallbackURL)

	service := &Service{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
	}
	if !cfg.Enabled {
		return service, nil
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	merchantPrivateKey, err := utils.LoadPrivateKeyWithPath(cfg.MerchantPrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load merchant private key: %w", err)
	}

	var (
		clientOption core.ClientOption
		verifier     auth.Verifier
	)
	if cfg.WechatPayPublicKeyID != "" || cfg.WechatPayPublicKeyPath != "" {
		if cfg.WechatPayPublicKeyID == "" || cfg.WechatPayPublicKeyPath == "" {
			return nil, errors.New("wechat pay public key id and path must be configured together")
		}
		publicKey, err := utils.LoadPublicKeyWithPath(cfg.WechatPayPublicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load wechat pay public key: %w", err)
		}
		clientOption = option.WithWechatPayPublicKeyAuthCipher(
			cfg.MchID,
			cfg.MerchantCertificateSerial,
			merchantPrivateKey,
			cfg.WechatPayPublicKeyID,
			publicKey,
		)
		verifier = verifiers.NewSHA256WithRSAPubkeyVerifier(cfg.WechatPayPublicKeyID, *publicKey)
	} else {
		clientOption = option.WithWechatPayAutoAuthCipher(
			cfg.MchID,
			cfg.MerchantCertificateSerial,
			merchantPrivateKey,
			cfg.APIV3Key,
		)
		verifier = verifiers.NewSHA256WithRSAVerifier(
			downloader.MgrInstance().GetCertificateVisitor(cfg.MchID),
		)
	}

	client, err := core.NewClient(ctx, clientOption, option.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}))
	if err != nil {
		return nil, fmt.Errorf("initialize wechat pay client: %w", err)
	}
	notifyHandler, err := notify.NewRSANotifyHandler(cfg.APIV3Key, verifier)
	if err != nil {
		return nil, fmt.Errorf("initialize wechat pay notify handler: %w", err)
	}
	service.jsapi = jsapi.JsapiApiService{Client: client}
	service.notifyHandler = notifyHandler
	return service, nil
}

func validateConfig(cfg Config) error {
	required := []struct {
		name  string
		value string
	}{
		{"WECHAT_PAY_APP_ID", cfg.AppID},
		{"WECHAT_PAY_MCH_ID", cfg.MchID},
		{"WECHAT_PAY_MERCHANT_CERT_SERIAL_NO", cfg.MerchantCertificateSerial},
		{"WECHAT_PAY_MERCHANT_PRIVATE_KEY_PATH", cfg.MerchantPrivateKeyPath},
		{"WECHAT_PAY_API_V3_KEY", cfg.APIV3Key},
		{"WECHAT_OFFICIAL_ACCOUNT_APP_SECRET", cfg.OfficialAccountAppSecret},
		{"WECHAT_PAY_NOTIFY_URL", cfg.NotifyURL},
		{"WECHAT_PAY_OAUTH_CALLBACK_URL", cfg.OAuthCallbackURL},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required when wechat pay is enabled", field.name)
		}
	}
	if len([]byte(cfg.APIV3Key)) != 32 {
		return errors.New("WECHAT_PAY_API_V3_KEY must contain exactly 32 bytes")
	}
	for _, rawURL := range []string{cfg.NotifyURL, cfg.OAuthCallbackURL} {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("wechat pay callback URL must be public HTTPS without query: %s", rawURL)
		}
	}
	return nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.notifyHandler != nil && s.jsapi.Client != nil
}

func (s *Service) AppID() string {
	if s == nil {
		return ""
	}
	return s.cfg.AppID
}

func (s *Service) MchID() string {
	if s == nil {
		return ""
	}
	return s.cfg.MchID
}

func (s *Service) OAuthURL(state string) (string, error) {
	if !s.Enabled() {
		return "", errors.New("wechat pay is disabled")
	}
	if strings.TrimSpace(state) == "" {
		return "", errors.New("wechat oauth state is required")
	}
	query := url.Values{}
	query.Set("appid", s.cfg.AppID)
	query.Set("redirect_uri", s.cfg.OAuthCallbackURL)
	query.Set("response_type", "code")
	query.Set("scope", "snsapi_base")
	query.Set("state", state)
	return oauthEndpoint + "?" + query.Encode() + "#wechat_redirect", nil
}

type oauthTokenResponse struct {
	OpenID  string `json:"openid"`
	ErrCode int64  `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (s *Service) ExchangeOAuthCode(ctx context.Context, code string) (string, error) {
	if !s.Enabled() {
		return "", errors.New("wechat pay is disabled")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errors.New("wechat oauth code is required")
	}
	query := url.Values{}
	query.Set("appid", s.cfg.AppID)
	query.Set("secret", s.cfg.OfficialAccountAppSecret)
	query.Set("code", code)
	query.Set("grant_type", "authorization_code")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthTokenEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		// net/url.Error contains the URL including AppSecret and the OAuth code.
		return "", errors.New("wechat oauth request failed")
	}
	defer resp.Body.Close()
	var payload oauthTokenResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := decoder.Decode(&payload); err != nil {
		return "", fmt.Errorf("decode wechat oauth response: %w", err)
	}
	if resp.StatusCode/100 != 2 || payload.ErrCode != 0 || strings.TrimSpace(payload.OpenID) == "" {
		return "", fmt.Errorf("wechat oauth rejected: code=%d message=%s", payload.ErrCode, payload.ErrMsg)
	}
	return strings.TrimSpace(payload.OpenID), nil
}

func (s *Service) Prepay(ctx context.Context, input PrepayInput) (JSAPIPaymentParams, error) {
	if !s.Enabled() {
		return JSAPIPaymentParams{}, errors.New("wechat pay is disabled")
	}
	input.Description = truncateUTF8(strings.TrimSpace(input.Description), 120)
	if input.Description == "" {
		input.Description = "小蓝商城订单"
	}
	if strings.TrimSpace(input.OutTradeNo) == "" || strings.TrimSpace(input.OpenID) == "" ||
		input.AmountCents == 0 || input.AmountCents > uint64(^uint64(0)>>1) {
		return JSAPIPaymentParams{}, errors.New("wechat prepay input is invalid")
	}
	amount := int64(input.AmountCents)
	expiresAt := input.ExpiresAt.UTC()
	request := jsapi.PrepayRequest{
		Appid:       core.String(s.cfg.AppID),
		Mchid:       core.String(s.cfg.MchID),
		Description: core.String(input.Description),
		OutTradeNo:  core.String(strings.TrimSpace(input.OutTradeNo)),
		TimeExpire:  &expiresAt,
		NotifyUrl:   core.String(s.cfg.NotifyURL),
		Amount: &jsapi.Amount{
			Total:    core.Int64(amount),
			Currency: core.String("CNY"),
		},
		Payer: &jsapi.Payer{Openid: core.String(strings.TrimSpace(input.OpenID))},
	}
	if clientIP := strings.TrimSpace(input.ClientIP); clientIP != "" {
		request.SceneInfo = &jsapi.SceneInfo{PayerClientIp: core.String(clientIP)}
	}
	response, _, err := s.jsapi.PrepayWithRequestPayment(ctx, request)
	if err != nil {
		return JSAPIPaymentParams{}, err
	}
	if response == nil || response.PrepayId == nil || response.Appid == nil ||
		response.TimeStamp == nil || response.NonceStr == nil || response.Package == nil ||
		response.SignType == nil || response.PaySign == nil {
		return JSAPIPaymentParams{}, errors.New("wechat prepay response is incomplete")
	}
	return JSAPIPaymentParams{
		AppID:     *response.Appid,
		TimeStamp: *response.TimeStamp,
		NonceStr:  *response.NonceStr,
		Package:   *response.Package,
		SignType:  *response.SignType,
		PaySign:   *response.PaySign,
		PrepayID:  *response.PrepayId,
	}, nil
}

func (s *Service) ParsePaymentNotification(ctx context.Context, request *http.Request) (Transaction, error) {
	if !s.Enabled() {
		return Transaction{}, errors.New("wechat pay is disabled")
	}
	// The SDK assumes a non-nil resource and a 12-byte GCM nonce. Validate the
	// envelope shape while retaining the exact bytes for signature verification.
	body, err := io.ReadAll(io.LimitReader(request.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return Transaction{}, errors.New("wechat notification body is invalid")
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	var envelope struct {
		Resource *struct {
			Nonce string `json:"nonce"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Resource == nil || len(envelope.Resource.Nonce) != 12 {
		return Transaction{}, errors.New("wechat notification resource is invalid")
	}
	content := new(payments.Transaction)
	notification, err := s.notifyHandler.ParseNotifyRequest(ctx, request, content)
	if err != nil {
		// The SDK error can contain the decrypted body or original request.
		return Transaction{}, errors.New("wechat notification verification or decryption failed")
	}
	transaction, err := normalizeTransaction(content)
	if err != nil {
		return Transaction{}, err
	}
	transaction.NotificationID = notification.ID
	transaction.EventType = notification.EventType
	return transaction, nil
}

func (s *Service) Query(ctx context.Context, outTradeNo string) (Transaction, error) {
	if !s.Enabled() {
		return Transaction{}, errors.New("wechat pay is disabled")
	}
	response, _, err := s.jsapi.QueryOrderByOutTradeNo(ctx, jsapi.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(strings.TrimSpace(outTradeNo)),
		Mchid:      core.String(s.cfg.MchID),
	})
	if err != nil {
		return Transaction{}, err
	}
	return normalizeTransaction(response)
}

func (s *Service) Close(ctx context.Context, outTradeNo string) error {
	if !s.Enabled() {
		return errors.New("wechat pay is disabled")
	}
	_, err := s.jsapi.CloseOrder(ctx, jsapi.CloseOrderRequest{
		OutTradeNo: core.String(outTradeNo), Mchid: core.String(s.cfg.MchID),
	})
	return err
}

func IsOrderNotExist(err error) bool { return core.IsAPIError(err, "ORDER_NOT_EXIST") }

// RequestPayment signs fresh bridge parameters for an existing prepay_id. The
// browser never receives the merchant key and retries reuse the same payment.
func (s *Service) RequestPayment(ctx context.Context, prepayID string) (JSAPIPaymentParams, error) {
	if !s.Enabled() || strings.TrimSpace(prepayID) == "" {
		return JSAPIPaymentParams{}, errors.New("wechat prepay id is missing")
	}
	nonce, err := utils.GenerateNonce()
	if err != nil {
		return JSAPIPaymentParams{}, err
	}
	params := JSAPIPaymentParams{
		AppID: s.cfg.AppID, TimeStamp: strconv.FormatInt(time.Now().Unix(), 10),
		NonceStr: nonce, Package: "prepay_id=" + prepayID, SignType: "RSA", PrepayID: prepayID,
	}
	message := fmt.Sprintf("%s\n%s\n%s\n%s\n", params.AppID, params.TimeStamp, params.NonceStr, params.Package)
	signed, err := s.jsapi.Client.Sign(ctx, message)
	if err != nil {
		return JSAPIPaymentParams{}, err
	}
	params.PaySign = signed.Signature
	return params, nil
}

func normalizeTransaction(content *payments.Transaction) (Transaction, error) {
	if content == nil || content.Appid == nil || content.Mchid == nil ||
		content.OutTradeNo == nil || content.TradeState == nil {
		return Transaction{}, errors.New("wechat transaction is incomplete")
	}
	transaction := Transaction{
		AppID:      strings.TrimSpace(*content.Appid),
		MchID:      strings.TrimSpace(*content.Mchid),
		OutTradeNo: strings.TrimSpace(*content.OutTradeNo),
		TradeState: strings.TrimSpace(*content.TradeState),
	}
	if content.TradeType != nil {
		transaction.TradeType = strings.TrimSpace(*content.TradeType)
	}
	if content.Amount != nil && content.Amount.Total != nil && content.Amount.Currency != nil {
		if *content.Amount.Total < 0 {
			return Transaction{}, errors.New("wechat transaction amount is invalid")
		}
		transaction.Currency = strings.TrimSpace(*content.Amount.Currency)
		transaction.AmountCents = uint64(*content.Amount.Total)
		if content.Amount.PayerTotal != nil && *content.Amount.PayerTotal >= 0 &&
			(content.Amount.PayerCurrency == nil || *content.Amount.PayerCurrency == "CNY") {
			transaction.PayerAmountCents = uint64(*content.Amount.PayerTotal)
			transaction.PayerAmountKnown = true
		}
	}
	if content.Payer != nil && content.Payer.Openid != nil {
		transaction.PayerOpenID = strings.TrimSpace(*content.Payer.Openid)
	}
	if content.TransactionId != nil {
		transaction.TransactionID = strings.TrimSpace(*content.TransactionId)
	}
	if content.SuccessTime != nil && strings.TrimSpace(*content.SuccessTime) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*content.SuccessTime))
		if err != nil {
			return Transaction{}, fmt.Errorf("parse wechat success time: %w", err)
		}
		transaction.SuccessTime = parsed
	}
	if transaction.TradeState == "SUCCESS" && (transaction.TransactionID == "" || transaction.SuccessTime.IsZero() ||
		transaction.Currency != "CNY" || transaction.AmountCents == 0 || transaction.PayerOpenID == "") {
		return Transaction{}, errors.New("successful wechat transaction is incomplete")
	}
	return transaction, nil
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		if size <= 0 {
			return ""
		}
		value = value[:len(value)-size]
	}
	return value
}
