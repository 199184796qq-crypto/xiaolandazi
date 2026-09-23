package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr          string
	Env           string
	DBHost        string
	DBPort        string
	DBName        string
	DBUser        string
	DBPassword    string
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int
	AuditLogLimit int
	AvatarDir     string
	CoreBaseURL   string
	CoreToken     string
	DevTenantCode string
	DevTenantName string
	PublicWebURL  string
	SMTPHost      string
	SMTPPort      string
	SMTPUsername  string
	SMTPPassword  string
	SMTPFromEmail string
	SMTPFromName  string
	SMTPTLSMode   string
}

func Load() Config {
	return Config{
		Addr:          envOrDefault("MGMT_ADDR", "127.0.0.1:8080"),
		Env:           envOrDefault("APP_ENV", "development"),
		DBHost:        envOrDefault("DB_HOST", "127.0.0.1"),
		DBPort:        envOrDefault("DB_PORT", "3306"),
		DBName:        envOrDefault("DB_NAME", "livecompanion"),
		DBUser:        envOrDefault("DB_USER", "livecompanion"),
		DBPassword:    envOrDefault("DB_PASSWORD", "livecompanion-dev"),
		RedisHost:     envOrDefault("REDIS_HOST", "127.0.0.1"),
		RedisPort:     envOrDefault("REDIS_PORT", "6379"),
		RedisPassword: strings.TrimSpace(os.Getenv("REDIS_PASSWORD")),
		RedisDB:       envInt("REDIS_DB", 0),
		AuditLogLimit: envInt("MGMT_AUDIT_LOG_LIMIT", 10000),
		AvatarDir:     envOrDefault("MGMT_AVATAR_DIR", "data/avatars"),
		CoreBaseURL:   envOrDefault("CORE_BASE_URL", "http://127.0.0.1:8081"),
		CoreToken:     envOrDefault("CORE_INTERNAL_TOKEN", "local-core-dev-token"),
		DevTenantCode: envOrDefault("DEV_TENANT_CODE", "demo"),
		DevTenantName: envOrDefault("DEV_TENANT_NAME", "演示终端"),
		PublicWebURL:  envOrDefault("PUBLIC_WEB_BASE_URL", "http://127.0.0.1:5173"),
		SMTPHost:      strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:      envOrDefault("SMTP_PORT", "587"),
		SMTPUsername:  strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		SMTPPassword:  os.Getenv("SMTP_PASSWORD"),
		SMTPFromEmail: strings.TrimSpace(os.Getenv("SMTP_FROM_EMAIL")),
		SMTPFromName:  envOrDefault("SMTP_FROM_NAME", "伴播搭子"),
		SMTPTLSMode:   envOrDefault("SMTP_TLS_MODE", "starttls"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
