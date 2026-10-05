package styleplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadExampleManifest(t *testing.T, plugin string) Manifest {
	t.Helper()
	path := filepath.Join("..", "..", "..", "plugins", "anchor-style", plugin, "plugin.json")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	manifest, err := DecodeManifest(file)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func loadExampleInstance(t *testing.T, plugin, filename string) Instance {
	t.Helper()
	path := filepath.Join("..", "..", "..", "plugins", "anchor-style", plugin, "testdata", filename)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	instance, err := DecodeInstance(file)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func TestExampleManifestsAndInstances(t *testing.T) {
	for _, plugin := range []string{"natural-hesitation", "natural-reduplication", "numeric-repair", "self-correction", "thinking-tempo"} {
		t.Run(plugin, func(t *testing.T) {
			manifest := loadExampleManifest(t, plugin)
			instance := loadExampleInstance(t, plugin, "valid-instance.json")
			if err := ValidateInstance(manifest, instance); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidInstanceIsRejected(t *testing.T) {
	manifest := loadExampleManifest(t, "numeric-repair")
	instance := loadExampleInstance(t, "numeric-repair", "invalid-instance.json")
	if err := ValidateInstance(manifest, instance); err == nil {
		t.Fatal("expected invalid instance to fail")
	}
}

func TestManifestRejectsUnknownField(t *testing.T) {
	document := `{
      "api_version":"anchor-style-plugin/v1",
      "id":"sample","version":"1.0.0","name":"Sample","description":"Sample",
      "mode":"declarative","category":"delivery","scenes":["mainline"],
      "safety":{"may_emit_intentional_false_derived_value":false,"may_change_source_fact":false,"atomic_output_required":false,"interruptible":true},
      "compiler":{"output_capability":"sample"},"surprise":true
    }`
	if _, err := DecodeManifest(strings.NewReader(document)); err == nil {
		t.Fatal("expected unknown field to fail")
	}
}

func TestFalseDerivedValueRequiresTrustedAtomicExecutor(t *testing.T) {
	manifest := loadExampleManifest(t, "numeric-repair")
	manifest.Mode = ModeDeclarative
	manifest.Runtime = nil
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected unsafe declarative manifest to fail")
	}
	manifest = loadExampleManifest(t, "numeric-repair")
	manifest.Safety.Interruptible = true
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected interruptible numeric repair to fail")
	}
}
