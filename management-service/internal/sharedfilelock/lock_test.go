package sharedfilelock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedFileLockProcessHelper(t *testing.T) {
	path := os.Getenv("SHARED_LOCK_HELPER_PATH")
	if path == "" {
		return
	}
	release, err := Lock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.WriteFile(path+".acquired", []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		if _, err := os.Stat(path + ".release"); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("parent did not release helper")
}
func TestOSLockSerializesSeparateProcessesAndHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSharedFileLockProcessHelper$")
	cmd.Env = append(os.Environ(), "SHARED_LOCK_HELPER_PATH="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(path+".release", nil, 0600); _ = cmd.Wait() })
	ready := false
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		if _, err := os.Stat(path + ".acquired"); err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("child did not acquire lock")
	}
	if release, ok, err := TryLock(path); err != nil || ok {
		if release != nil {
			release()
		}
		t.Fatal("second process entered occupied gate", ok, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := Lock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked wait ignored deadline", err)
	}
	if err := os.WriteFile(path+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	release, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	release()
	release()
	ctx2, stop := context.WithCancel(t.Context())
	stop()
	if _, err := Lock(ctx2, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
