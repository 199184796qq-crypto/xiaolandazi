package douyin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/model"
)

var (
	ErrNoWebcastFrames = errors.New("no douyin webcast payload received")
	ErrUnsupportedMode = errors.New("unsupported douyin collector mode")
)

const defaultFrameTimeout = 45 * time.Second

type Factory struct {
	browser browserRuntime
}

func NewFactory(browser browserRuntime) *Factory {
	return &Factory{browser: browser}
}

func (f *Factory) Descriptor() collector.ProviderDescriptor {
	return collector.ProviderDescriptor{
		ID:       "douyin.browser",
		Platform: "douyin",
		Modes: []string{
			"browser",
			// Keep the old lightweight mode as a compatibility alias until a real
			// lightweight provider is registered. A future provider with a lower
			// priority can take over this mode without changing Core.
			"lightweight",
		},
		Priority: 1000,
		Capabilities: []collector.Capability{
			collector.CapabilityEvents,
			collector.CapabilityPreview,
			collector.CapabilityMediaStream,
		},
	}
}

func (f *Factory) Create(room model.Room) (collector.Runner, error) {
	if room.Platform != "" && room.Platform != "douyin" {
		return nil, fmt.Errorf("unsupported platform %q", room.Platform)
	}

	switch strings.ToLower(strings.TrimSpace(room.CollectorMode)) {
	case "", "auto", "browser":
		return &BrowserCollector{
			browser:      f.browser,
			frameTimeout: defaultFrameTimeout,
		}, nil

	case "lightweight":
		// Compatibility mode: keep legacy/lightweight room configurations usable
		// by sharing the proven Playwright transport. This avoids hard failures
		// while preserving one decoder/event pipeline for all Douyin rooms.
		return &BrowserCollector{
			browser:      f.browser,
			frameTimeout: defaultFrameTimeout,
			name:         "douyin-lightweight-compat",
		}, nil

	default:
		return nil, fmt.Errorf(
			"%w: %s",
			ErrUnsupportedMode,
			room.CollectorMode,
		)
	}
}

func (f *Factory) Stream(
	ctx context.Context,
	room model.Room,
) (collector.StreamSource, error) {
	return f.browser.Stream(room.ID)
}
func (f *Factory) Preview(
	ctx context.Context,
	room model.Room,
) ([]byte, string, error) {
	return f.browser.RequestPreview(ctx, room.ID)
}

type BrowserCollector struct {
	browser      browserRuntime
	frameTimeout time.Duration
	name         string
}

func (c *BrowserCollector) Name() string {
	if strings.TrimSpace(c.name) != "" {
		return c.name
	}
	return "douyin-playwright"
}

func (c *BrowserCollector) Run(
	ctx context.Context,
	room model.Room,
	onLive collector.LiveFunc,
	emit collector.EmitFunc,
) error {
	session, err := c.browser.StartRoom(ctx, room)
	if err != nil {
		return err
	}
	defer session.Close()

	timer := time.NewTimer(c.frameTimeout)
	defer timer.Stop()

	liveNotified := false
	transportSeen := false
	frameCount := 0
	decodeErrors := 0
	transportCh := session.Transport()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-transportCh:
			if !transportSeen {
				log.Printf(
					"collector room=%d transport=playwright-wss seen=true",
					room.ID,
				)
			}
			transportSeen = true
			transportCh = nil

		case sessionErr := <-session.Errors():
			if sessionErr == nil {
				continue
			}
			return sessionErr

		case <-timer.C:
			return fmt.Errorf(
				"%w (transport_seen=%t frames=%d decode_errors=%d)",
				collector.ErrOffline,
				transportSeen,
				frameCount,
				decodeErrors,
			)

		case raw := <-session.Frames():
			frameCount++

			result, decodeErr := DecodePushFrame(raw)
			if decodeErr != nil {
				decodeErrors++
				if decodeErrors <= 3 {
					log.Printf(
						"collector room=%d decode_error=%v",
						room.ID,
						decodeErr,
					)
				}
				continue
			}
			if !result.Valid {
				continue
			}

			if !liveNotified {
				if err := onLive(ctx); err != nil {
					return err
				}
				liveNotified = true
				log.Printf(
					"collector room=%d status=live frames=%d",
					room.ID,
					frameCount,
				)
			}

			resetTimer(timer, c.frameTimeout)

			for _, event := range result.Events {
				if err := emit(ctx, event); err != nil {
					return err
				}
			}
		}
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
