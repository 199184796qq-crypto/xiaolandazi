package wechatpay

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OfficialAccountOAuth uses the public account credentials independently of merchant payments.
type OfficialAccountOAuth struct {
	appID    string
	secret   string
	callback string
	client   *http.Client
}

func NewOfficialAccountOAuth(cfg Config, callback string) (*OfficialAccountOAuth, error) {
	appID, secret := strings.TrimSpace(cfg.AppID), strings.TrimSpace(cfg.OfficialAccountAppSecret)
	uri, err := url.Parse(callback)
	if appID == "" || len(appID) > 64 || secret == "" || err != nil || uri.Scheme != "https" ||
		uri.Hostname() == "" || uri.User != nil || uri.RawQuery != "" || uri.Fragment != "" {
		return nil, errors.New("wechat login requires official account credentials and an HTTPS callback")
	}
	return &OfficialAccountOAuth{appID: appID, secret: secret, callback: callback, client: &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (o *OfficialAccountOAuth) AppID() string { return o.appID }

func (o *OfficialAccountOAuth) OAuthURL(state string) string {
	return officialOAuthURL(o.appID, o.callback, state)
}

func (o *OfficialAccountOAuth) ExchangeOAuthCode(ctx context.Context, code string) (string, error) {
	return exchangeOfficialOAuthCode(ctx, o.client, o.appID, o.secret, code)
}
