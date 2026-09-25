package supervisor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInstanceLockExclusiveAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	first, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := acquireInstanceLock(path)
	if second != nil {
		second.Close()
		t.Fatal("duplicate owner admitted")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("duplicate error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatalf("stale file blocks restart: %v", err)
	}
	reopened.Close()
}

func TestNewSupervisorOwnsRunDirectory(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Root: root, RunDir: filepath.Join(root, "run"), LogDir: filepath.Join(root, "logs")}
	first, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := New(cfg); !errors.Is(err, ErrAlreadyRunning) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("duplicate New must fail before reading PID files: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	third.Close()
}

func TestInstanceLockProcessHelper(t *testing.T) {
	path := os.Getenv("LIVE_TEST_INSTANCE_LOCK")
	if path == "" {
		return
	}
	lock, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("LOCK_READY")
	time.Sleep(30 * time.Second)
}

func TestInstanceLockReleasedAfterProcessDeath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.lock")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInstanceLockProcessHelper$")
	child.Env = append(os.Environ(), "LIVE_TEST_INSTANCE_LOCK="+path)
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	ready := make(chan bool, 1)
	go func() {
		s := bufio.NewScanner(out)
		for s.Scan() {
			if s.Text() == "LOCK_READY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child failed to acquire lock")
		}
	case <-ctx.Done():
		t.Fatal("child start timed out")
	}
	if f, err := acquireInstanceLock(path); !errors.Is(err, ErrAlreadyRunning) {
		if f != nil {
			f.Close()
		}
		t.Fatalf("second process admitted: %v", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	f, err := acquireInstanceLock(path)
	if err != nil {
		t.Fatalf("dead process still owns lock: %v", err)
	}
	f.Close()
}
