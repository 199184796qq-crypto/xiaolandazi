package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"livecompanion/management/internal/styleplugins/numericrepair"
	"livecompanion/management/pkg/styleplugin"
)

func loadStylePluginRegistry(configuredPath string) (*styleplugin.Registry, []styleplugin.LoadedPlugin, string, error) {
	configuredPath = strings.TrimSpace(configuredPath)
	if configuredPath == "" {
		return nil, nil, "", fmt.Errorf("ANCHOR_STYLE_PLUGIN_DIR is empty")
	}
	candidates := []string{configuredPath}
	if !filepath.IsAbs(configuredPath) {
		candidates = append(candidates, filepath.Join("..", configuredPath))
		if executable, err := os.Executable(); err == nil {
			candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", configuredPath))
		}
	}
	var root string
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			root, _ = filepath.Abs(candidate)
			break
		}
	}
	if root == "" {
		return nil, nil, "", fmt.Errorf("anchor-style plugin directory not found: %s", configuredPath)
	}
	registry, loaded, err := styleplugin.LoadDirectory(root, []string{
		"verified-calculator", "atomic-audio-segment", "atomic-subtitle-segment",
	}, func(name string) (styleplugin.Executor, bool) {
		switch name {
		case numericrepair.ExecutorName:
			return numericrepair.Executor{}, true
		default:
			return nil, false
		}
	})
	if err != nil {
		return nil, nil, root, err
	}
	return registry, loaded, root, nil
}
