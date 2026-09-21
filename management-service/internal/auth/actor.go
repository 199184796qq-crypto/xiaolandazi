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
	ErrNotAuthenticated   = errors.New("not authenticated")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrCaptchaInvalid     = errors.New("captcha invalid")
	ErrRateLimited        = errors.New("too many login attempts")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrInvalidUsername    = errors.New("invalid username")
	ErrInvalidDisplayName = errors.New("invalid display name")
	ErrWeakPassword       = errors.New("weak password")
	ErrCurrentPassword    = errors.New("current password incorrect")
)

var usernamePattern = regexp.MustCompile("^[A-Za-z0-9_.-]{4,32}$")

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
	env   string
	store *appdb.Store

	captchaMu sync.Mutex
	captchas  map[string]captchaChallenge

	attemptMu sync.Mutex
	attempts  map[string]loginAttempt
}

func NewResolver(env string, store *appdb.Store) *Resolver {
	return &Resolver{
		env:      env,
		store:    store,
		captchas: make(map[string]captchaChallenge),
		attempts: make(map[string]loginAttempt),
	}
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
	username = strings.TrimSpace(username)
	key := loginKey(req, username)

	if err := r.checkRateLimit(key); err != nil {
		return model.Actor{}, err
	}
	if !r.verifyCaptcha(req, captcha) {
		r.recordFailure(key)
		return model.Actor{}, ErrCaptchaInvalid
	}

	user, err := r.store.GetUserByUsername(ctx, username)
	if err != nil || user.Status != "active" || user.PasswordHash == "" {
		r.recordFailure(key)
		return model.Actor{}, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(password),
	) != nil {
		r.recordFailure(key)
		return model.Actor{}, ErrInvalidCredentials
	}

	r.resetFailures(key)
	if err := r.issueSession(ctx, w, user.ID); err != nil {
		return model.Actor{}, err
	}

	return actorFromUser(user), nil
}

func (r *Resolver) Register(
	ctx context.Context,
	w http.ResponseWriter,
	req *http.Request,
	username string,
	displayName string,
	password string,
	captcha string,
) (model.Actor, error) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)

	if !r.verifyCaptcha(req, captcha) {
		return model.Actor{}, ErrCaptchaInvalid
	}
	if !usernamePattern.MatchString(username) {
		return model.Actor{}, ErrInvalidUsername
	}
	if len([]rune(displayName)) < 2 || len([]rune(displayName)) > 64 {
		return model.Actor{}, ErrInvalidDisplayName
	}
	if err := validatePassword(password); err != nil {
		return model.Actor{}, err
	}

	if _, err := r.store.GetUserByUsername(ctx, username); err == nil {
		return model.Actor{}, ErrUsernameTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.Actor{}, err
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return model.Actor{}, err
	}

	user, err := r.store.RegisterCustomer(
		ctx,
		username,
		displayName,
		passwordHash,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate:") {
			return model.Actor{}, ErrUsernameTaken
		}
		return model.Actor{}, err
	}

	if err := r.issueSession(ctx, w, user.ID); err != nil {
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
	return r.issueSession(ctx, w, user.ID)
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
	return r.env != "development"
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
		UserID:      user.ID,
		Username:    user.Username,
		Role:        user.Role,
		TenantID:    user.TenantID,
		DisplayName: user.DisplayName,
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
