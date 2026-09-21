package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr            string
	Env             string
	DBHost          string
	DBPort          string
	DBName          string
	DBUser          string
	DBPassword      string
	RedisHost       string
	RedisPort       string
	RedisPassword   string
	RedisDB         int
	EventCacheLimit int
	InternalToken   string
	BrowserPath     string
	BrowserHeadless bool
	FFmpegPath      string
	MediaCacheRoot  string
	LogFile         string
}

func Load() Config {
	return Config{
		Addr:            envOrDefault("CORE_ADDR", "127.0.0.1:8081"),
		Env:             envOrDefault("APP_ENV", "development"),
		DBHost:          envOrDefault("DB_HOST", "127.0.0.1"),
		DBPort:          envOrDefault("DB_PORT", "3306"),
		DBName:          envOrDefault("DB_NAME", "livecompanion"),
		DBUser:          envOrDefault("DB_USER", "livecompanion"),
		DBPassword:      envOrDefault("DB_PASSWORD", "livecompanion-dev"),
		RedisHost:       envOrDefault("REDIS_HOST", "127.0.0.1"),
		RedisPort:       envOrDefault("REDIS_PORT", "6379"),
		RedisPassword:   strings.TrimSpace(os.Getenv("REDIS_PASSWORD")),
		RedisDB:         envInt("REDIS_DB", 0),
		EventCacheLimit: envInt("PUBLIC_EVENT_CACHE_LIMIT", 500),
		InternalToken:   envOrDefault("CORE_INTERNAL_TOKEN", "local-core-dev-token"),
		BrowserPath:     strings.TrimSpace(os.Getenv("COLLECTOR_BROWSER_PATH")),
		BrowserHeadless: envBool("COLLECTOR_BROWSER_HEADLESS", true),
		FFmpegPath:      envOrDefault("FFMPEG_PATH", "ffmpeg"),
		MediaCacheRoot:  strings.TrimSpace(os.Getenv("MEDIA_CACHE_ROOT")),
		LogFile:         strings.TrimSpace(os.Getenv("CORE_LOG_FILE")),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
