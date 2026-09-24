package collector

import (
	"context"
	"errors"
	"testing"

	"livecompanion/core/internal/model"
)

type registryTestRunner struct {
	name string
}

func (r *registryTestRunner) Name() string {
	return r.name
}

func (r *registryTestRunner) Run(
	context.Context,
	model.Room,
	LiveFunc,
	EmitFunc,
) error {
	return nil
}

type registryTestProvider struct {
	descriptor ProviderDescriptor
	createErr  error
	seenMode   string
}

func (p *registryTestProvider) Descriptor() ProviderDescriptor {
	return p.descriptor
}

func (p *registryTestProvider) Create(room model.Room) (Runner, error) {
	p.seenMode = room.CollectorMode
	if p.createErr != nil {
		return nil, p.createErr
	}
	return &registryTestRunner{name: p.descriptor.ID}, nil
}

type registryPushProvider struct {
	registryTestProvider
	result IngressResult
}

func (p *registryPushProvider) Ingest(
	context.Context,
	model.Room,
	IngressEnvelope,
) (IngressResult, error) {
	return p.result, nil
}

func TestRegistryAutoSelectsHighestPriorityProvider(t *testing.T) {
	browser := &registryTestProvider{descriptor: ProviderDescriptor{
		ID:       "douyin.browser",
		Platform: "douyin",
		Modes:    []string{"browser"},
		Priority: 1000,
	}}
	official := &registryTestProvider{descriptor: ProviderDescriptor{
		ID:       "douyin.official",
		Platform: "douyin",
		Modes:    []string{"official"},
		Priority: 10,
	}}
	registry, err := NewRegistry(browser, official)
	if err != nil {
		t.Fatal(err)
	}

	runner, err := registry.Create(model.Room{
		ID:            1,
		Platform:      "douyin",
		CollectorMode: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.Name() != "douyin.official/douyin.official" {
		t.Fatalf("runner=%q", runner.Name())
	}
	if official.seenMode != "official" {
		t.Fatalf("auto must be normalized to provider mode, got %q", official.seenMode)
	}
}

func TestRegistryExplicitModeSelectsMatchingProvider(t *testing.T) {
	browser := &registryTestProvider{descriptor: ProviderDescriptor{
		ID:       "douyin.browser",
		Platform: "douyin",
		Modes:    []string{"browser"},
		Priority: 1000,
	}}
	official := &registryTestProvider{descriptor: ProviderDescriptor{
		ID:       "douyin.official",
		Platform: "douyin",
		Modes:    []string{"official"},
		Priority: 10,
	}}
	registry, err := NewRegistry(browser, official)
	if err != nil {
		t.Fatal(err)
	}

	runner, err := registry.Create(model.Room{
		ID:            1,
		Platform:      "douyin",
		CollectorMode: "browser",
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.Name() != "douyin.browser/douyin.browser" {
		t.Fatalf("runner=%q", runner.Name())
	}
}

func TestRegistryFallsBackWhenPreferredProviderCannotCreate(t *testing.T) {
	official := &registryTestProvider{
		descriptor: ProviderDescriptor{
			ID:       "douyin.official",
			Platform: "douyin",
			Modes:    []string{"official"},
			Priority: 10,
		},
		createErr: errors.New("credentials unavailable"),
	}
	browser := &registryTestProvider{descriptor: ProviderDescriptor{
		ID:       "douyin.browser",
		Platform: "douyin",
		Modes:    []string{"browser"},
		Priority: 1000,
	}}
	registry, err := NewRegistry(official, browser)
	if err != nil {
		t.Fatal(err)
	}

	runner, err := registry.Create(model.Room{
		ID:            1,
		Platform:      "douyin",
		CollectorMode: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if runner.Name() != "douyin.browser/douyin.browser" {
		t.Fatalf("runner=%q", runner.Name())
	}
}

func TestRegistryRoutesPushIngress(t *testing.T) {
	push := &registryPushProvider{
		registryTestProvider: registryTestProvider{descriptor: ProviderDescriptor{
			ID:           "douyin.official",
			Platform:     "douyin",
			Modes:        []string{"official"},
			Priority:     10,
			Capabilities: []Capability{CapabilityEvents, CapabilityPushIngress},
		}},
		result: IngressResult{
			Live: true,
			Events: []model.CreateEventInput{{
				EventType: "comment",
				Content:   "hello",
			}},
		},
	}
	registry, err := NewRegistry(push)
	if err != nil {
		t.Fatal(err)
	}

	result, err := registry.Ingest(
		context.Background(),
		model.Room{
			ID:            1,
			Platform:      "douyin",
			CollectorMode: "official",
		},
		IngressEnvelope{
			ProviderID:     "douyin.official",
			Platform:       "douyin",
			ExternalRoomID: "room-1",
			Payload:        []byte(`{"type":"comment"}`),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Live || len(result.Events) != 1 || result.Events[0].Content != "hello" {
		t.Fatalf("unexpected ingress result: %#v", result)
	}
}
