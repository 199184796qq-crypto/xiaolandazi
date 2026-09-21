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
	CoreBaseURL   string
	CoreToken     string
	DevTenantCode string
	DevTenantName string
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
		CoreBaseURL:   envOrDefault("CORE_BASE_URL", "http://127.0.0.1:8081"),
		CoreToken:     envOrDefault("CORE_INTERNAL_TOKEN", "local-core-dev-token"),
		DevTenantCode: envOrDefault("DEV_TENANT_CODE", "demo"),
		DevTenantName: envOrDefault("DEV_TENANT_NAME", "演示客户"),
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
