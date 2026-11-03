// Package sharedfilelock coordinates paid work and cache writes across processes
// that use the same media filesystem. A process exit releases the OS lock; an
// uncertain paid task remains fenced by its separate durable job checkpoint.
package sharedfilelock

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func TryLock(path string) (release func(), acquired bool, err error) {
	if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	acquired, err = tryLock(f)
	if err != nil || !acquired {
		_ = f.Close()
		return nil, acquired, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = unlock(f); _ = f.Close() }) }, true, nil
}

func Lock(ctx context.Context, path string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, acquired, err := TryLock(path)
		if err != nil {
			return nil, err
		}
		if acquired {
			if err = ctx.Err(); err != nil {
				release()
				return nil, err
			}
			return release, nil
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
