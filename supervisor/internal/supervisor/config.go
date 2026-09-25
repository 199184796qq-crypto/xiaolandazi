package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Config struct {
	Root                      string
	LogDir                    string
	RunDir                    string
	CheckInterval             time.Duration
	HealthTimeout             time.Duration
	HeartbeatInterval         time.Duration
	FailureThreshold          int
	RestartBackoff            time.Duration
	ResourceCheckInterval     time.Duration
	EmergencyMemoryFloorMB    uint64
	EmergencyFailureThreshold int
	EmergencyRestartDelay     time.Duration
	EmergencyTargetService    string
	Services                  []ServiceConfig
}

type ServiceConfig struct {
	Name           string
	HealthURL      string
	Command        string
	CommandWindows string
	CommandLinux   string
	CommandDarwin  string
	Args           []string
	WorkingDir     string
	Env            map[string]string
	StartupGrace   time.Duration
	Priority       string
}

type rawConfig struct {
	LogDir                    string
	RunDir                    string
	CheckInterval             string
	HealthTimeout             string
	HeartbeatInterval         string
	FailureThreshold          int
	RestartBackoff            string
	ResourceCheckInterval     string
	EmergencyMemoryFloorMB    uint64
	EmergencyFailureThreshold int
	EmergencyRestartDelay     string
	EmergencyTargetService    string
	Services                  []rawService
}

type rawService struct {
	Name           string
	HealthURL      string
	Command        string
	CommandWindows string
	CommandLinux   string
	CommandDarwin  string
	Args           []string
	WorkingDir     string
	Env            map[string]string
	StartupGrace   string
	Priority       string
}

func LoadConfig(path, root string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", path, err)
	}

	checkInterval, err := parseDuration(raw.CheckInterval, 5*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("CheckInterval: %w", err)
	}
	healthTimeout, err := parseDuration(raw.HealthTimeout, 2*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("HealthTimeout: %w", err)
	}
	heartbeatInterval, err := parseDuration(raw.HeartbeatInterval, time.Minute)
	if err != nil {
		return Config{}, fmt.Errorf("HeartbeatInterval: %w", err)
	}
	restartBackoff, err := parseDuration(raw.RestartBackoff, 2*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("RestartBackoff: %w", err)
	}
	if raw.FailureThreshold <= 0 {
		raw.FailureThreshold = 2
	}
	resourceCheckInterval, err := parseDuration(raw.ResourceCheckInterval, time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("ResourceCheckInterval: %w", err)
	}
	emergencyRestartDelay, err := parseDuration(raw.EmergencyRestartDelay, 15*time.Second)
	if err != nil {
		return Config{}, fmt.Errorf("EmergencyRestartDelay: %w", err)
	}
	if raw.EmergencyMemoryFloorMB == 0 {
		raw.EmergencyMemoryFloorMB = 1024
	}
	if raw.EmergencyFailureThreshold <= 0 {
		raw.EmergencyFailureThreshold = 3
	}
	if strings.TrimSpace(raw.EmergencyTargetService) == "" {
		raw.EmergencyTargetService = "core-service"
	}

	config := Config{
		Root:                      filepath.Clean(root),
		LogDir:                    expandPath(raw.LogDir, root, filepath.Join(root, "data", "logs")),
		RunDir:                    expandPath(raw.RunDir, root, filepath.Join(root, "data", "run")),
		CheckInterval:             checkInterval,
		HealthTimeout:             healthTimeout,
		HeartbeatInterval:         heartbeatInterval,
		FailureThreshold:          raw.FailureThreshold,
		RestartBackoff:            restartBackoff,
		ResourceCheckInterval:     resourceCheckInterval,
		EmergencyMemoryFloorMB:    raw.EmergencyMemoryFloorMB,
		EmergencyFailureThreshold: raw.EmergencyFailureThreshold,
		EmergencyRestartDelay:     emergencyRestartDelay,
		EmergencyTargetService:    strings.TrimSpace(raw.EmergencyTargetService),
		Services:                  make([]ServiceConfig, 0, len(raw.Services)),
	}

	seen := map[string]struct{}{}
	for index, item := range raw.Services {
		if strings.TrimSpace(item.Name) == "" {
			return Config{}, fmt.Errorf("Services[%d].Name is required", index)
		}
		if _, exists := seen[item.Name]; exists {
			return Config{}, fmt.Errorf("duplicate service name %q", item.Name)
		}
		seen[item.Name] = struct{}{}

		grace, err := parseDuration(item.StartupGrace, 8*time.Second)
		if err != nil {
			return Config{}, fmt.Errorf("Services[%d].StartupGrace: %w", index, err)
		}

		service := ServiceConfig{
			Name:           item.Name,
			HealthURL:      strings.TrimSpace(item.HealthURL),
			Command:        expandValue(item.Command, root),
			CommandWindows: expandValue(item.CommandWindows, root),
			CommandLinux:   expandValue(item.CommandLinux, root),
			CommandDarwin:  expandValue(item.CommandDarwin, root),
			Args:           expandValues(item.Args, root),
			WorkingDir:     expandPath(item.WorkingDir, root, root),
			Env:            make(map[string]string, len(item.Env)),
			StartupGrace:   grace,
			Priority:       normalizePriority(item.Priority),
		}
		for key, value := range item.Env {
			service.Env[key] = expandValue(value, root)
		}
		if service.HealthURL == "" {
			return Config{}, fmt.Errorf("Services[%d].HealthURL is required", index)
		}
		if service.commandForOS(runtime.GOOS) == "" {
			return Config{}, fmt.Errorf("Services[%d] has no command for %s", index, runtime.GOOS)
		}
		config.Services = append(config.Services, service)
	}

	if len(config.Services) == 0 {
		return Config{}, fmt.Errorf("at least one service is required")
	}
	return config, nil
}

func normalizePriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "below_normal", "belownormal", "low":
		return "below_normal"
	case "high":
		return "high"
	default:
		return "normal"
	}
}

func (s ServiceConfig) commandForOS(goos string) string {
	switch goos {
	case "windows":
		if s.CommandWindows != "" {
			return s.CommandWindows
		}
	case "linux":
		if s.CommandLinux != "" {
			return s.CommandLinux
		}
	case "darwin":
		if s.CommandDarwin != "" {
			return s.CommandDarwin
		}
	}
	return s.Command
}

func parseDuration(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return time.ParseDuration(value)
}

func expandValue(value, root string) string {
	return strings.ReplaceAll(value, "${ROOT}", filepath.Clean(root))
}

func expandValues(values []string, root string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = expandValue(value, root)
	}
	return result
}

func expandPath(value, root, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return filepath.Clean(fallback)
	}
	expanded := expandValue(value, root)
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(root, expanded)
	}
	return filepath.Clean(expanded)
}
