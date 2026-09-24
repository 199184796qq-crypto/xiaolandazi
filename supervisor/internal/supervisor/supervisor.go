package supervisor

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxLogSize = 5 * 1024 * 1024

type Supervisor struct {
	cfg       Config
	client    *http.Client
	logger    *log.Logger
	logFile   *os.File
	services  []*serviceState
	closeOnce sync.Once
}

type serviceState struct {
	cfg          ServiceConfig
	mu           sync.Mutex
	cmd          *exec.Cmd
	pid          int
	lastStart    time.Time
	failures     int
	backoffUntil time.Time
}

func New(cfg Config) (*Supervisor, error) {
	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if err := os.MkdirAll(cfg.RunDir, 0o755); err != nil {
		return nil, fmt.Errorf("create run dir: %w", err)
	}

	logPath := filepath.Join(cfg.LogDir, "supervisor.log")
	if err := rotateLog(logPath); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open supervisor log: %w", err)
	}

	runner := &Supervisor{
		cfg:     cfg,
		client:  &http.Client{},
		logFile: logFile,
		logger:  log.New(io.MultiWriter(logFile), "", log.LstdFlags|log.Lmicroseconds),
	}
	for _, item := range cfg.Services {
		runner.services = append(runner.services, &serviceState{cfg: item})
	}
	return runner, nil
}

func (s *Supervisor) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.logFile.Close()
	})
	return err
}

func (s *Supervisor) Run(ctx context.Context) error {
	s.logger.Printf(
		"supervisor started goos=%s root=%s services=%d",
		runtime.GOOS,
		s.cfg.Root,
		len(s.services),
	)

	s.reconcileAll(ctx, true)
	checkTicker := time.NewTicker(s.cfg.CheckInterval)
	defer checkTicker.Stop()
	heartbeatTicker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Printf("supervisor stopping: %v", ctx.Err())
			s.stopAll()
			return nil
		case <-checkTicker.C:
			s.reconcileAll(ctx, false)
		case <-heartbeatTicker.C:
			s.logHeartbeat()
		}
	}
}

func (s *Supervisor) reconcileAll(ctx context.Context, startup bool) {
	var wg sync.WaitGroup
	wg.Add(len(s.services))
	for _, state := range s.services {
		state := state
		go func() {
			defer wg.Done()
			s.reconcileService(ctx, state, startup)
		}()
	}
	wg.Wait()
}

func (s *Supervisor) reconcileService(ctx context.Context, state *serviceState, startup bool) {
	healthy, healthErr := s.probe(ctx, state.cfg.HealthURL)

	state.mu.Lock()
	defer state.mu.Unlock()

	if healthy {
		if state.failures > 0 {
			s.logger.Printf("service recovered name=%s", state.cfg.Name)
		}
		state.failures = 0
		return
	}

	if state.cmd != nil && !state.lastStart.IsZero() && time.Since(state.lastStart) < state.cfg.StartupGrace {
		return
	}

	state.failures++
	if !startup && state.failures < s.cfg.FailureThreshold {
		s.logger.Printf(
			"health check failed name=%s failure=%d/%d err=%v",
			state.cfg.Name,
			state.failures,
			s.cfg.FailureThreshold,
			healthErr,
		)
		return
	}

	if time.Now().Before(state.backoffUntil) {
		return
	}

	reason := "startup health check failed"
	if !startup {
		reason = fmt.Sprintf("health check failed %d times: %v", state.failures, healthErr)
	}
	s.restartLocked(state, reason)
}

func (s *Supervisor) probe(parent context.Context, url string) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, s.cfg.HealthTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8*1024))

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
		return false, fmt.Errorf("http status %d", response.StatusCode)
	}
	return true, nil
}

func (s *Supervisor) restartLocked(state *serviceState, reason string) {
	s.logger.Printf("restart requested name=%s reason=%s", state.cfg.Name, reason)

	if state.cmd != nil && state.pid > 0 {
		if err := terminatePID(state.pid); err != nil {
			s.logger.Printf("terminate owned process failed name=%s pid=%d err=%v", state.cfg.Name, state.pid, err)
		}
		state.cmd = nil
		state.pid = 0
	} else if stalePID := s.readPID(state.cfg.Name); stalePID > 0 {
		if err := terminatePID(stalePID); err != nil {
			s.logger.Printf("terminate stale process failed name=%s pid=%d err=%v", state.cfg.Name, stalePID, err)
		} else {
			s.logger.Printf("terminated stale process name=%s pid=%d", state.cfg.Name, stalePID)
		}
		s.removePID(state.cfg.Name, stalePID)
	}

	if err := s.startLocked(state); err != nil {
		state.backoffUntil = time.Now().Add(s.cfg.RestartBackoff)
		s.logger.Printf("start failed name=%s err=%v", state.cfg.Name, err)
		return
	}
	state.failures = 0
}

