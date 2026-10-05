// Package styleplugin defines the stable extension contract for anchor-style
// plugins. It deliberately contains no HTTP, database, model-provider, or
// prompt implementation so plugin authors can validate a plugin independently.
package styleplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
)

const (
	ManifestAPIVersion = "anchor-style-plugin/v1"
	InstanceAPIVersion = "anchor-style-plugin-instance/v1"

	ModeDeclarative     = "declarative"
	ModeTrustedExecutor = "trusted_executor"
)

var (
	pluginIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	semverPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
)

// Manifest is the machine-readable declaration stored in plugin.json.
// Unknown JSON fields are rejected by DecodeManifest.
type Manifest struct {
	Schema      string                         `json:"$schema,omitempty"`
	APIVersion  string                         `json:"api_version"`
	ID          string                         `json:"id"`
	Version     string                         `json:"version"`
	Name        string                         `json:"name"`
	Description string                         `json:"description"`
	Mode        string                         `json:"mode"`
	Category    string                         `json:"category"`
	Scenes      []string                       `json:"scenes"`
	Parameters  map[string]ParameterDefinition `json:"parameters,omitempty"`
	Requires    []string                       `json:"requires,omitempty"`
	Conflicts   []string                       `json:"conflicts,omitempty"`
	Safety      SafetyDeclaration              `json:"safety"`
	Compiler    CompilerDeclaration            `json:"compiler"`
	Runtime     *RuntimeDeclaration            `json:"runtime,omitempty"`
}

