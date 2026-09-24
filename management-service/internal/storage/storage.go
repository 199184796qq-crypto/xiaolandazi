package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

type Config struct {
	Driver             string
	LocalRoot          string
	OSSEndpoint        string
	OSSPublicEndpoint  string
	OSSBucket          string
	OSSAccessKeyID     string
	OSSAccessKeySecret string
}

type Store interface {
	Driver() string
	Bucket() string
	Put(ctx context.Context, objectKey string, source io.Reader, contentType string) error
	Open(ctx context.Context, objectKey string) (io.ReadCloser, error)
	Delete(ctx context.Context, objectKey string) error
	SignedURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error)
}

type Registry struct {
	current Store
	stores  map[string]Store
}

func NewRegistry(cfg Config) (*Registry, error) {
	localStore, err := NewLocal(cfg.LocalRoot)
	if err != nil {
		return nil, err
	}
	stores := map[string]Store{
		"local": localStore,
	}

	ossConfigured := strings.TrimSpace(cfg.OSSEndpoint) != "" ||
		strings.TrimSpace(cfg.OSSBucket) != "" ||
		strings.TrimSpace(cfg.OSSAccessKeyID) != "" ||
		cfg.OSSAccessKeySecret != ""
	if ossConfigured {
		ossStore, err := NewOSS(cfg)
		if err != nil {
			return nil, err
		}
		stores["oss"] = ossStore
	}

	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "local"
	}
	current, ok := stores[driver]
	if !ok {
		return nil, fmt.Errorf("storage driver %q is selected but not configured", driver)
	}
	return &Registry{current: current, stores: stores}, nil
}

func (r *Registry) Current() Store {
	return r.current
}

func (r *Registry) For(driver string) (Store, error) {
	driver = strings.ToLower(strings.TrimSpace(driver))
	store, ok := r.stores[driver]
	if !ok {
		return nil, fmt.Errorf("storage driver %q is not available", driver)
	}
	return store, nil
}

func New(cfg Config) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "", "local":
		return NewLocal(cfg.LocalRoot)
	case "oss":
		return NewOSS(cfg)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.Driver)
	}
}
