package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr                      string
	Env                       string
	DBHost                    string
	DBPort                    string
	DBName                    string
	DBUser                    string
	DBPassword                string
	DBMaxOpenConns            int
	DBMaxIdleConns            int
	RedisHost                 string
	RedisPort                 string
	RedisPassword             string
	RedisDB                   int
	AuditLogLimit             int
	AvatarDir                 string
	StorageDriver             string
	MediaRoot                 string
	OSSEndpoint               string
	OSSPublicEndpoint         string
	OSSBucket                 string
	OSSAccessKeyID            string
	OSSAccessKeySecret        string
	OSSURLExpirySeconds       int
	CoreBaseURL               string
	CoreToken                 string
	RuntimeReconcileWorkers   int
	RuntimeReconcileBatch     int
	NodeID                    string
	ReconcileLeaderTTLSeconds int
	DevTenantCode             string
	DevTenantName             string
	PublicWebURL              string
	SMTPHost                  string
	SMTPPort                  string
	SMTPUsername              string
	SMTPPassword              string
	SMTPFromEmail             string
	SMTPFromName              string
	SMTPTLSMode               string
}

func Load() Config {
	return Config{
		Addr:                      envOrDefault("MGMT_ADDR", "127.0.0.1:8080"),
		Env:                       envOrDefault("APP_ENV", "development"),
		DBHost:                    envOrDefault("DB_HOST", "127.0.0.1"),
		DBPort:                    envOrDefault("DB_PORT", "3306"),
		DBName:                    envOrDefault("DB_NAME", "livecompanion"),
		DBUser:                    envOrDefault("DB_USER", "livecompanion"),
		DBPassword:                envOrDefault("DB_PASSWORD", "livecompanion-dev"),
		DBMaxOpenConns:            envPositiveInt("MGMT_DB_MAX_OPEN_CONNS", 30),
		DBMaxIdleConns:            envPositiveInt("MGMT_DB_MAX_IDLE_CONNS", 10),
		RedisHost:                 envOrDefault("REDIS_HOST", "127.0.0.1"),
		RedisPort:                 envOrDefault("REDIS_PORT", "6379"),
		RedisPassword:             strings.TrimSpace(os.Getenv("REDIS_PASSWORD")),
		RedisDB:                   envInt("REDIS_DB", 0),
		AuditLogLimit:             envInt("MGMT_AUDIT_LOG_LIMIT", 10000),
		AvatarDir:                 envOrDefault("MGMT_AVATAR_DIR", "data/avatars"),
		StorageDriver:             strings.ToLower(envOrDefault("STORAGE_DRIVER", "local")),
		MediaRoot:                 envOrDefault("MEDIA_ROOT", "data/media"),
		OSSEndpoint:               strings.TrimSpace(os.Getenv("OSS_ENDPOINT")),
		OSSPublicEndpoint:         strings.TrimSpace(os.Getenv("OSS_PUBLIC_ENDPOINT")),
		OSSBucket:                 strings.TrimSpace(os.Getenv("OSS_BUCKET")),
		OSSAccessKeyID:            strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_ID")),
		OSSAccessKeySecret:        os.Getenv("OSS_ACCESS_KEY_SECRET"),
		OSSURLExpirySeconds:       envPositiveInt("OSS_URL_EXPIRY_SECONDS", 900),
		CoreBaseURL:               envOrDefault("CORE_BASE_URLS", envOrDefault("CORE_BASE_URL", "http://127.0.0.1:8081")),
		CoreToken:                 envOrDefault("CORE_INTERNAL_TOKEN", "local-core-dev-token"),
		RuntimeReconcileWorkers:   envPositiveInt("LIVE_RUNTIME_RECONCILE_WORKERS", 16),
		RuntimeReconcileBatch:     envPositiveInt("LIVE_RUNTIME_RECONCILE_BATCH", 500),
		NodeID:                    envOrDefault("MGMT_NODE_ID", defaultNodeID("management")),
		ReconcileLeaderTTLSeconds: envPositiveInt("MGMT_RECONCILE_LEADER_TTL_SECONDS", 15),
		DevTenantCode:             envOrDefault("DEV_TENANT_CODE", "demo"),
		DevTenantName:             envOrDefault("DEV_TENANT_NAME", "演示终端"),
		PublicWebURL:              envOrDefault("PUBLIC_WEB_BASE_URL", "http://127.0.0.1:5173"),
		SMTPHost:                  strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:                  envOrDefault("SMTP_PORT", "587"),
		SMTPUsername:              strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		SMTPPassword:              os.Getenv("SMTP_PASSWORD"),
		SMTPFromEmail:             strings.TrimSpace(os.Getenv("SMTP_FROM_EMAIL")),
		SMTPFromName:              envOrDefault("SMTP_FROM_NAME", "伴播搭子"),
		SMTPTLSMode:               envOrDefault("SMTP_TLS_MODE", "starttls"),
	}
}

func defaultNodeID(prefix string) string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s-%s-%d", prefix, host, os.Getpid())
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

func envPositiveInt(key string, fallback int) int {
	value := envInt(key, fallback)
	if value <= 0 {
		return fallback
	}
	return value
}
