package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/wechatpay"
)

const wechatLoginBase = "/api/v1/auth/wechat"
const wechatLoginCookie = "xiaolan_wechat_oauth"
const wechatLoginTTL = 10 * time.Minute

type wechatLoginOAuth interface {
	AppID() string
	OAuthURL(string) string
	ExchangeOAuthCode(context.Context, string) (string, error)
}

func (s *Server) EnableWechatLogin(ctx context.Context, cfg wechatpay.Config) error {
	provider, err := wechatpay.NewOfficialAccountOAuth(cfg, s.publicWebURL+wechatLoginBase+"/callback")
	if err != nil {
		return err
	}
	if err := s.store.MigrateWechatLogin(ctx); err != nil {
		return err
	}
	s.wechatLogin = provider
	return nil
}

func (s *Server) registerWechatLoginRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+wechatLoginBase+"/status", s.wechatLoginStatus)
	mux.HandleFunc("GET "+wechatLoginBase+"/start", s.wechatLoginStart)
	mux.HandleFunc("GET "+wechatLoginBase+"/scan", s.wechatScanStart)
	mux.HandleFunc("GET "+wechatLoginBase+"/callback", s.wechatLoginCallback)
	mux.HandleFunc("POST "+wechatLoginBase+"/bind", func(w http.ResponseWriter, r *http.Request) { s.wechatBindingStart(w, r, false) })
	mux.HandleFunc("POST "+wechatLoginBase+"/unbind", func(w http.ResponseWriter, r *http.Request) { s.wechatBindingStart(w, r, true) })
	mux.HandleFunc("POST "+wechatLoginBase+"/confirm", s.wechatBindConfirm)
}

func wechatLoginHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func inWechatBrowser(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.UserAgent()), "micromessenger")
}

func wechatLoginToken() (string, error) {
	var value [32]byte
	_, err := rand.Read(value[:])
	return hex.EncodeToString(value[:]), err
}

func wechatHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validWechatLoginToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (s *Server) newWechatLoginFlow(ctx context.Context, flow db.WechatLoginFlow) (string, error) {
	state, err := wechatLoginToken()
	if err != nil {
		return "", err
	}
	flow.AppID = s.wechatLogin.AppID()
	err = s.store.CreateWechatLoginFlow(ctx, wechatHash(state), flow, time.Now().Add(wechatLoginTTL))
	return state, err
}

func wechatLoginRedirect(w http.ResponseWriter, r *http.Request, target, result string) {
	u, _ := url.Parse(target)
	query := u.Query()
	query.Set("wechat_login", result)
	u.RawQuery = query.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func wechatLoginDestination(value string) string {
	if validWechatReturnPath(value) {
		return value
	}
	return "/"
}

func (s *Server) wechatLoginStatus(w http.ResponseWriter, r *http.Request) {
	wechatLoginHeaders(w)
	result := map[string]bool{"enabled": s.wechatLogin != nil, "bound": false}
	if s.wechatLogin != nil && s.auth.SessionKey(r) != "" {
		actor, err := s.auth.Resolve(r)
		if err == nil && actor.Role == "customer" && actor.TenantID != nil {
			bound, verified, err := s.store.WechatAccountSecurity(r.Context(), s.wechatLogin.AppID(), s.auth.SessionKey(r), actor.UserID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "暂时无法读取微信绑定状态")
				return
			}
			result["bound"] = bound
			result["password_change_allowed"] = !bound || verified
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) wechatBrowserAuthorization(w http.ResponseWriter, r *http.Request, flow db.WechatLoginFlow) (string, error) {
	browser, err := wechatLoginToken()
	if err != nil {
		return "", err
	}
	flow.BrowserHash = wechatHash(browser)
	state, err := s.newWechatLoginFlow(r.Context(), flow)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: wechatLoginCookie, Value: browser, Path: wechatLoginBase,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(wechatLoginTTL.Seconds())})
	return s.wechatLogin.OAuthURL(state), nil
}

