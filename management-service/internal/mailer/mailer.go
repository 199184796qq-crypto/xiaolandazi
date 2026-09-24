package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("smtp not configured")

type Config struct {
	Host        string
	Port        string
	Username    string
	Password    string
	FromEmail   string
	FromName    string
	TLSMode     string
	DialTimeout time.Duration
}

type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	if cfg.FromName == "" {
		cfg.FromName = "伴播搭子"
	}
	if cfg.TLSMode == "" {
		cfg.TLSMode = "starttls"
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 8 * time.Second
	}
	return &Client{cfg: cfg}
}

func (c *Client) Configured() bool {
	return c != nil &&
		strings.TrimSpace(c.cfg.Host) != "" &&
		strings.TrimSpace(c.cfg.FromEmail) != ""
}

func (c *Client) SendInitialCredential(
	to string,
	displayName string,
	username string,
	password string,
	loginURL string,
) error {
	if !c.Configured() {
		return ErrNotConfigured
	}

	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("recipient email is empty")
	}

	subject := "伴播搭子账号开通通知"
	body := fmt.Sprintf(
		"%s，您好：\r\n\r\n您的伴播搭子账号已开通。\r\n\r\n登录账号：%s\r\n初始密码：%s\r\n登录地址：%s\r\n\r\n首次登录后系统会要求您立即修改初始密码，请勿将初始密码转发给无关人员。\r\n\r\n伴播搭子\r\n",
		strings.TrimSpace(displayName),
		strings.TrimSpace(username),
		password,
		strings.TrimSpace(loginURL),
	)

	fromHeader := c.cfg.FromEmail
	if strings.TrimSpace(c.cfg.FromName) != "" {
		fromHeader = fmt.Sprintf("%s <%s>", c.cfg.FromName, c.cfg.FromEmail)
	}

	message := []byte(
		"From: " + fromHeader + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" +
			body,
	)

	address := net.JoinHostPort(c.cfg.Host, c.cfg.Port)
	var conn net.Conn
	var err error

	switch strings.ToLower(strings.TrimSpace(c.cfg.TLSMode)) {
	case "tls", "implicit_tls", "smtps":
		conn, err = tls.DialWithDialer(
			&net.Dialer{Timeout: c.cfg.DialTimeout},
			"tcp",
			address,
			&tls.Config{
				ServerName: c.cfg.Host,
				MinVersion: tls.VersionTLS12,
			},
		)
	default:
		conn, err = net.DialTimeout("tcp", address, c.cfg.DialTimeout)
	}
	if err != nil {
		return fmt.Errorf("connect smtp: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(c.cfg.DialTimeout)); err != nil {
		return err
	}

	client, err := smtp.NewClient(conn, c.cfg.Host)
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}
	defer client.Close()

	if strings.EqualFold(strings.TrimSpace(c.cfg.TLSMode), "starttls") {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return fmt.Errorf("smtp server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{
			ServerName: c.cfg.Host,
			MinVersion: tls.VersionTLS12,
		}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}

	if strings.TrimSpace(c.cfg.Username) != "" {
		auth := smtp.PlainAuth(
			"",
			c.cfg.Username,
			c.cfg.Password,
			c.cfg.Host,
		)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(c.cfg.FromEmail); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp recipient: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}
