package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr                     string
	Env                      string
	DBHost                   string
	DBPort                   string
	DBName                   string
	DBUser                   string
	DBPassword               string
	DBMaxOpenConns           int
	DBMaxIdleConns           int
	RedisHost                string
	RedisPort                string
	RedisPassword            string
	RedisDB                  int
	EventCacheLimit          int
	ImportantEventCacheLimit int
	PublicEventLogEnabled    bool
	EventArchiveSpoolDir     string
	InternalToken            string
	CorePublicURL            string
	CoreAudioTestWAVPath     string
	BrowserPath              string
	BrowserHeadless          bool
	CollectorWorkers         int
	CollectorRoomsPerWorker  int
	ShardIndex               int
	ShardCount               int
	NodeID                   string
	RoomLeaseEnabled         bool
	RoomLeaseTTLSeconds      int
	RoomLeaseRenewSeconds    int
	RoomFailoverDelaySeconds int
	FFmpegPath               string
	MediaCacheRoot           string
	MediaMaxSessions         int
	CaptureRoot              string
	StorageDriver            string
	MediaRoot                string
	OSSEndpoint              string
	OSSBucket                string
	OSSAccessKeyID           string
	OSSAccessKeySecret       string
	LogFile                  string
}

func Load() Config {
	env := envOrDefault("APP_ENV", "development")
	return Config{
		Addr:                     envOrDefault("CORE_ADDR", "127.0.0.1:8081"),
		Env:                      env,
		DBHost:                   envOrDefault("DB_HOST", "127.0.0.1"),
		DBPort:                   envOrDefault("DB_PORT", "3306"),
		DBName:                   envOrDefault("DB_NAME", "livecompanion"),
		DBUser:                   envOrDefault("DB_USER", "livecompanion"),
		DBPassword:               envOrDefault("DB_PASSWORD", "livecompanion-dev"),
		DBMaxOpenConns:           envPositiveInt("CORE_DB_MAX_OPEN_CONNS", 40),
		DBMaxIdleConns:           envPositiveInt("CORE_DB_MAX_IDLE_CONNS", 10),
		RedisHost:                envOrDefault("REDIS_HOST", "127.0.0.1"),
		RedisPort:                envOrDefault("REDIS_PORT", "6379"),
		RedisPassword:            strings.TrimSpace(os.Getenv("REDIS_PASSWORD")),
		RedisDB:                  envInt("REDIS_DB", 0),
		EventCacheLimit:          envInt("PUBLIC_EVENT_CACHE_LIMIT", 500),
		ImportantEventCacheLimit: envPositiveInt("IMPORTANT_EVENT_CACHE_LIMIT", 20000),
		PublicEventLogEnabled:    envBool("PUBLIC_EVENT_LOG_ENABLED", env == "development"),
		EventArchiveSpoolDir:     envOrDefault("EVENT_ARCHIVE_SPOOL_DIR", "data/event-archive-spool"),
		InternalToken:            envOrDefault("CORE_INTERNAL_TOKEN", "local-core-dev-token"),
		CorePublicURL:            envOrDefault("CORE_PUBLIC_URL", "http://127.0.0.1:8081"),
		CoreAudioTestWAVPath:     envOrDefault("CORE_AUDIO_TEST_WAV_PATH", envOrDefault("AUDIO_TEST_WAV_PATH", `E:\直播伴播\测试素材\母带时间轴测试\mainline_same_tts.wav`)),
		BrowserPath:              strings.TrimSpace(os.Getenv("COLLECTOR_BROWSER_PATH")),
		BrowserHeadless:          envBool("COLLECTOR_BROWSER_HEADLESS", true),
		CollectorWorkers:         envPositiveInt("COLLECTOR_WORKERS", 5),
		CollectorRoomsPerWorker:  envPositiveInt("COLLECTOR_ROOMS_PER_WORKER", 20),
		ShardIndex:               envNonNegativeInt("CORE_SHARD_INDEX", 0),
		ShardCount:               envPositiveInt("CORE_SHARD_COUNT", 1),
		NodeID:                   envOrDefault("CORE_NODE_ID", defaultNodeID("core")),
		RoomLeaseEnabled:         envBool("CORE_ROOM_LEASE_ENABLED", true),
		RoomLeaseTTLSeconds:      envPositiveInt("CORE_ROOM_LEASE_TTL_SECONDS", 15),
		RoomLeaseRenewSeconds:    envPositiveInt("CORE_ROOM_LEASE_RENEW_SECONDS", 5),
		RoomFailoverDelaySeconds: envPositiveInt("CORE_ROOM_FAILOVER_DELAY_SECONDS", 3),
		FFmpegPath:               envOrDefault("FFMPEG_PATH", "ffmpeg"),
		MediaCacheRoot:           strings.TrimSpace(os.Getenv("MEDIA_CACHE_ROOT")),
		MediaMaxSessions:         envPositiveInt("MEDIA_MAX_SESSIONS", 8),
		CaptureRoot:              envOrDefault("CAPTURE_ROOT", "data/recordings"),
		StorageDriver:            strings.ToLower(envOrDefault("STORAGE_DRIVER", "local")),
		MediaRoot:                envOrDefault("MEDIA_ROOT", "data/media"),
		OSSEndpoint:              strings.TrimSpace(os.Getenv("OSS_ENDPOINT")),
		OSSBucket:                strings.TrimSpace(os.Getenv("OSS_BUCKET")),
		OSSAccessKeyID:           strings.TrimSpace(os.Getenv("OSS_ACCESS_KEY_ID")),
		OSSAccessKeySecret:       os.Getenv("OSS_ACCESS_KEY_SECRET"),
		LogFile:                  strings.TrimSpace(os.Getenv("CORE_LOG_FILE")),
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

func envPositiveInt(key string, fallback int) int {
	value := envInt(key, fallback)
	if value <= 0 {
		return fallback
	}
	return value
}

func envNonNegativeInt(key string, fallback int) int {
	value := envInt(key, fallback)
	if value < 0 {
		return fallback
	}
	return value
}
