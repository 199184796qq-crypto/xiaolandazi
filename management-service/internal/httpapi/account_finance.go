package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const maxAvatarBytes = 2 * 1024 * 1024

type customerRechargeRequest struct {
	AmountCents uint64 `json:"amount_cents"`
	Reason      string `json:"reason"`
}

func (s *Server) accountDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	item, err := s.store.GetAccountDashboard(r.Context(), actor.UserID, actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取账户信息失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) accountUpdateProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	var input model.AccountProfileChanges
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if input.Username != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	if actor.IsInternalStaff() {
		// Employee names are maintained by the organization.
		input.DisplayName = nil
	}
	s.updateOwnAccountProfile(w, r, actor.UserID, input)
}

func (s *Server) updateOwnAccountProfile(w http.ResponseWriter, r *http.Request, userID int64, input model.AccountProfileChanges) {
	if message := normalizeAccountProfileChanges(input); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	profile, err := s.store.UpdateAccountProfile(r.Context(), userID, input)
	if err != nil {
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 &&
			(strings.Contains(duplicate.Message, db.UsernameAccountIndex) ||
				strings.Contains(duplicate.Message, db.LegacyUsernameIndex)) {
			writeError(w, http.StatusConflict, "该登录账号已被使用")
			return
		}
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "该手机号已经绑定其他账号")
			return
		}
		writeError(w, http.StatusInternalServerError, "修改账户资料失败")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func normalizeAccountProfileChanges(input model.AccountProfileChanges) string {
	if input.Username != nil {
		*input.Username = strings.TrimSpace(*input.Username)
		if *input.Username == "" || utf8.RuneCountInString(*input.Username) > 128 ||
			strings.ContainsFunc(*input.Username, func(r rune) bool { return !unicode.IsGraphic(r) || unicode.IsSpace(r) }) {
			return "登录账号须为 1–128 个字符，且不能包含空格或不可见字符"
		}
	}
	fields := []struct {
		value *string
		limit int
	}{
		{input.DisplayName, 64}, {input.Phone, 64}, {input.Email, 254},
		{input.QQ, 32}, {input.Wechat, 64}, {input.Province, 64},
		{input.City, 64}, {input.District, 64}, {input.Address, 255},
	}
	for _, field := range fields {
		if field.value == nil {
			continue
		}
		*field.value = strings.TrimSpace(*field.value)
		if utf8.RuneCountInString(*field.value) > field.limit {
			return "账户资料字段长度超出限制"
		}
	}
	if input.DisplayName != nil && utf8.RuneCountInString(*input.DisplayName) < 2 {
		return "昵称需为 2-64 个字符"
	}
	if input.Phone != nil {
		normalized, ok := auth.NormalizeMainlandPhone(*input.Phone)
		if !ok {
			return "请输入正确的中国大陆手机号码"
		}
		*input.Phone = normalized
	}
	if input.Email != nil && *input.Email != "" {
		parsed, err := mail.ParseAddress(*input.Email)
		if err != nil || !strings.EqualFold(parsed.Address, *input.Email) {
			return "邮箱格式不正确"
		}
	}
	if input.Province != nil || input.City != nil || input.District != nil {
		for _, part := range []*string{input.Province, input.City, input.District} {
			if part == nil || *part == "" {
				return "请完整选择省、市和区县"
			}
		}
	}
	return ""
}

