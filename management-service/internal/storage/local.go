package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "data/media"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve media root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create media root: %w", err)
	}
	return &Local{root: abs}, nil
}

func (s *Local) Driver() string { return "local" }
func (s *Local) Bucket() string { return "" }

func (s *Local) Put(_ context.Context, objectKey string, source io.Reader, _ string) error {
	path, err := s.resolve(objectKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, source)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Local) Open(_ context.Context, objectKey string) (io.ReadCloser, error) {
	path, err := s.resolve(objectKey)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *Local) Delete(_ context.Context, objectKey string) error {
	path, err := s.resolve(objectKey)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Local) SignedURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", nil
}

func (s *Local) resolve(objectKey string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(objectKey)))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("invalid media object key")
	}
	path := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("media object key escapes storage root")
	}
	return path, nil
}
