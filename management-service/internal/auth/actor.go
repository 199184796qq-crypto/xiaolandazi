package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const (
	sessionCookieName = "lc_session"
	captchaCookieName = "lc_captcha"
	sessionDuration   = 7 * 24 * time.Hour
	captchaDuration   = 5 * time.Minute
	maxLoginFailures  = 5
	loginWindow       = 10 * time.Minute
	loginBlock        = 15 * time.Minute
)

var (
	ErrNotAuthenticated       = errors.New("not authenticated")
	ErrInvalidCredentials     = errors.New("invalid credentials")
	ErrCaptchaInvalid         = errors.New("captcha invalid")
	ErrRateLimited            = errors.New("too many login attempts")
	ErrUsernameTaken          = errors.New("username already exists")
	ErrInvalidUsername        = errors.New("invalid username")
	ErrInvalidDisplayName     = errors.New("invalid display name")
	ErrInvalidPhone           = errors.New("invalid phone")
	ErrInvalidLocation        = errors.New("invalid location")
	ErrInvalidInviteCode      = errors.New("invalid invite code")
	ErrRegistrationCapacity   = errors.New("registration capacity insufficient")
	ErrWeakPassword           = errors.New("weak password")
	ErrCurrentPassword        = errors.New("current password incorrect")
	ErrUnsupportedLoginMethod = errors.New("unsupported login method")
	ErrPhoneNotRegistered     = errors.New("phone not registered")
	ErrPhoneTaken             = errors.New("phone already exists")
	ErrSMSCodeInvalid         = errors.New("sms code invalid")
	ErrSMSCodeExpired         = errors.New("sms code expired")
	ErrSMSUnavailable         = errors.New("sms provider unavailable")
	ErrSMSRateLimited         = errors.New("sms rate limited")
)

var usernamePattern = regexp.MustCompile("^[A-Za-z0-9_.-]{4,32}$")
var mainlandPhonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)

type LoginMethod string

const (
	LoginMethodPassword LoginMethod = "password"
	LoginMethodSMS      LoginMethod = "sms"
)

type LoginInput struct {
	Method     LoginMethod
	Identifier string
	Credential string
	Captcha    string
}