func (s *Server) accountUploadAvatar(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+64*1024)
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		writeError(w, http.StatusBadRequest, "头像文件过大或上传格式错误")
		return
	}

	file, _, err := r.FormFile("avatar")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择头像文件")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取头像文件失败")
		return
	}
	if len(data) == 0 || len(data) > maxAvatarBytes {
		writeError(w, http.StatusBadRequest, "头像大小需小于 2MB")
		return
	}

	contentType := http.DetectContentType(data)
	ext := ""
	switch contentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		writeError(w, http.StatusBadRequest, "头像仅支持 JPG、PNG 或 WebP")
		return
	}

	if err := os.MkdirAll(s.avatarDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "头像存储目录不可用")
		return
	}

	token, err := randomAvatarToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成头像文件名失败")
		return
	}
	filename := fmt.Sprintf("user-%d-%s%s", actor.UserID, token, ext)
	targetPath := filepath.Join(s.avatarDir, filename)

	oldProfile, _ := s.store.GetAccountProfile(r.Context(), actor.UserID)
	if err := os.WriteFile(targetPath, data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "保存头像失败")
		return
	}

	avatarURL := "/api/v1/account/avatar/" + filename
	profile, err := s.store.UpdateAccountAvatar(r.Context(), actor.UserID, avatarURL)
	if err != nil {
		_ = os.Remove(targetPath)
		writeError(w, http.StatusInternalServerError, "保存头像信息失败")
		return
	}

	s.removeOldAvatarFile(oldProfile.AvatarURL, filename)
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) accountAvatarFile(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimSpace(r.PathValue("file"))
	if filename == "" || filepath.Base(filename) != filename || !strings.HasPrefix(filename, "user-") {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".jpg" && ext != ".png" && ext != ".webp" {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(s.avatarDir, filename)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "读取头像失败")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, path)
}

func (s *Server) financeCreateRechargeRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可发起充值申请")
		return
	}

	var input customerRechargeRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.AmountCents == 0 {
		writeError(w, http.StatusBadRequest, "充值金额必须大于 0")
		return
	}
	if input.AmountCents > 100000000000 {
		writeError(w, http.StatusBadRequest, "充值金额超出允许范围")
		return
	}
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, "请填写充值申请说明")
		return
	}
	if utf8.RuneCountInString(input.Reason) > 512 {
		writeError(w, http.StatusBadRequest, "充值申请说明最多 512 个字符")
		return
	}

	result, err := s.store.CreateCustomerRechargeRequest(
		r.Context(),
		*actor.TenantID,
		input.AmountCents,
		input.Reason,
		actor.UserID,
	)
	if err != nil {
		writeStaffFinanceOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) financeDashboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "当前账号不能访问终端财务中心")
		return
	}
	if actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "当前账号没有终端财务账户")
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	item, err := s.store.GetFinanceDashboard(r.Context(), *actor.TenantID, limit)
	if err != nil {
		var sqlErr *mysql.MySQLError
		var code uint16
		if errors.As(err, &sqlErr) {
			code = sqlErr.Number
		}
		// Deliberately omit raw SQL/error payloads and customer/credential values.
		log.Printf("finance dashboard read unavailable: mysql_code=%d context_cancelled=%t", code, r.Context().Err() != nil)
		writeError(w, http.StatusInternalServerError, "读取财务信息失败")
		return
	}
	referralWallet, err := s.store.GetCustomerReferralWalletDashboard(r.Context(), *actor.TenantID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取返佣钱包失败")
		return
	}
	item.CommissionBalanceCents = referralWallet.Wallet.AvailableBalanceCents
	item.CommissionFrozenCents = referralWallet.Wallet.FrozenBalanceCents
	item.TotalBalanceCents += referralWallet.Wallet.AvailableBalanceCents
	beanWallet, err := s.store.CustomerBeanWallet(r.Context(), *actor.TenantID, 1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取小蓝豆余额失败")
		return
	}
	item.BeanBalance = beanWallet.Wallet.AvailableBeans
	item.BeanFrozen = beanWallet.Wallet.FrozenBeans
	writeJSON(w, http.StatusOK, item)
}

func randomAvatarToken() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (s *Server) removeOldAvatarFile(oldURL string, keepFilename string) {
	const prefix = "/api/v1/account/avatar/"
	if !strings.HasPrefix(oldURL, prefix) {
		return
	}
	filename := strings.TrimPrefix(oldURL, prefix)
	if filename == "" || filename == keepFilename || filepath.Base(filename) != filename {
		return
	}
	_ = os.Remove(filepath.Join(s.avatarDir, filename))
}

var _ model.AccountDashboard
