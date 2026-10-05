package main

import (
	"path/filepath"
	"testing"
)

func TestLoadStylePluginRegistryFromRepositoryDirectory(t *testing.T) {
	root := filepath.Join("..", "..", "..", "plugins", "anchor-style")
	registry, loaded, resolved, err := loadStylePluginRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	if registry == nil || len(loaded) != 5 || resolved == "" {
		t.Fatalf("registry=%v loaded=%v resolved=%q", registry, loaded, resolved)
	}
}
