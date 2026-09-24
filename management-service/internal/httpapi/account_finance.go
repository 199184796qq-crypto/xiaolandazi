package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

const maxAvatarBytes = 2 * 1024 * 1024

type updateAccountProfileRequest struct {
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	QQ          string `json:"qq"`
	Wechat      string `json:"wechat"`
	Province    string `json:"province"`
	City        string `json:"city"`
	District    string `json:"district"`
	Address     string `json:"address"`
}

type customerRechargeRequest struct {
	AmountCents uint64 `json:"amount_cents"`
	Reason      string `json:"reason"`
}

func (s *Server) accountDashboard(w http.ResponseWriter, r *http.Request) {
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
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}

	var input updateAccountProfileRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Phone = strings.TrimSpace(input.Phone)
	input.Email = strings.TrimSpace(input.Email)
	input.QQ = strings.TrimSpace(input.QQ)
	input.Wechat = strings.TrimSpace(input.Wechat)
	input.Province = strings.TrimSpace(input.Province)
	input.City = strings.TrimSpace(input.City)
	input.District = strings.TrimSpace(input.District)
	input.Address = strings.TrimSpace(input.Address)

	// Internal employee names are formal organization identities. They are maintained
	// from the organization structure rather than self-edited in Personal Center.
	if actor.IsInternalStaff() {
		currentProfile, err := s.store.GetAccountProfile(r.Context(), actor.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取员工正式姓名失败")
			return
		}
		input.DisplayName = currentProfile.DisplayName
	}

	nameLength := utf8.RuneCountInString(input.DisplayName)
	if nameLength < 2 || nameLength > 64 {
		writeError(w, http.StatusBadRequest, "账户名称需为 2-64 个字符")
		return
	}
	normalizedPhone, phoneOK := auth.NormalizeMainlandPhone(input.Phone)
	if !phoneOK {
		writeError(w, http.StatusBadRequest, "请输入正确的中国大陆手机号码")
		return
	}
	input.Phone = normalizedPhone
	if input.Province == "" || input.City == "" || input.District == "" {
		writeError(w, http.StatusBadRequest, "省、市、区/县不能为空")
		return
	}
	if input.Email != "" {
		parsed, err := mail.ParseAddress(input.Email)
		if err != nil || !strings.EqualFold(parsed.Address, input.Email) {
			writeError(w, http.StatusBadRequest, "邮箱格式不正确")
			return
		}
	}
	if !contactFieldLengthOK(input.Email, 254) ||
		!contactFieldLengthOK(input.QQ, 32) ||
		!contactFieldLengthOK(input.Wechat, 64) ||
		!contactFieldLengthOK(input.Province, 64) ||
		!contactFieldLengthOK(input.City, 64) ||
		!contactFieldLengthOK(input.District, 64) ||
		!contactFieldLengthOK(input.Address, 255) {
		writeError(w, http.StatusBadRequest, "联系资料字段长度超出限制")
		return
	}

	profile, err := s.store.UpdateAccountProfile(
		r.Context(),
		actor.UserID,
		input.DisplayName,
		input.Phone,
		input.Email,
		input.QQ,
		input.Wechat,
		input.Province,
		input.City,
		input.District,
		input.Address,
	)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "该手机号已经绑定其他账号")
			return
		}
		writeError(w, http.StatusInternalServerError, "修改账户资料失败")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func contactFieldLengthOK(value string, max int) bool {
	return utf8.RuneCountInString(value) <= max
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
		writeError(w, http.StatusInternalServerError, "读取财务信息失败")
		return
	}
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
