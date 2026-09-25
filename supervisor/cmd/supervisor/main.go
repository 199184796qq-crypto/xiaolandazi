package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"livecompanion/supervisor/internal/supervisor"
)

func main() {
	var root string
	var configPath string
	flag.StringVar(&root, "root", "", "project root")
	flag.StringVar(&configPath, "config", "", "supervisor config file")
	flag.Parse()

	resolvedRoot, err := resolveRoot(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve root:", err)
		os.Exit(1)
	}
	if configPath == "" {
		configPath = filepath.Join(resolvedRoot, "configs", "supervisor.json")
	}

	cfg, err := supervisor.LoadConfig(configPath, resolvedRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load supervisor config:", err)
		os.Exit(1)
	}

	runner, err := supervisor.New(cfg)
	if errors.Is(err, supervisor.ErrAlreadyRunning) {
		fmt.Fprintln(os.Stderr, "supervisor already running; duplicate launch ignored")
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "create supervisor:", err)
		os.Exit(1)
	}
	defer runner.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runner.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "supervisor stopped:", err)
		os.Exit(1)
	}
}

func resolveRoot(flagValue string) (string, error) {
	candidates := []string{flagValue, os.Getenv("LIVE_COMPANION_ROOT")}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "..")))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(absolute, "configs", "supervisor.json")); err == nil {
			return absolute, nil
		}
	}

	return "", fmt.Errorf("project root not found; pass -root or set LIVE_COMPANION_ROOT")
}
