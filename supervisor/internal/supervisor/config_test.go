package supervisor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadConfigExpandsRootAndDefaults(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "supervisor.json")
	data := []byte(`{
  "CheckInterval": "3s",
  "Services": [
    {
      "Name": "example",
      "HealthURL": "http://127.0.0.1:9999/healthz",
      "Command": "${ROOT}/bin/example",
      "CommandWindows": "${ROOT}/bin/example.exe",
      "WorkingDir": "${ROOT}/work"
    }
  ]
}`)
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(configPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CheckInterval != 3*time.Second {
		t.Fatalf("CheckInterval=%v", cfg.CheckInterval)
	}
	if cfg.FailureThreshold != 2 {
		t.Fatalf("FailureThreshold=%d", cfg.FailureThreshold)
	}
	if got := cfg.Services[0].WorkingDir; got != filepath.Join(root, "work") {
		t.Fatalf("WorkingDir=%q", got)
	}
	command := cfg.Services[0].commandForOS(runtime.GOOS)
	if !filepath.IsAbs(command) {
		t.Fatalf("command should be absolute: %q", command)
	}
}

func TestCommandForOSPrefersOverride(t *testing.T) {
	cfg := ServiceConfig{
		Command:        "default",
		CommandWindows: "windows",
		CommandLinux:   "linux",
		CommandDarwin:  "darwin",
	}
	tests := map[string]string{
		"windows": "windows",
		"linux":   "linux",
		"darwin":  "darwin",
		"freebsd": "default",
	}
	for goos, want := range tests {
		if got := cfg.commandForOS(goos); got != want {
			t.Fatalf("%s: got %q want %q", goos, got, want)
		}
	}
}
