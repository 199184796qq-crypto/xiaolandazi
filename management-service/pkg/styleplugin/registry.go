package styleplugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type CompileContext struct {
	Scene               string         `json:"scene"`
	TargetChars         int            `json:"target_chars,omitempty"`
	Serious             bool           `json:"serious"`
	Mode                string         `json:"mode,omitempty"`
	ElapsedSeconds      int            `json:"elapsed_seconds,omitempty"`
	CycleIndex          int            `json:"cycle_index,omitempty"`
	StepIndex           int            `json:"step_index,omitempty"`
	RoomHeat            string         `json:"room_heat,omitempty"`
	OnlineCount         int            `json:"online_count,omitempty"`
	QuestionPressure    float64        `json:"question_pressure,omitempty"`
	AudienceTurnover5m  float64        `json:"audience_turnover_5m,omitempty"`
	RecentCapabilities  []string       `json:"recent_capabilities,omitempty"`
	CapabilityUseCounts map[string]int `json:"capability_use_counts,omitempty"`
}

// CompiledFragment is merged by the core compiler. It contains bounded style
// behavior only; facts and interruption decisions stay outside this contract.
type CompiledFragment struct {
	PluginID        string         `json:"plugin_id"`
	PluginVersion   string         `json:"plugin_version"`
	InstanceID      string         `json:"instance_id"`
	Capability      string         `json:"capability"`
	Instruction     string         `json:"instruction,omitempty"`
	MicroActions    []string       `json:"micro_actions,omitempty"`
	Strength        int            `json:"strength"`
	Parameters      map[string]any `json:"parameters,omitempty"`
	Atomic          bool           `json:"atomic"`
	Interruptible   bool           `json:"interruptible"`
	ForbiddenScenes []string       `json:"forbidden_scenes,omitempty"`
}

type Executor interface {
	Compile(context.Context, Manifest, Instance, CompileContext) (CompiledFragment, error)
}

type ExecutorFunc func(context.Context, Manifest, Instance, CompileContext) (CompiledFragment, error)

func (fn ExecutorFunc) Compile(ctx context.Context, manifest Manifest, instance Instance, input CompileContext) (CompiledFragment, error) {
	return fn(ctx, manifest, instance, input)
}

type registeredPlugin struct {
	manifest Manifest
	executor Executor
}

type Registry struct {
	mu                  sync.RWMutex
	plugins             map[string]registeredPlugin
	available           map[string]bool
	enforceRequirements bool
}

func NewRegistry() *Registry {
	return &Registry{plugins: map[string]registeredPlugin{}, available: map[string]bool{}}
}

// NewRegistryWithCapabilities enables strict dependency checks. Capabilities
// describe deployment-owned services such as verified-calculator and atomic
// audio/subtitle segments.
func NewRegistryWithCapabilities(capabilities []string) *Registry {
	registry := NewRegistry()
	registry.enforceRequirements = true
	for _, capability := range capabilities {
		registry.available[capability] = true
	}
	return registry
}

func registryKey(id, version string) string { return id + "@" + version }

func (registry *Registry) Register(manifest Manifest, executor Executor) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	if manifest.Mode == ModeTrustedExecutor && executor == nil {
		return fmt.Errorf("plugin %s requires trusted executor %s", manifest.ID, manifest.Runtime.Executor)
	}
	if manifest.Mode == ModeDeclarative && executor != nil {
		return fmt.Errorf("declarative plugin %s must not register an executor", manifest.ID)
	}
	key := registryKey(manifest.ID, manifest.Version)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.plugins[key]; exists {
		return fmt.Errorf("plugin %s already registered", key)
	}
	if registry.enforceRequirements {
		for _, requirement := range manifest.Requires {
			if !registry.available[requirement] {
				return fmt.Errorf("plugin %s requires unavailable capability %s", manifest.ID, requirement)
			}
		}
	}
	registry.plugins[key] = registeredPlugin{manifest: manifest, executor: executor}
	registry.available[manifest.ID] = true
	registry.available[manifest.Compiler.OutputCapability] = true
	return nil
}

