package httpapi

import (
	"net/mail"
	"strings"

	"livecompanion/management/internal/mailer"
)

type credentialDeliveryResponse struct {
	InitialPassword string `json:"initial_password"`
	LoginURL        string `json:"login_url"`
	DeliveryMethod  string `json:"delivery_method"`
	Email           string `json:"email,omitempty"`
	EmailSent       bool   `json:"email_sent"`
	EmailError      string `json:"email_error,omitempty"`
}

func normalizeDeliveryMethod(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "email") {
		return "email"
	}
	return "copy"
}

func normalizeCredentialEmail(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || !strings.EqualFold(parsed.Address, value) {
		return "", false
	}
	return value, true
}

func (s *Server) deliverInitialCredential(
	method string,
	email string,
	displayName string,
	username string,
	password string,
	portals ...string,
) credentialDeliveryResponse {
	method = normalizeDeliveryMethod(method)
	portal := "user"
	if len(portals) > 0 && strings.EqualFold(strings.TrimSpace(portals[0]), "internal") {
		portal = "internal"
	}
	result := credentialDeliveryResponse{
		InitialPassword: password,
		LoginURL:        strings.TrimRight(s.publicWebURL, "/") + "/login?portal=" + portal,
		DeliveryMethod:  method,
		Email:           strings.TrimSpace(email),
	}

	if method != "email" {
		return result
	}

	if result.Email == "" {
		result.EmailError = "未填写邮箱，请使用复制方式交付登录凭证"
		return result
	}
	if s.mailer == nil || !s.mailer.Configured() {
		result.EmailError = "邮件服务尚未配置，请使用复制方式交付登录凭证"
		return result
	}

	if err := s.mailer.SendInitialCredential(
		result.Email,
		displayName,
		username,
		password,
		result.LoginURL,
	); err != nil {
		if err == mailer.ErrNotConfigured {
			result.EmailError = "邮件服务尚未配置，请使用复制方式交付登录凭证"
		} else {
			result.EmailError = "邮件发送失败，请使用复制方式交付登录凭证"
		}
		return result
	}

	result.EmailSent = true
	return result
}
