package httpapi

import (
	"context"
	"path/filepath"
	"testing"

	"livecompanion/management/internal/styleplugins/numericrepair"
	"livecompanion/management/pkg/styleplugin"
)

func testStylePluginRegistry(t *testing.T) *styleplugin.Registry {
	t.Helper()
	root := filepath.Join("..", "..", "..", "plugins", "anchor-style")
	registry, _, err := styleplugin.LoadDirectory(root, []string{
		"verified-calculator", "atomic-audio-segment", "atomic-subtitle-segment",
	}, func(name string) (styleplugin.Executor, bool) {
		if name == numericrepair.ExecutorName {
			return numericrepair.Executor{}, true
		}
		return nil, false
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestBuildDeclarativePluginOverlay(t *testing.T) {
	registry := testStylePluginRegistry(t)
	manifest, ok := registry.Resolve("self-correction", "1.0.0")
	if !ok {
		t.Fatal("self-correction manifest missing")
	}
	instance := styleplugin.Instance{
		APIVersion: styleplugin.InstanceAPIVersion, InstanceID: "binding-test",
		PluginID: manifest.ID, PluginVersion: manifest.Version, Enabled: true, Strength: 45,
	}
	item, err := buildDeclarativePluginOverlay(context.Background(), registry, manifest, instance)
	if err != nil {
		t.Fatal(err)
	}
	if item.Plugin == nil || item.Plugin.PluginID != manifest.ID || item.Rule.MainlineMaxPer1000Chars != 2 {
		t.Fatalf("unexpected plugin overlay: %+v", item)
	}
}

func TestTrustedPluginCannotBecomePromptOverlay(t *testing.T) {
	registry := testStylePluginRegistry(t)
	manifest, ok := registry.Resolve("numeric-repair", "1.0.0")
	if !ok {
		t.Fatal("numeric-repair manifest missing")
	}
	if manifest.Mode == styleplugin.ModeDeclarative {
		t.Fatal("numeric repair must remain a trusted executor plugin")
	}
}