type ParameterDefinition struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	MinItems    *int     `json:"min_items,omitempty"`
	MaxItems    *int     `json:"max_items,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type SafetyDeclaration struct {
	MayEmitIntentionalFalseDerivedValue bool     `json:"may_emit_intentional_false_derived_value"`
	MayChangeSourceFact                 bool     `json:"may_change_source_fact"`
	AtomicOutputRequired                bool     `json:"atomic_output_required"`
	Interruptible                       bool     `json:"interruptible"`
	AllowedFactKinds                    []string `json:"allowed_fact_kinds,omitempty"`
	ForbiddenScenes                     []string `json:"forbidden_scenes,omitempty"`
}

type CompilerDeclaration struct {
	RecognizedIntents []string          `json:"recognized_intents,omitempty"`
	OutputCapability  string            `json:"output_capability"`
	Instructions      map[string]string `json:"instructions,omitempty"`
	MicroActions      []string          `json:"micro_actions,omitempty"`
}

type RuntimeDeclaration struct {
	Executor string `json:"executor"`
}

// Instance binds a manifest to one anchor plan. It is configuration, not code.
type Instance struct {
	APIVersion    string         `json:"api_version"`
	InstanceID    string         `json:"instance_id"`
	PluginID      string         `json:"plugin_id"`
	PluginVersion string         `json:"plugin_version"`
	Enabled       bool           `json:"enabled"`
	Strength      int            `json:"strength"`
	Parameters    map[string]any `json:"parameters,omitempty"`
}

var allowedCategories = map[string]bool{
	"delivery": true, "rhythm": true, "lexical": true, "local_flow": true,
	"humor": true, "interaction_delivery": true, "storytelling": true,
}

var allowedScenes = map[string]bool{
	"mainline": true, "interaction": true, "refresh": true, "serious": true,
}

var allowedParameterTypes = map[string]bool{
	"integer": true, "number": true, "boolean": true, "string": true, "string_array": true,
}

var allowedMicroActions = map[string]bool{
	"audience_address": true, "self_reference": true, "state_information": true,
	"direct_answer": true, "short_confirmation": true, "rephrase": true,
	"supplement": true, "bridge": true, "question": true, "scene_detail": true,
	"reaction": true, "conclusion": true, "reason": true, "example": true,
	"self_correction": true, "close": true,
}

func DecodeManifest(reader io.Reader) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode plugin manifest: %w", err)
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return Manifest{}, err
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func DecodeInstance(reader io.Reader) (Instance, error) {
	var instance Instance
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&instance); err != nil {
		return Instance{}, fmt.Errorf("decode plugin instance: %w", err)
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return Instance{}, err
	}
	return instance, nil
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("plugin document contains more than one JSON value")
		}
		return fmt.Errorf("decode plugin document trailer: %w", err)
	}
	return nil
}

func ValidateManifest(manifest Manifest) error {
	if manifest.APIVersion != ManifestAPIVersion {
		return fmt.Errorf("unsupported api_version %q", manifest.APIVersion)
	}
	if !pluginIDPattern.MatchString(manifest.ID) {
		return fmt.Errorf("invalid plugin id %q", manifest.ID)
	}
	if !semverPattern.MatchString(manifest.Version) {
		return fmt.Errorf("invalid semantic version %q", manifest.Version)
	}
	if strings.TrimSpace(manifest.Name) == "" || strings.TrimSpace(manifest.Description) == "" {
		return errors.New("plugin name and description are required")
	}
	if manifest.Mode != ModeDeclarative && manifest.Mode != ModeTrustedExecutor {
		return fmt.Errorf("unsupported plugin mode %q", manifest.Mode)
	}
	if !allowedCategories[manifest.Category] {
		return fmt.Errorf("unsupported plugin category %q", manifest.Category)
	}
	if len(manifest.Scenes) == 0 {
		return errors.New("at least one scene is required")
	}
	if err := validateUniqueNames("scene", manifest.Scenes, allowedScenes); err != nil {
		return err
	}
	if manifest.Safety.MayChangeSourceFact {
		return errors.New("style plugins may never change source facts")
	}
	if err := validateUniqueNames("forbidden scene", manifest.Safety.ForbiddenScenes, map[string]bool{"serious": true}); err != nil {
		return err
	}
	if err := validateUniqueNames("allowed fact kind", manifest.Safety.AllowedFactKinds, map[string]bool{"none": true, "derived_arithmetic": true}); err != nil {
		return err
	}
	if manifest.Safety.MayEmitIntentionalFalseDerivedValue {
		if manifest.Mode != ModeTrustedExecutor {
			return errors.New("intentional derived-value errors require trusted_executor mode")
		}
		if !manifest.Safety.AtomicOutputRequired || manifest.Safety.Interruptible {
			return errors.New("intentional derived-value errors must be atomic and non-interruptible")
		}
		if !slices.Contains(manifest.Safety.AllowedFactKinds, "derived_arithmetic") {
			return errors.New("intentional derived-value errors must be limited to derived_arithmetic")
		}
	}
	if !capabilityPattern.MatchString(manifest.Compiler.OutputCapability) {
		return fmt.Errorf("invalid output capability %q", manifest.Compiler.OutputCapability)
	}
	for scene, instruction := range manifest.Compiler.Instructions {
		if !allowedScenes[scene] {
			return fmt.Errorf("unsupported instruction scene %q", scene)
		}
		if !slices.Contains(manifest.Scenes, scene) {
			return fmt.Errorf("instruction scene %q is not declared in scenes", scene)
		}
		if strings.TrimSpace(instruction) == "" {
			return fmt.Errorf("instruction for scene %q is empty", scene)
		}
	}
	if len(manifest.Compiler.RecognizedIntents) > 20 {
		return errors.New("recognized_intents may contain at most 20 values")
	}
	intentSeen := map[string]bool{}
	for _, intent := range manifest.Compiler.RecognizedIntents {
		intent = strings.TrimSpace(intent)
		if intent == "" || intentSeen[intent] {
			return errors.New("recognized_intents must be non-empty and unique")
		}
		intentSeen[intent] = true
	}
	if len(manifest.Compiler.MicroActions) > 8 {
		return errors.New("micro_actions may contain at most 8 values")
	}
	actionSeen := map[string]bool{}
	for _, action := range manifest.Compiler.MicroActions {
		if !allowedMicroActions[action] {
			return fmt.Errorf("unsupported micro action %q", action)
		}
		if actionSeen[action] {
			return fmt.Errorf("duplicate micro action %q", action)
		}
		actionSeen[action] = true
	}
	for name, definition := range manifest.Parameters {
		if !capabilityPattern.MatchString(name) {
			return fmt.Errorf("invalid parameter name %q", name)
		}
		if err := validateParameterDefinition(name, definition); err != nil {
			return err
		}
	}
	if err := validateIdentifierList("requirement", manifest.Requires); err != nil {
		return err
	}
	if err := validateIdentifierList("conflict", manifest.Conflicts); err != nil {
		return err
	}
	if manifest.Mode == ModeTrustedExecutor {
		if manifest.Runtime == nil || !capabilityPattern.MatchString(manifest.Runtime.Executor) {
			return errors.New("trusted_executor plugin requires a valid runtime.executor")
		}
	} else if manifest.Runtime != nil {
		return errors.New("declarative plugin must not declare a runtime executor")
	}
	return nil
}

func validateUniqueNames(kind string, values []string, allowed map[string]bool) error {
	seen := map[string]bool{}
	for _, value := range values {
		if !allowed[value] {
			return fmt.Errorf("unsupported %s %q", kind, value)
		}
		if seen[value] {
			return fmt.Errorf("duplicate %s %q", kind, value)
		}
		seen[value] = true
	}
	return nil
}

func validateIdentifierList(kind string, values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if !pluginIDPattern.MatchString(value) && !capabilityPattern.MatchString(value) {
			return fmt.Errorf("invalid %s %q", kind, value)
		}
		if seen[value] {
			return fmt.Errorf("duplicate %s %q", kind, value)
		}
		seen[value] = true
	}
	return nil
}

func validateParameterDefinition(name string, definition ParameterDefinition) error {
	if !allowedParameterTypes[definition.Type] {
		return fmt.Errorf("parameter %q has unsupported type %q", name, definition.Type)
	}
	if strings.TrimSpace(definition.Description) == "" {
		return fmt.Errorf("parameter %q requires a description", name)
	}
	if definition.Minimum != nil && definition.Maximum != nil && *definition.Minimum > *definition.Maximum {
		return fmt.Errorf("parameter %q minimum exceeds maximum", name)
	}
	if definition.MinItems != nil && definition.MaxItems != nil && *definition.MinItems > *definition.MaxItems {
		return fmt.Errorf("parameter %q min_items exceeds max_items", name)
	}
	if definition.Type != "integer" && definition.Type != "number" && (definition.Minimum != nil || definition.Maximum != nil) {
		return fmt.Errorf("parameter %q uses numeric bounds with a non-numeric type", name)
	}
	if definition.Type != "string_array" && (definition.MinItems != nil || definition.MaxItems != nil) {
		return fmt.Errorf("parameter %q uses item bounds with a non-array type", name)
	}
	if definition.Type != "string" && definition.Type != "string_array" && len(definition.Enum) > 0 {
		return fmt.Errorf("parameter %q uses enum with an unsupported type", name)
	}
	if definition.Default != nil {
		if err := validateParameterValue(name, definition, definition.Default); err != nil {
			return fmt.Errorf("invalid default: %w", err)
		}
	}
	return nil
}

func ValidateInstance(manifest Manifest, instance Instance) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	if instance.APIVersion != InstanceAPIVersion {
		return fmt.Errorf("unsupported instance api_version %q", instance.APIVersion)
	}
	if !pluginIDPattern.MatchString(instance.InstanceID) {
		return fmt.Errorf("invalid instance id %q", instance.InstanceID)
	}
	if instance.PluginID != manifest.ID || instance.PluginVersion != manifest.Version {
		return errors.New("instance plugin id/version does not match manifest")
	}
	if instance.Strength < 0 || instance.Strength > 100 {
		return errors.New("instance strength must be between 0 and 100")
	}
	for name := range instance.Parameters {
		if _, ok := manifest.Parameters[name]; !ok {
			return fmt.Errorf("unknown parameter %q", name)
		}
	}
	for name, definition := range manifest.Parameters {
		value, ok := instance.Parameters[name]
		if !ok {
			if definition.Required && definition.Default == nil {
				return fmt.Errorf("required parameter %q is missing", name)
			}
			continue
		}
		if err := validateParameterValue(name, definition, value); err != nil {
			return err
		}
	}
	return nil
}

func validateParameterValue(name string, definition ParameterDefinition, value any) error {
	switch definition.Type {
	case "integer", "number":
		number, ok := numericValue(value)
		if !ok || definition.Type == "integer" && math.Trunc(number) != number {
			return fmt.Errorf("parameter %q must be %s", name, definition.Type)
		}
		if definition.Minimum != nil && number < *definition.Minimum || definition.Maximum != nil && number > *definition.Maximum {
			return fmt.Errorf("parameter %q is outside the allowed range", name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("parameter %q must be boolean", name)
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("parameter %q must be string", name)
		}
		if len(definition.Enum) > 0 && !slices.Contains(definition.Enum, text) {
			return fmt.Errorf("parameter %q is not one of the allowed values", name)
		}
	case "string_array":
		items, ok := stringSlice(value)
		if !ok {
			return fmt.Errorf("parameter %q must be a string array", name)
		}
		if definition.MinItems != nil && len(items) < *definition.MinItems || definition.MaxItems != nil && len(items) > *definition.MaxItems {
			return fmt.Errorf("parameter %q has an invalid item count", name)
		}
		for _, item := range items {
			if len(definition.Enum) > 0 && !slices.Contains(definition.Enum, item) {
				return fmt.Errorf("parameter %q contains a disallowed value", name)
			}
		}
	}
	return nil
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case int32:
		return float64(number), true
	default:
		return 0, false
	}
}

func stringSlice(value any) ([]string, bool) {
	switch items := value.(type) {
	case []string:
		return items, true
	case []any:
		result := make([]string, len(items))
		for index, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result[index] = text
		}
		return result, true
	default:
		return nil, false
	}
}

// EffectiveParameters returns instance values with manifest defaults applied.
func EffectiveParameters(manifest Manifest, instance Instance) (map[string]any, error) {
	if err := ValidateInstance(manifest, instance); err != nil {
		return nil, err
	}
	result := make(map[string]any, len(manifest.Parameters))
	for name, definition := range manifest.Parameters {
		if value, ok := instance.Parameters[name]; ok {
			result[name] = value
		} else if definition.Default != nil {
			result[name] = definition.Default
		}
	}
	return result, nil
}