func conflictsWith(left, right Manifest) bool {
	return slicesContains(left.Conflicts, right.ID) || slicesContains(left.Conflicts, right.Compiler.OutputCapability)
}

// ManifestsConflict is evaluated when instances are activated. Conflicting
// alternatives may coexist in the catalog; they may not coexist on one anchor.
func ManifestsConflict(left, right Manifest) bool {
	return conflictsWith(left, right) || conflictsWith(right, left)
}

func (registry *Registry) Resolve(id, version string) (Manifest, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	plugin, ok := registry.plugins[registryKey(id, version)]
	return plugin.manifest, ok
}

func (registry *Registry) IDs() []string {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	ids := make([]string, 0, len(registry.plugins))
	for key := range registry.plugins {
		ids = append(ids, key)
	}
	sort.Strings(ids)
	return ids
}

func (registry *Registry) Manifests() []Manifest {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	manifests := make([]Manifest, 0, len(registry.plugins))
	for _, plugin := range registry.plugins {
		manifests = append(manifests, plugin.manifest)
	}
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].ID == manifests[j].ID {
			return manifests[i].Version < manifests[j].Version
		}
		return manifests[i].ID < manifests[j].ID
	})
	return manifests
}

func (registry *Registry) Compile(ctx context.Context, instance Instance, input CompileContext) (CompiledFragment, error) {
	registry.mu.RLock()
	plugin, ok := registry.plugins[registryKey(instance.PluginID, instance.PluginVersion)]
	registry.mu.RUnlock()
	if !ok {
		return CompiledFragment{}, errors.New("style plugin is not registered")
	}
	if err := ValidateInstance(plugin.manifest, instance); err != nil {
		return CompiledFragment{}, err
	}
	if !instance.Enabled {
		return CompiledFragment{}, errors.New("style plugin instance is disabled")
	}
	if !allowedScenes[input.Scene] {
		return CompiledFragment{}, fmt.Errorf("unsupported compile scene %q", input.Scene)
	}
	if !slicesContains(plugin.manifest.Scenes, input.Scene) {
		return CompiledFragment{}, fmt.Errorf("plugin %s does not support scene %s", plugin.manifest.ID, input.Scene)
	}
	if input.Serious && slicesContains(plugin.manifest.Safety.ForbiddenScenes, "serious") {
		return CompiledFragment{}, errors.New("plugin is disabled in serious scenes")
	}
	if plugin.executor != nil {
		fragment, err := plugin.executor.Compile(ctx, plugin.manifest, instance, input)
		if err != nil {
			return CompiledFragment{}, err
		}
		return normalizeFragment(plugin.manifest, instance, fragment), nil
	}
	parameters, err := EffectiveParameters(plugin.manifest, instance)
	if err != nil {
		return CompiledFragment{}, err
	}
	return normalizeFragment(plugin.manifest, instance, CompiledFragment{
		Instruction:  strings.TrimSpace(plugin.manifest.Compiler.Instructions[input.Scene]),
		MicroActions: append([]string(nil), plugin.manifest.Compiler.MicroActions...),
		Parameters:   parameters,
	}), nil
}

func normalizeFragment(manifest Manifest, instance Instance, fragment CompiledFragment) CompiledFragment {
	fragment.PluginID = manifest.ID
	fragment.PluginVersion = manifest.Version
	fragment.InstanceID = instance.InstanceID
	fragment.Capability = manifest.Compiler.OutputCapability
	fragment.Strength = instance.Strength
	fragment.Atomic = manifest.Safety.AtomicOutputRequired
	fragment.Interruptible = manifest.Safety.Interruptible
	fragment.ForbiddenScenes = append([]string(nil), manifest.Safety.ForbiddenScenes...)
	return fragment
}

func slicesContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