type SMSCodeDelivery struct {
	Phone             string `json:"phone"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	DebugCode         string `json:"debug_code,omitempty"`
}

type SMSProvider interface {
	SendLoginCode(context.Context, string, string) error
}

type developmentSMSProvider struct{}

func (developmentSMSProvider) SendLoginCode(context.Context, string, string) error { return nil }

type unavailableSMSProvider struct{}

func (unavailableSMSProvider) SendLoginCode(context.Context, string, string) error {
	return ErrSMSUnavailable
}

func NormalizeMainlandPhone(value string) (string, bool) {
	value = strings.TrimSpace(value)
	var digits strings.Builder
	for _, ch := range value {
		if ch >= '0' && ch <= '9' {
			digits.WriteRune(ch)
		}
	}
	normalized := digits.String()
	switch {
	case len(normalized) == 13 && strings.HasPrefix(normalized, "86"):
		normalized = normalized[2:]
	case len(normalized) == 15 && strings.HasPrefix(normalized, "0086"):
		normalized = normalized[4:]
	}
	return normalized, mainlandPhonePattern.MatchString(normalized)
}

type captchaChallenge struct {
	Code      string
	ExpiresAt time.Time
}

type loginAttempt struct {
	Failures     int
	WindowStart  time.Time
	BlockedUntil time.Time
}

type Resolver struct {
	env         string
	store       *appdb.Store
	smsProvider SMSProvider

	captchaMu sync.Mutex
	captchas  map[string]captchaChallenge

	attemptMu sync.Mutex
	attempts  map[string]loginAttempt
}

func NewResolver(env string, store *appdb.Store) *Resolver {
	var smsProvider SMSProvider = unavailableSMSProvider{}
	if env == "development" {
		smsProvider = developmentSMSProvider{}
	}
	return &Resolver{
		env:         env,
		store:       store,
		smsProvider: smsProvider,
		captchas:    make(map[string]captchaChallenge),
		attempts:    make(map[string]loginAttempt),
	}
}

func (r *Resolver) SetSMSProvider(provider SMSProvider) {
	if provider == nil {
		r.smsProvider = unavailableSMSProvider{}
		return
	}
	r.smsProvider = provider
}

func GenerateInitialPassword() (string, error) {
	const (
		length   = 14
		lower    = "abcdefghijkmnopqrstuvwxyz"
		upper    = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digits   = "23456789"
		symbols  = "!@#$%*+-_"
		alphabet = lower + upper + digits + symbols
	)

	result := make([]byte, 0, length)
	groups := []string{lower, upper, digits, symbols}

	for _, group := range groups {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(group))))
		if err != nil {
			return "", err
		}
		result = append(result, group[value.Int64()])
	}

	for len(result) < length {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		result = append(result, alphabet[value.Int64()])
	}

	for i := len(result) - 1; i > 0; i-- {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := int(value.Int64())
		result[i], result[j] = result[j], result[i]
	}

	return string(result), nil
}
func HashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	raw, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (r *Resolver) Resolve(req *http.Request) (model.Actor, error) {
	cookie, err := req.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return model.Actor{}, ErrNotAuthenticated
	}

	hash := hashToken(cookie.Value)
	user, err := r.store.ResolveSession(req.Context(), hash, time.Now().UTC())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Actor{}, ErrNotAuthenticated
		}
		return model.Actor{}, err
	}

	return actorFromUser(user), nil
}

func (r *Resolver) Login(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	username string,
	password string,
	captcha string,
) (model.Actor, error) {
	return r.Authenticate(ctx, w, req, LoginInput{
		Method:     LoginMethodPassword,
		Identifier: username,
		Credential: password,
		Captcha:    captcha,
	})
}

func (r *Resolver) Authenticate(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	input LoginInput,
) (model.Actor, error) {
	method := LoginMethod(strings.ToLower(strings.TrimSpace(string(input.Method))))
	identifier := strings.TrimSpace(input.Identifier)
	key := loginKey(req, string(method)+":"+identifier)
	if err := r.checkRateLimit(key); err != nil {
		return model.Actor{}, err
	}

	var (
		user model.User
		err  error
	)
	switch method {
	case LoginMethodPassword:
		if !r.verifyCaptcha(req, input.Captcha) {
			r.recordFailure(key)
			return model.Actor{}, ErrCaptchaInvalid
		}
		user, err = r.authenticatePassword(ctx, identifier, input.Credential)
	case LoginMethodSMS:
		user, err = r.authenticateSMS(ctx, identifier, input.Credential)
	default:
		return model.Actor{}, ErrUnsupportedLoginMethod
	}
	if err != nil {
		r.recordFailure(key)
		return model.Actor{}, err
	}
	if user.Status != "active" {
		r.recordFailure(key)
		return model.Actor{}, ErrInvalidCredentials
	}

	r.resetFailures(key)
	if err := r.issueSession(ctx, w, req, user.ID); err != nil {
		return model.Actor{}, err
	}
	return actorFromUser(user), nil
}

func (r *Resolver) authenticatePassword(
	ctx context.Context,
	username string,
	password string,
) (model.User, error) {
	user, err := r.store.GetUserByUsername(ctx, strings.TrimSpace(username))
	if err != nil || user.Status != "active" || user.PasswordHash == "" {
		return model.User{}, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return model.User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (r *Resolver) authenticateSMS(
	ctx context.Context,
	phone string,
	code string,
) (model.User, error) {
	normalizedPhone, ok := NormalizeMainlandPhone(phone)
	if !ok {
		return model.User{}, ErrInvalidPhone
	}
	user, err := r.store.GetUserByPhone(ctx, normalizedPhone)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrPhoneNotRegistered
		}
		return model.User{}, err
	}
	if user.Status != "active" {
		return model.User{}, ErrInvalidCredentials
	}

	challenge, err := r.store.GetLatestSMSLoginChallenge(ctx, normalizedPhone)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrSMSCodeInvalid
		}
		return model.User{}, err
	}
	if challenge.UsedAt.Valid || challenge.FailedAttempts >= 5 {
		return model.User{}, ErrSMSCodeInvalid
	}
	if time.Now().UTC().After(challenge.ExpiresAt) {
		return model.User{}, ErrSMSCodeExpired
	}
	if bcrypt.CompareHashAndPassword([]byte(challenge.CodeHash), []byte(strings.TrimSpace(code))) != nil {
		_ = r.store.IncrementSMSLoginChallengeFailure(ctx, challenge.ID)
		return model.User{}, ErrSMSCodeInvalid
	}
	if err := r.store.MarkSMSLoginChallengeUsed(ctx, challenge.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrSMSCodeInvalid
		}
		return model.User{}, err
	}
	return user, nil
}

func (r *Resolver) SendSMSLoginCode(
	ctx context.Context,
	req *http.Request,
	phone string,
) (SMSCodeDelivery, error) {
	normalizedPhone, ok := NormalizeMainlandPhone(phone)
	if !ok {
		return SMSCodeDelivery{}, ErrInvalidPhone
	}
	user, err := r.store.GetUserByPhone(ctx, normalizedPhone)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SMSCodeDelivery{}, ErrPhoneNotRegistered
		}
		return SMSCodeDelivery{}, err
	}
	if user.Status != "active" {
		return SMSCodeDelivery{}, ErrPhoneNotRegistered
	}

	if _, unavailable := r.smsProvider.(unavailableSMSProvider); unavailable {
		return SMSCodeDelivery{}, ErrSMSUnavailable
	}

	code, err := randomDigits(6)
	if err != nil {
		return SMSCodeDelivery{}, err
	}
	codeHash, err := bcrypt.GenerateFromPassword([]byte(code), 10)
	if err != nil {
		return SMSCodeDelivery{}, err
	}
	if err := r.store.CreateSMSLoginChallenge(
		ctx,
		normalizedPhone,
		string(codeHash),
		time.Now().UTC().Add(5*time.Minute),
		requestIP(req),
	); err != nil {
		if errors.Is(err, appdb.ErrSMSChallengeRateLimited) {
			return SMSCodeDelivery{}, ErrSMSRateLimited
		}
		return SMSCodeDelivery{}, err
	}
	if err := r.smsProvider.SendLoginCode(ctx, normalizedPhone, code); err != nil {
		if challenge, loadErr := r.store.GetLatestSMSLoginChallenge(ctx, normalizedPhone); loadErr == nil {
			_ = r.store.MarkSMSLoginChallengeUsed(ctx, challenge.ID)
		}
		return SMSCodeDelivery{}, err
	}

	result := SMSCodeDelivery{
		Phone:             normalizedPhone,
		RetryAfterSeconds: 60,
	}
	if r.env == "development" {
		result.DebugCode = code
	}
	return result, nil
}

func (r *Resolver) Register(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	username string,
	displayName string,
	phone string,
	province string,
	city string,
	district string,
	password string,
	inviteCode string,
	captcha string,
) (model.Actor, error) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = username
	}
	phone = strings.TrimSpace(phone)
	province = strings.TrimSpace(province)
	city = strings.TrimSpace(city)
	district = strings.TrimSpace(district)
	inviteCode = strings.TrimSpace(inviteCode)

	if !r.verifyCaptcha(req, captcha) {
		return model.Actor{}, ErrCaptchaInvalid
	}
	if !usernamePattern.MatchString(username) {
		return model.Actor{}, ErrInvalidUsername
	}
	if len([]rune(displayName)) < 2 || len([]rune(displayName)) > 64 {
		return model.Actor{}, ErrInvalidDisplayName
	}
	normalizedPhone, phoneOK := NormalizeMainlandPhone(phone)
	if !phoneOK {
		return model.Actor{}, ErrInvalidPhone
	}
	phone = normalizedPhone
	if inviteCode == "" {
		return model.Actor{}, ErrInvalidInviteCode
	}
	if err := validatePassword(password); err != nil {
		return model.Actor{}, err
	}

	if _, err := r.store.GetUserByUsername(ctx, username); err == nil {
		return model.Actor{}, ErrUsernameTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.Actor{}, err
	}
	if _, err := r.store.GetUserByPhone(ctx, phone); err == nil {
		return model.Actor{}, ErrPhoneTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.Actor{}, err
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return model.Actor{}, err
	}

	user, err := r.store.RegisterCustomerByInvite(
		ctx,
		username,
		displayName,
		phone,
		province,
		city,
		district,
		passwordHash,
		inviteCode,
	)
	if err != nil {
		if strings.Contains(err.Error(), "uk_mgmt_users_phone_unique") {
			return model.Actor{}, ErrPhoneTaken
		}
		if strings.Contains(err.Error(), "duplicate:") {
			return model.Actor{}, ErrUsernameTaken
		}
		if errors.Is(err, appdb.ErrInviteCodeInvalid) {
			return model.Actor{}, ErrInvalidInviteCode
		}
		if errors.Is(err, appdb.ErrInsufficientResource) {
			return model.Actor{}, ErrRegistrationCapacity
		}
		return model.Actor{}, err
	}

	if err := r.issueSession(ctx, w, req, user.ID); err != nil {
		return model.Actor{}, err
	}
	return actorFromUser(user), nil
}

func (r *Resolver) Logout(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
) error {
	if cookie, err := req.Cookie(sessionCookieName); err == nil &&
		strings.TrimSpace(cookie.Value) != "" {
		_ = r.store.DeleteSession(ctx, hashToken(cookie.Value))
	}
	r.clearSessionCookie(w)
	return nil
}

func (r *Resolver) ChangePassword(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	currentPassword string,
	newPassword string,
) error {
	actor, err := r.Resolve(req)
	if err != nil {
		return err
	}

	user, err := r.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(currentPassword),
	) != nil {
		return ErrCurrentPassword
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	if currentPassword == newPassword {
		return errors.New("new password must be different")
	}

	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := r.store.UpdatePassword(ctx, user.ID, passwordHash); err != nil {
		return err
	}

	if err := r.store.DeleteUserSessions(ctx, user.ID); err != nil {
		return err
	}
	return r.issueSession(ctx, w, req, user.ID)
}

func (r *Resolver) ListSessions(ctx context.Context, req *http.Request) ([]model.AuthSessionSummary, error) {
	actor, err := r.Resolve(req)
	if err != nil {
		return nil, err
	}
	cookie, err := req.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return nil, ErrNotAuthenticated
	}
	return r.store.ListUserSessions(ctx, actor.UserID, hashToken(cookie.Value))
}

func (r *Resolver) LogoutOtherSessions(ctx context.Context, req *http.Request) error {
	actor, err := r.Resolve(req)
	if err != nil {
		return err
	}
	cookie, err := req.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return ErrNotAuthenticated
	}
	return r.store.DeleteOtherUserSessions(ctx, actor.UserID, hashToken(cookie.Value))
}

func (r *Resolver) Captcha(
	w http.ResponseWriter,
	_ *http.Request,
) error {
	code, err := randomDigits(5)
	if err != nil {
		return err
	}
	id, err := randomToken(18)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	r.captchaMu.Lock()
	for key, challenge := range r.captchas {
		if now.After(challenge.ExpiresAt) {
			delete(r.captchas, key)
		}
	}
	r.captchas[id] = captchaChallenge{
		Code:      code,
		ExpiresAt: now.Add(captchaDuration),
	}
	r.captchaMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     captchaCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.secureCookie(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(captchaDuration.Seconds()),
		Expires:  now.Add(captchaDuration),
	})

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	return renderCaptchaPNG(w, code)
}

func (r *Resolver) SetDevelopmentActor(
	context.Context,
	http.ResponseWriter,
	string,
	int64,
) error {
	return errors.New("development actor switching is disabled")
}

func (r *Resolver) issueSession(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	userID int64,
) error {
	token, err := randomToken(32)
	if err != nil {
		return err
	}
	expiresAt := time.Now().UTC().Add(sessionDuration)

	if err := r.store.CreateSession(
		ctx,
		userID,
		hashToken(token),
		expiresAt,
		requestIP(req),
		req.UserAgent(),
	); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.secureCookie(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
		Expires:  expiresAt,
	})
	return nil
}

func (r *Resolver) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.secureCookie(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (r *Resolver) secureCookie() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_COOKIE_SECURE"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return r.env != "development"
	}
}

func (r *Resolver) verifyCaptcha(
	req *http.Request,
	input string,
) bool {
	input = strings.TrimSpace(input)
	cookie, err := req.Cookie(captchaCookieName)
	if err != nil || input == "" {
		return false
	}

	r.captchaMu.Lock()
	challenge, ok := r.captchas[cookie.Value]
	delete(r.captchas, cookie.Value)
	r.captchaMu.Unlock()

	if !ok || time.Now().UTC().After(challenge.ExpiresAt) {
		return false
	}
	return subtle.ConstantTimeCompare(
		[]byte(challenge.Code),
		[]byte(input),
	) == 1
}

func (r *Resolver) checkRateLimit(key string) error {
	now := time.Now().UTC()

	r.attemptMu.Lock()
	defer r.attemptMu.Unlock()

	item, ok := r.attempts[key]
	if !ok {
		return nil
	}
	if now.Before(item.BlockedUntil) {
		return ErrRateLimited
	}
	if now.Sub(item.WindowStart) > loginWindow {
		delete(r.attempts, key)
	}
	return nil
}

func (r *Resolver) recordFailure(key string) {
	now := time.Now().UTC()

	r.attemptMu.Lock()
	defer r.attemptMu.Unlock()

	item := r.attempts[key]
	if item.WindowStart.IsZero() || now.Sub(item.WindowStart) > loginWindow {
		item = loginAttempt{
			WindowStart: now,
		}
	}
	item.Failures++
	if item.Failures >= maxLoginFailures {
		item.BlockedUntil = now.Add(loginBlock)
	}
	r.attempts[key] = item
}

func (r *Resolver) resetFailures(key string) {
	r.attemptMu.Lock()
	delete(r.attempts, key)
	r.attemptMu.Unlock()
}

func actorFromUser(user model.User) model.Actor {
	return model.Actor{
		UserID:             user.ID,
		Username:           user.Username,
		Role:               user.Role,
		MustChangePassword: user.MustChangePassword,
		Phone:              user.Phone,
		Province:           user.Province,
		City:               user.City,
		District:           user.District,
		TenantID:           user.TenantID,
		DisplayName:        user.DisplayName,
		AvatarURL:          user.AvatarURL,
	}
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return ErrWeakPassword
	}
	return nil
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func randomDigits(count int) (string, error) {
	var builder strings.Builder
	builder.Grow(count)

	for i := 0; i < count; i++ {
		value, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + value.Int64()))
	}
	return builder.String(), nil
}

func requestIP(req *http.Request) string {
	host := strings.TrimSpace(req.RemoteAddr)
	if parsedHost, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		host = parsedHost
	}
	return host
}

func loginKey(req *http.Request, username string) string {
	host := req.RemoteAddr
	if parsedHost, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		host = parsedHost
	}
	return strings.ToLower(strings.TrimSpace(username)) + "|" + host
}

func renderCaptchaPNG(
	w http.ResponseWriter,
	code string,
) error {
	const (
		width  = 190
		height = 64
	)

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(
		canvas,
		canvas.Bounds(),
		&image.Uniform{C: color.RGBA{R: 246, G: 248, B: 252, A: 255}},
		image.Point{},
		draw.Src,
	)

	for i := 0; i < 12; i++ {
		x1 := secureInt(width)
		y1 := secureInt(height)
		x2 := secureInt(width)
		y2 := secureInt(height)
		shade := uint8(170 + secureInt(60))
		drawLine(
			canvas,
			x1,
			y1,
			x2,
			y2,
			color.RGBA{R: shade, G: shade, B: 225, A: 110},
		)
	}

	for index, char := range code {
		x := 18 + index*33 + secureInt(5) - 2
		y := 14 + secureInt(5) - 2
		drawDigit(
			canvas,
			x,
			y,
			int(char-'0'),
			color.RGBA{
				R: uint8(38 + secureInt(35)),
				G: uint8(45 + secureInt(35)),
				B: uint8(85 + secureInt(55)),
				A: 255,
			},
		)
	}

	return png.Encode(w, canvas)
}

var digitSegments = [10][7]bool{
	{true, true, true, true, true, true, false},
	{false, true, true, false, false, false, false},
	{true, true, false, true, true, false, true},
	{true, true, true, true, false, false, true},
	{false, true, true, false, false, true, true},
	{true, false, true, true, false, true, true},
	{true, false, true, true, true, true, true},
	{true, true, true, false, false, false, false},
	{true, true, true, true, true, true, true},
	{true, true, true, true, false, true, true},
}

func drawDigit(
	img *image.RGBA,
	x int,
	y int,
	digit int,
	c color.RGBA,
) {
	if digit < 0 || digit > 9 {
		return
	}

	const (
		w = 19
		h = 34
		t = 4
	)

	segments := digitSegments[digit]
	rectangles := [7]image.Rectangle{
		image.Rect(x+t, y, x+w-t, y+t),
		image.Rect(x+w-t, y+t, x+w, y+h/2-t/2),
		image.Rect(x+w-t, y+h/2+t/2, x+w, y+h-t),
		image.Rect(x+t, y+h-t, x+w-t, y+h),
		image.Rect(x, y+h/2+t/2, x+t, y+h-t),
		image.Rect(x, y+t, x+t, y+h/2-t/2),
		image.Rect(x+t, y+h/2-t/2, x+w-t, y+h/2+t/2),
	}

	for i, enabled := range segments {
		if enabled {
			draw.Draw(
				img,
				rectangles[i],
				&image.Uniform{C: c},
				image.Point{},
				draw.Src,
			)
		}
	}
}

func drawLine(
	img *image.RGBA,
	x0 int,
	y0 int,
	x1 int,
	y1 int,
	c color.RGBA,
) {
	dx := abs(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -abs(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	errValue := dx + dy

	for {
		if image.Pt(x0, y0).In(img.Bounds()) {
			img.SetRGBA(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * errValue
		if e2 >= dy {
			errValue += dy
			x0 += sx
		}
		if e2 <= dx {
			errValue += dx
			y0 += sy
		}
	}
}

func secureInt(max int) int {
	if max <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(value.Int64())
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func PasswordPolicyText() string {
	return fmt.Sprintf("password length must be between %d and %d", 8, 72)
}
