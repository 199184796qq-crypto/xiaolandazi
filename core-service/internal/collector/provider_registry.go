package collector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"livecompanion/core/internal/model"
)

type Capability string

const (
	CapabilityEvents      Capability = "events"
	CapabilityPreview     Capability = "preview"
	CapabilityMediaStream Capability = "media_stream"
	CapabilityPushIngress Capability = "push_ingress"
)

type ProviderDescriptor struct {
	ID           string       `json:"id"`
	Platform     string       `json:"platform"`
	Modes        []string     `json:"modes"`
	Priority     int          `json:"priority"`
	Capabilities []Capability `json:"capabilities"`
}

type Provider interface {
	Descriptor() ProviderDescriptor
	Create(model.Room) (Runner, error)
}

type IngressEnvelope struct {
	ProviderID     string            `json:"provider_id,omitempty"`
	Platform       string            `json:"platform"`
	Mode           string            `json:"mode,omitempty"`
	ExternalRoomID string            `json:"external_room_id"`
	EventType      string            `json:"event_type,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Payload        []byte            `json:"payload"`
	ReceivedAt     time.Time         `json:"received_at"`
}

type IngressResult struct {
	Live   bool                     `json:"live"`
	Events []model.CreateEventInput `json:"events,omitempty"`
}

// PushIngressProvider is transport-neutral. An official HTTP webhook,
// a Live Companion plugin bridge, or another message gateway can normalize
// its input into IngressEnvelope before reaching Core.
type PushIngressProvider interface {
	Ingest(context.Context, model.Room, IngressEnvelope) (IngressResult, error)
}

type ProviderCatalog interface {
	ProviderDescriptors() []ProviderDescriptor
}

type providerSelection struct {
	provider Provider
	token    uint64
}

type Registry struct {
	mu        sync.RWMutex
	providers []Provider
	active    map[int64]providerSelection
	nextToken atomic.Uint64
}

func NewRegistry(providers ...Provider) (*Registry, error) {
	registry := &Registry{
		active: make(map[int64]providerSelection),
	}
	for _, provider := range providers {
		if err := registry.Register(provider); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return errors.New("collector provider is nil")
	}
	descriptor := normalizeProviderDescriptor(provider.Descriptor())
	if descriptor.ID == "" {
		return errors.New("collector provider id is required")
	}
	if descriptor.Platform == "" {
		return fmt.Errorf("collector provider %q platform is required", descriptor.ID)
	}
	if len(descriptor.Modes) == 0 {
		return fmt.Errorf("collector provider %q must expose at least one mode", descriptor.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.providers {
		if normalizeProviderDescriptor(existing.Descriptor()).ID == descriptor.ID {
			return fmt.Errorf("collector provider %q already registered", descriptor.ID)
		}
	}
	r.providers = append(r.providers, provider)
	r.sortLocked()
	return nil
}

func (r *Registry) ProviderDescriptors() []ProviderDescriptor {
	r.mu.RLock()
	providers := append([]Provider(nil), r.providers...)
	r.mu.RUnlock()

	items := make([]ProviderDescriptor, 0, len(providers))
	for _, provider := range providers {
		items = append(items, normalizeProviderDescriptor(provider.Descriptor()))
	}
	return items
}

func (r *Registry) Create(room model.Room) (Runner, error) {
	candidates := r.candidates(room)
	if len(candidates) == 0 {
		return nil, fmt.Errorf(
			"no collector provider for platform=%q mode=%q",
			normalizePlatform(room.Platform),
			normalizeMode(room.CollectorMode),
		)
	}

	var failures []error
	for _, provider := range candidates {
		descriptor := normalizeProviderDescriptor(provider.Descriptor())
		providerRoom := room
		if normalizeMode(room.CollectorMode) == "auto" {
			providerRoom.CollectorMode = descriptor.Modes[0]
		}
		runner, err := provider.Create(providerRoom)
		if err != nil {
			failures = append(
				failures,
				fmt.Errorf("%s: %w", descriptor.ID, err),
			)
			continue
		}
		token := r.nextToken.Add(1)
		return &trackedRunner{
			registry: r,
			provider: provider,
			runner:   runner,
			roomID:   room.ID,
			token:    token,
		}, nil
	}
	return nil, fmt.Errorf(
		"all collector providers failed for platform=%q mode=%q: %w",
		normalizePlatform(room.Platform),
		normalizeMode(room.CollectorMode),
		errors.Join(failures...),
	)
}

func (r *Registry) Preview(
	ctx context.Context,
	room model.Room,
) ([]byte, string, error) {
	providers := r.capabilityCandidates(room, CapabilityPreview)
	var failures []error
	for _, provider := range providers {
		preview, ok := provider.(PreviewProvider)
		if !ok {
			continue
		}
		payload, contentType, err := preview.Preview(ctx, room)
		if err == nil {
			return payload, contentType, nil
		}
		failures = append(
			failures,
			fmt.Errorf("%s: %w", provider.Descriptor().ID, err),
		)
	}
	if len(failures) == 0 {
		return nil, "", errors.New("collector preview is not supported")
	}
	return nil, "", errors.Join(failures...)
}

func (r *Registry) Stream(
	ctx context.Context,
	room model.Room,
) (StreamSource, error) {
	providers := r.capabilityCandidates(room, CapabilityMediaStream)
	var failures []error
	for _, provider := range providers {
		stream, ok := provider.(StreamProvider)
		if !ok {
			continue
		}
		source, err := stream.Stream(ctx, room)
		if err == nil {
			return source, nil
		}
		failures = append(
			failures,
			fmt.Errorf("%s: %w", provider.Descriptor().ID, err),
		)
	}
	if len(failures) == 0 {
		return StreamSource{}, errors.New("collector stream source is not supported")
	}
	return StreamSource{}, errors.Join(failures...)
}

func (r *Registry) Ingest(
	ctx context.Context,
	room model.Room,
	envelope IngressEnvelope,
) (IngressResult, error) {
	providers := r.capabilityCandidates(room, CapabilityPushIngress)
	providerID := strings.ToLower(strings.TrimSpace(envelope.ProviderID))
	var failures []error
	for _, provider := range providers {
		descriptor := normalizeProviderDescriptor(provider.Descriptor())
		if providerID != "" && descriptor.ID != providerID {
			continue
		}
		ingress, ok := provider.(PushIngressProvider)
		if !ok {
			continue
		}
		result, err := ingress.Ingest(ctx, room, envelope)
		if err == nil {
			return result, nil
		}
		failures = append(
			failures,
			fmt.Errorf("%s: %w", descriptor.ID, err),
		)
	}
	if len(failures) == 0 {
		return IngressResult{}, errors.New("collector push ingress is not supported")
	}
	return IngressResult{}, errors.Join(failures...)
}

func (r *Registry) candidates(room model.Room) []Provider {
	platform := normalizePlatform(room.Platform)
	mode := normalizeMode(room.CollectorMode)
	r.mu.RLock()
	providers := append([]Provider(nil), r.providers...)
	r.mu.RUnlock()

	result := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		descriptor := normalizeProviderDescriptor(provider.Descriptor())
		if descriptor.Platform != platform {
			continue
		}
		if mode != "auto" && !containsMode(descriptor.Modes, mode) {
			continue
		}
		result = append(result, provider)
	}
	return result
}

func (r *Registry) capabilityCandidates(
	room model.Room,
	capability Capability,
) []Provider {
	active := r.activeProvider(room.ID)
	candidates := r.candidates(room)
	result := make([]Provider, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	appendProvider := func(provider Provider) {
		if provider == nil {
			return
		}
		descriptor := normalizeProviderDescriptor(provider.Descriptor())
		if !containsCapability(descriptor.Capabilities, capability) {
			return
		}
		if _, ok := seen[descriptor.ID]; ok {
			return
		}
		seen[descriptor.ID] = struct{}{}
		result = append(result, provider)
	}
	appendProvider(active)
	for _, provider := range candidates {
		appendProvider(provider)
	}
	return result
}

func (r *Registry) activeProvider(roomID int64) Provider {
	r.mu.RLock()
	selection := r.active[roomID]
	r.mu.RUnlock()
	return selection.provider
}

func (r *Registry) markActive(roomID int64, provider Provider, token uint64) {
	r.mu.Lock()
	r.active[roomID] = providerSelection{provider: provider, token: token}
	r.mu.Unlock()
}

func (r *Registry) clearActive(roomID int64, token uint64) {
	r.mu.Lock()
	if current, ok := r.active[roomID]; ok && current.token == token {
		delete(r.active, roomID)
	}
	r.mu.Unlock()
}

func (r *Registry) sortLocked() {
	sort.SliceStable(r.providers, func(i, j int) bool {
		left := normalizeProviderDescriptor(r.providers[i].Descriptor())
		right := normalizeProviderDescriptor(r.providers[j].Descriptor())
		if left.Priority == right.Priority {
			return left.ID < right.ID
		}
		return left.Priority < right.Priority
	})
}

type trackedRunner struct {
	registry *Registry
	provider Provider
	runner   Runner
	roomID   int64
	token    uint64
}

func (r *trackedRunner) Name() string {
	return r.provider.Descriptor().ID + "/" + r.runner.Name()
}

func (r *trackedRunner) Run(
	ctx context.Context,
	room model.Room,
	onLive LiveFunc,
	emit EmitFunc,
) error {
	r.registry.markActive(r.roomID, r.provider, r.token)
	defer r.registry.clearActive(r.roomID, r.token)
	return r.runner.Run(ctx, room, onLive, emit)
}

func normalizeProviderDescriptor(value ProviderDescriptor) ProviderDescriptor {
	value.ID = strings.ToLower(strings.TrimSpace(value.ID))
	value.Platform = normalizePlatform(value.Platform)
	if value.Priority < 0 {
		value.Priority = 0
	}
	modes := make([]string, 0, len(value.Modes))
	seenModes := map[string]struct{}{}
	for _, mode := range value.Modes {
		mode = normalizeMode(mode)
		if mode == "auto" {
			continue
		}
		if _, ok := seenModes[mode]; ok {
			continue
		}
		seenModes[mode] = struct{}{}
		modes = append(modes, mode)
	}
	value.Modes = modes
	return value
}

func normalizePlatform(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "douyin"
	}
	return value
}

func normalizeMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "auto"
	}
	return value
}

func containsMode(modes []string, target string) bool {
	for _, mode := range modes {
		if normalizeMode(mode) == target {
			return true
		}
	}
	return false
}

func containsCapability(capabilities []Capability, target Capability) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}