func (s *Server) wechatLoginStart(w http.ResponseWriter, r *http.Request) {
	wechatLoginHeaders(w)
	if s.wechatLogin == nil || !inWechatBrowser(r) {
		wechatLoginRedirect(w, r, "/login", "manual")
		return
	}
	target := wechatLoginDestination(r.URL.Query().Get("return"))
	if s.auth.SessionKey(r) != "" {
		actor, err := s.auth.Resolve(r)
		if err == nil && actor.Role == "customer" && actor.TenantID != nil {
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		if !errors.Is(err, auth.ErrNotAuthenticated) {
			wechatLoginRedirect(w, r, "/login", "manual")
			return
		}
	}
	destination, err := s.wechatBrowserAuthorization(w, r, db.WechatLoginFlow{Purpose: "login", Target: target})
	if err != nil {
		wechatLoginRedirect(w, r, "/login", "failed")
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (s *Server) wechatScanStart(w http.ResponseWriter, r *http.Request) {
	wechatLoginHeaders(w)
	token := r.URL.Query().Get("ticket")
	if s.wechatLogin == nil || !inWechatBrowser(r) || !validWechatLoginToken(token) {
		writeWechatScanPage(w, "", "请用微信扫描网页版个人中心的授权二维码。")
		return
	}
	parentHash := wechatHash(token)
	exists, err := s.store.WechatDesktopFlowExists(r.Context(), parentHash, s.wechatLogin.AppID())
	if err != nil || !exists {
		writeWechatScanPage(w, "", "二维码已失效，请回到电脑刷新二维码。")
		return
	}
	destination, err := s.wechatBrowserAuthorization(w, r, db.WechatLoginFlow{Purpose: "scan", Target: parentHash})
	if err != nil {
		writeWechatScanPage(w, "", "微信授权暂未完成，请重新扫码。")
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (s *Server) wechatBindingActor(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	wechatLoginHeaders(w)
	base, err := url.Parse(s.publicWebURL)
	origin := r.Header.Get("Origin")
	if err != nil || !strings.EqualFold(origin, base.Scheme+"://"+base.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, http.StatusForbidden, "微信授权请求来源无效")
		return model.Actor{}, false
	}
	if s.wechatLogin == nil {
		writeError(w, http.StatusServiceUnavailable, "微信授权暂未开通")
		return model.Actor{}, false
	}
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	if actor.Role != "customer" || actor.TenantID == nil || actor.MustChangePassword || s.auth.SessionKey(r) == "" {
		writeError(w, http.StatusForbidden, "请先使用个人账号登录并完成密码设置")
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) wechatBindingStart(w http.ResponseWriter, r *http.Request, unlink bool) {
	actor, ok := s.wechatBindingActor(w, r)
	if !ok {
		return
	}
	bound, err := s.store.HasWechatLogin(r.Context(), s.wechatLogin.AppID(), actor.UserID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "暂时无法读取微信绑定状态")
		return
	}
	if unlink && !bound {
		writeError(w, http.StatusConflict, "此账号尚未绑定微信")
		return
	}
	if !unlink && bound {
		writeError(w, http.StatusConflict, "此账号已绑定微信，请用原微信解除授权后再绑定")
		return
	}
	mobilePurpose, desktopPurpose := "mobile", "desktop"
	if unlink {
		mobilePurpose, desktopPurpose = "mobile_unbind", "desktop_unbind"
	}
	sessionHash := s.auth.SessionKey(r)
	if inWechatBrowser(r) {
		destination, err := s.wechatBrowserAuthorization(w, r, db.WechatLoginFlow{
			Purpose: mobilePurpose, UserID: actor.UserID, Target: sessionHash,
		})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "暂时无法发起微信授权")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"authorize_url": destination})
		return
	}
	token, err := s.newWechatLoginFlow(r.Context(), db.WechatLoginFlow{Purpose: desktopPurpose, UserID: actor.UserID, BrowserHash: sessionHash})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "暂时无法生成授权二维码")
		return
	}
	scan := s.publicWebURL + wechatLoginBase + "/scan?ticket=" + token
	png, err := qrcode.Encode(scan, qrcode.Medium, 256)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "暂时无法生成授权二维码")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"ticket":           token,
		"qr_code_data_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	})
}

func (s *Server) wechatBindConfirm(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.wechatBindingActor(w, r)
	if !ok {
		return
	}
	var input struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if readJSON(w, r, &input) != nil || !validWechatLoginToken(input.Ticket) ||
		len(input.Code) != 6 || strings.Trim(input.Code, "0123456789") != "" {
		writeError(w, http.StatusBadRequest, "请输入微信页面显示的 6 位确认码")
		return
	}
	ticket := wechatHash(input.Ticket)
	bound, err := s.store.ConfirmWechatDesktopFlow(r.Context(), ticket, s.wechatLogin.AppID(), s.auth.SessionKey(r),
		wechatHash(ticket+input.Code), actor.UserID)
	switch {
	case err == nil:
		if !bound {
			_ = s.auth.Logout(r.Context(), w, r)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"bound": bound})
	case errors.Is(err, db.ErrWechatOriginalRequired):
		writeError(w, http.StatusForbidden, "请用原绑定的微信解除授权")
	case errors.Is(err, db.ErrWechatLoginConflict):
		writeError(w, http.StatusConflict, "微信或小蓝搭子账号已与其他账号绑定")
	case errors.Is(err, db.ErrWechatPairingCode):
		writeError(w, http.StatusBadRequest, "确认码不正确")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusBadRequest, "请先扫码获取确认码。已过期时请刷新二维码。")
	default:
		writeError(w, http.StatusServiceUnavailable, "绑定失败，请稍后重试")
	}
}

