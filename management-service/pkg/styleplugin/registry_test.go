package styleplugin

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDeclarativeRegistryCompile(t *testing.T) {
	manifest := loadExampleManifest(t, "self-correction")
	instance := loadExampleInstance(t, "self-correction", "valid-instance.json")
	registry := NewRegistry()
	if err := registry.Register(manifest, nil); err != nil {
		t.Fatal(err)
	}
	fragment, err := registry.Compile(context.Background(), instance, CompileContext{Scene: "mainline", TargetChars: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if fragment.Capability != "self_correction" || fragment.Instruction == "" || fragment.Strength != 45 {
		t.Fatalf("unexpected fragment: %+v", fragment)
	}
}

func TestTrustedPluginRequiresExecutor(t *testing.T) {
	manifest := loadExampleManifest(t, "numeric-repair")
	registry := NewRegistry()
	if err := registry.Register(manifest, nil); err == nil {
		t.Fatal("expected missing trusted executor to fail")
	}
}

func TestStrictRegistryChecksRequirements(t *testing.T) {
	manifest := loadExampleManifest(t, "numeric-repair")
	registry := NewRegistryWithCapabilities([]string{"verified-calculator"})
	if err := registry.Register(manifest, ExecutorFunc(func(context.Context, Manifest, Instance, CompileContext) (CompiledFragment, error) {
		return CompiledFragment{}, nil
	})); err == nil {
		t.Fatal("expected missing atomic segment capabilities to fail")
	}
}

func TestLoadDirectory(t *testing.T) {
	root := filepath.Join("..", "..", "..", "plugins", "anchor-style")
	registry, loaded, err := LoadDirectory(root, []string{
		"verified-calculator", "atomic-audio-segment", "atomic-subtitle-segment",
	}, func(name string) (Executor, bool) {
		if name != "numeric_repair_v1" {
			return nil, false
		}
		return ExecutorFunc(func(context.Context, Manifest, Instance, CompileContext) (CompiledFragment, error) {
			return CompiledFragment{}, nil
		}), true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 5 || len(registry.IDs()) != 5 {
		t.Fatalf("loaded=%v ids=%v", loaded, registry.IDs())
	}
}