func (s *Supervisor) startLocked(state *serviceState) error {
	command := state.cfg.commandForOS(runtime.GOOS)
	if command == "" {
		return fmt.Errorf("no command for %s", runtime.GOOS)
	}

	if err := os.MkdirAll(state.cfg.WorkingDir, 0o755); err != nil {
		return fmt.Errorf("working dir: %w", err)
	}

	stdoutPath := filepath.Join(s.cfg.LogDir, state.cfg.Name+".stdout.log")
	stderrPath := filepath.Join(s.cfg.LogDir, state.cfg.Name+".stderr.log")
	if err := rotateLog(stdoutPath); err != nil {
		return err
	}
	if err := rotateLog(stderrPath); err != nil {
		return err
	}
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		stdoutFile.Close()
		return err
	}

	cmd := exec.Command(command, state.cfg.Args...)
	cmd.Dir = state.cfg.WorkingDir
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = os.Environ()
	for key, value := range state.cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	configureProcess(cmd)

	if err := cmd.Start(); err != nil {
		stdoutFile.Close()
		stderrFile.Close()
		return err
	}

	state.cmd = cmd
	state.pid = cmd.Process.Pid
	state.lastStart = time.Now()
	state.backoffUntil = state.lastStart.Add(s.cfg.RestartBackoff)
	if err := s.writePID(state.cfg.Name, state.pid); err != nil {
		s.logger.Printf("write pid failed name=%s pid=%d err=%v", state.cfg.Name, state.pid, err)
	}
	s.logger.Printf("started name=%s pid=%d command=%s", state.cfg.Name, state.pid, command)

	go s.waitForProcess(state, cmd, stdoutFile, stderrFile)
	return nil
}

func (s *Supervisor) waitForProcess(state *serviceState, cmd *exec.Cmd, stdoutFile, stderrFile *os.File) {
	err := cmd.Wait()
	_ = stdoutFile.Close()
	_ = stderrFile.Close()

	state.mu.Lock()
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	if state.cmd == cmd {
		state.cmd = nil
		state.pid = 0
	}
	state.mu.Unlock()

	s.removePID(state.cfg.Name, pid)
	if err != nil {
		s.logger.Printf("process exited name=%s pid=%d err=%v", state.cfg.Name, pid, err)
	} else {
		s.logger.Printf("process exited name=%s pid=%d", state.cfg.Name, pid)
	}
}

func (s *Supervisor) stopAll() {
	for _, state := range s.services {
		state.mu.Lock()
		pid := state.pid
		state.cmd = nil
		state.pid = 0
		state.mu.Unlock()

		if pid <= 0 {
			continue
		}
		if err := terminatePID(pid); err != nil {
			s.logger.Printf("shutdown terminate failed name=%s pid=%d err=%v", state.cfg.Name, pid, err)
		} else {
			s.logger.Printf("shutdown terminated name=%s pid=%d", state.cfg.Name, pid)
		}
		s.removePID(state.cfg.Name, pid)
	}
}

func (s *Supervisor) logHeartbeat() {
	parts := make([]string, 0, len(s.services))
	for _, state := range s.services {
		state.mu.Lock()
		pid := state.pid
		failures := state.failures
		state.mu.Unlock()
		parts = append(parts, fmt.Sprintf("%s(pid=%d failures=%d)", state.cfg.Name, pid, failures))
	}
	s.logger.Printf("heartbeat %s", strings.Join(parts, " "))
}

func (s *Supervisor) pidPath(name string) string {
	return filepath.Join(s.cfg.RunDir, name+".pid")
}

func (s *Supervisor) writePID(name string, pid int) error {
	return os.WriteFile(s.pidPath(name), []byte(strconv.Itoa(pid)), 0o644)
}

func (s *Supervisor) readPID(name string) int {
	data, err := os.ReadFile(s.pidPath(name))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func (s *Supervisor) removePID(name string, expectedPID int) {
	if expectedPID > 0 {
		current := s.readPID(name)
		if current != 0 && current != expectedPID {
			return
		}
	}
	_ = os.Remove(s.pidPath(name))
}

func rotateLog(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() <= maxLogSize {
		return nil
	}
	backup := path + ".1"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("rotate log %s: %w", path, err)
	}
	return nil
}