func (s *Server) wechatLoginCallback(w http.ResponseWriter, r *http.Request) {
	wechatLoginHeaders(w)
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(wechatLoginCookie)
	if s.wechatLogin == nil || !inWechatBrowser(r) || err != nil ||
		!validWechatLoginToken(state) || !validWechatLoginToken(cookie.Value) {
		wechatLoginRedirect(w, r, "/login", "failed")
		return
	}
	flow, err := s.store.ConsumeWechatLoginFlow(r.Context(), wechatHash(state), wechatHash(cookie.Value))
	if err != nil || flow.AppID != s.wechatLogin.AppID() {
		wechatLoginRedirect(w, r, "/login", "failed")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: wechatLoginCookie, Path: wechatLoginBase,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})

	openID, exchangeError := s.wechatLogin.ExchangeOAuthCode(r.Context(), r.URL.Query().Get("code"))
	if exchangeError == nil && (len(openID) < 1 || len(openID) > 128) {
		exchangeError = errors.New("invalid wechat identity")
	}
	identity := wechatHash(openID)
	switch flow.Purpose {
	case "mobile", "mobile_unbind":
		result := "failed"
		if exchangeError == nil && s.auth.SessionKey(r) == flow.Target {
			unlink := flow.Purpose == "mobile_unbind"
			err := s.store.UpdateWechatBinding(r.Context(), flow.AppID, identity, flow.Target, flow.UserID, unlink)
			if err == nil && unlink {
				_ = s.auth.Logout(r.Context(), w, r)
				wechatLoginRedirect(w, r, "/login", "unbound_confirmed")
				return
			}
			if err == nil {
				result = "bound"
			}
			if errors.Is(err, db.ErrWechatLoginConflict) {
				result = "conflict"
			}
			if errors.Is(err, db.ErrWechatOriginalRequired) {
				result = "original_required"
			}
		}
		wechatLoginRedirect(w, r, "/me", result)
	case "scan":
		if exchangeError == nil {
			number, err := rand.Int(rand.Reader, big.NewInt(1000000))
			if err == nil {
				code := fmt.Sprintf("%06d", number.Int64())
				err = s.store.SetWechatDesktopCandidate(r.Context(), flow.Target, flow.AppID, identity, wechatHash(flow.Target+code))
				if err == nil {
					writeWechatScanPage(w, code, "将此确认码输入电脑上的个人中心，完成微信授权操作。")
					return
				}
			}
		}
		writeWechatScanPage(w, "", "授权未完成。解除授权时请用原绑定微信扫码。")
	case "login":
		target := wechatLoginDestination(flow.Target)
		if s.auth.SessionKey(r) != "" {
			actor, err := s.auth.Resolve(r)
			if err == nil && actor.Role == "customer" && actor.TenantID != nil {
				http.Redirect(w, r, target, http.StatusSeeOther)
				return
			}
			if !errors.Is(err, auth.ErrNotAuthenticated) {
				wechatLoginRedirect(w, r, "/login", "manual")
				return
			}
		}
		if exchangeError == nil {
			id, err := s.store.WechatLoginUser(r.Context(), flow.AppID, identity)
			if errors.Is(err, sql.ErrNoRows) {
				wechatLoginRedirect(w, r, "/login", "unbound")
				return
			}
			if err == nil && s.auth.LoginWechatCustomer(r.Context(), w, r, id, flow.AppID, identity) == nil {
				http.Redirect(w, r, target, http.StatusSeeOther)
				return
			}
		}
		wechatLoginRedirect(w, r, "/login", "failed")
	}
}

func writeWechatScanPage(w http.ResponseWriter, code, message string) {
	// All callers supply fixed messages and, if present, a server-generated numeric code.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>微信授权 · 小蓝搭子</title>
    <style>body{margin:0;background:#f1f4fc;color:#232e49;font:16px/1.6 system-ui,sans-serif}main{max-width:26rem;margin:12vh auto;padding:1.5rem;background:#fff;border-radius:20px}h1{font-size:1.4rem}strong{font-size:2.5rem;color:#4355bf;letter-spacing:.16em}a{color:#4355bf}@media(max-width:32rem){main{margin:12vh 1rem}}</style></head><body>
    <main><h1>微信授权</h1><p>%s</p><strong>%s</strong><p><a href="/">打开小蓝搭子</a></p></main></body></html>`, message, code)
}
