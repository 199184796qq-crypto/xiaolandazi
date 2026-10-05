package collector

import (
	"context"
	"errors"
	"time"

	"livecompanion/core/internal/model"
)

var (
	ErrOffline   = errors.New("collector reports room offline")
	ErrLiveEnded = errors.New("collector reports platform live ended")
)

type EmitFunc func(context.Context, model.CreateEventInput) error
type LiveFunc func(context.Context) error

type Runner interface {
	Name() string
	Run(context.Context, model.Room, LiveFunc, EmitFunc) error
}

type Factory interface {
	Create(model.Room) (Runner, error)
}

type PreviewProvider interface {
	Preview(context.Context, model.Room) ([]byte, string, error)
}

type StreamSource struct {
	Protocol     string    `json:"protocol"`
	URL          string    `json:"url"`
	ContentType  string    `json:"content_type,omitempty"`
	ResourceType string    `json:"resource_type,omitempty"`
	CapturedAt   time.Time `json:"captured_at"`
}

type StreamProvider interface {
	Stream(context.Context, model.Room) (StreamSource, error)
}
