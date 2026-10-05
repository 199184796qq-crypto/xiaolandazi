package main

import (
	"os"
	"strings"
)

const (
	defaultAddr         = "127.0.0.1:8083"
	defaultPublicWSURL  = "wss://www.xiaolandaizi.cn/xiaozhi/v1/"
	defaultCoreBaseURL  = "http://127.0.0.1:8081"
	defaultBindingsFile = "xiaozhi-bindings.json"
	defaultSampleRate   = 24000
	defaultFrameMS      = 60
)

type config struct {
	addr              string
	publicWSURL       string
	coreBaseURL       string
	managementBaseURL string
	secret            string
	internalToken     string
	bindingsFile      string
	ffmpegPath        string
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func loadConfig() config {
	return config{
		addr:              envOrDefault("XIAOZHI_ADDR", defaultAddr),
		publicWSURL:       envOrDefault("XIAOZHI_PUBLIC_WS_URL", defaultPublicWSURL),
		coreBaseURL:       envOrDefault("XIAOZHI_CORE_BASE_URL", defaultCoreBaseURL),
		managementBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("XIAOZHI_MANAGEMENT_BASE_URL")), "/"),
		secret:            envOrDefault("XIAOZHI_DEVICE_SECRET", "local-xiaozhi-dev-secret"),
		internalToken:     envOrDefault("XIAOZHI_INTERNAL_TOKEN", "local-xiaozhi-internal-token"),
		bindingsFile:      envOrDefault("XIAOZHI_BINDINGS_FILE", defaultBindingsFile),
		ffmpegPath:        envOrDefault("FFMPEG_PATH", "ffmpeg"),
	}
}
