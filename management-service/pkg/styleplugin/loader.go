package styleplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ExecutorResolver maps a manifest runtime name to a service-owned trusted
// implementation. Returning false always rejects the plugin; no prompt-only
// fallback is allowed.
type ExecutorResolver func(name string) (Executor, bool)

type LoadedPlugin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

// LoadDirectory scans exactly one plugin level: <root>/<plugin>/plugin.json.
// It never opens or executes any other file from a plugin directory.
func LoadDirectory(root string, capabilities []string, resolver ExecutorResolver) (*Registry, []LoadedPlugin, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("read style plugin directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	registry := NewRegistryWithCapabilities(capabilities)
	loaded := make([]LoadedPlugin, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "plugin.json")
		file, openErr := os.Open(path)
		if os.IsNotExist(openErr) {
			continue
		}
		if openErr != nil {
			return nil, nil, fmt.Errorf("open style plugin %s: %w", entry.Name(), openErr)
		}
		manifest, decodeErr := DecodeManifest(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return nil, nil, fmt.Errorf("load style plugin %s: %w", entry.Name(), decodeErr)
		}
		if closeErr != nil {
			return nil, nil, fmt.Errorf("close style plugin %s: %w", entry.Name(), closeErr)
		}
		var executor Executor
		if manifest.Mode == ModeTrustedExecutor {
			if resolver == nil {
				return nil, nil, fmt.Errorf("style plugin %s requires executor %s", manifest.ID, manifest.Runtime.Executor)
			}
			var ok bool
			executor, ok = resolver(manifest.Runtime.Executor)
			if !ok || executor == nil {
				return nil, nil, fmt.Errorf("style plugin %s references unknown executor %s", manifest.ID, manifest.Runtime.Executor)
			}
		}
		if err := registry.Register(manifest, executor); err != nil {
			return nil, nil, fmt.Errorf("register style plugin %s: %w", manifest.ID, err)
		}
		loaded = append(loaded, LoadedPlugin{ID: manifest.ID, Version: manifest.Version, Path: path})
	}
	return registry, loaded, nil
}
