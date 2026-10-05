package wechatpay

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
)

// ValidNativeCodeURL rejects web/JavaScript links; only a signed provider
// response may supply the actual payment URL. No external QR image service.
func ValidNativeCodeURL(value string) bool {
	if len(value) == 0 || len(value) > 512 || strings.ContainsAny(value, "\r\n\t ") {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "weixin" && u.Host != "" && u.User == nil && u.Fragment == "" && u.RawQuery != ""
}

func NativeQRCodeDataURL(codeURL string) (string, error) {
	if !ValidNativeCodeURL(codeURL) {
		return "", errors.New("invalid native payment URL")
	}
	png, err := qrcode.Encode(codeURL, qrcode.Medium, 256)
	if err != nil {
		return "", errors.New("native QR encoding failed")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func (s *Service) NativePrepay(ctx context.Context, input PrepayInput) (string, error) {
	if !s.Enabled() {
		return "", errors.New("wechat pay is disabled")
	}
	if strings.TrimSpace(input.OutTradeNo) == "" || len(input.OutTradeNo) > 32 || input.OpenID != "" ||
		input.AmountCents == 0 || input.AmountCents > math.MaxInt32 || !input.ExpiresAt.After(time.Now()) {
		return "", errors.New("invalid native prepay input")
	}
	description := truncateUTF8(strings.TrimSpace(input.Description), 120)
	if description == "" {
		description = "小蓝直播搭子钱包充值"
	}
	expiresAt := input.ExpiresAt.UTC()
	api := native.NativeApiService{Client: s.jsapi.Client}
	response, _, err := api.Prepay(ctx, native.PrepayRequest{
		Appid: core.String(s.cfg.AppID), Mchid: core.String(s.cfg.MchID),
		Description: core.String(description), OutTradeNo: core.String(input.OutTradeNo),
		NotifyUrl: core.String(s.cfg.NotifyURL), TimeExpire: &expiresAt,
		Amount: &native.Amount{Total: core.Int64(int64(input.AmountCents)), Currency: core.String("CNY")},
	})
	if err != nil {
		return "", err
	}
	if response == nil || response.CodeUrl == nil || !ValidNativeCodeURL(*response.CodeUrl) {
		return "", errors.New("invalid native prepay response")
	}
	return *response.CodeUrl, nil
}

func IsNativeNotAuthorized(err error) bool { return core.IsAPIError(err, "NO_AUTH") }
