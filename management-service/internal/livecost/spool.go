package livecost

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Events contain identities/counts only. Never add prompt text, audio URLs or credentials.
type spoolEvent struct {
	Version int       `json:"version"`
	At      time.Time `json:"at"`
	Key     string    `json:"key,omitempty"`
	Request *Request  `json:"request,omitempty"`
	Receipt *Receipt  `json:"receipt,omitempty"`
}
type ReplayResult struct {
	Replayed int `json:"replayed"`
	Pending  int `json:"pending"`
}

func spoolBeginName(r Request) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%s", r.TenantID, r.RoomID, r.Key)))
	return "000-begin-" + hex.EncodeToString(sum[:]) + ".json"
}
func (c *Controller) hasSpoolBegin(r Request) (bool, error) {
	if strings.TrimSpace(c.policy.SpoolDir) == "" {
		return false, nil
	}
	name := filepath.Join(c.policy.SpoolDir, spoolBeginName(r))
	_, err := os.Stat(name)
	if os.IsNotExist(err) {
		_, err = os.Stat(name + ".done")
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
func (c *Controller) writeSpool(event spoolEvent) error {
	if c == nil || strings.TrimSpace(c.policy.SpoolDir) == "" {
		return errors.New("未配置费用降级记录目录")
	}
	root, err := filepath.Abs(c.policy.SpoolDir)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	event.Version = 1
	event.At = c.now()
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(root, ".cost-pending-")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	name := event.At.UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(id[:]) + ".json"
	if event.Request != nil {
		// Link creates the final name atomically and exclusively on both Windows
		// and Linux. Two processes cannot both claim the same degraded request.
		if err = os.Link(tmp, filepath.Join(root, spoolBeginName(*event.Request))); err != nil {
			return err
		}
	} else if err = os.Rename(tmp, filepath.Join(root, name)); err != nil {
		return err
	}
	if directory, e := os.Open(root); e == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

// ReplaySpool performs accounting only; it never reissues a model/TTS request.
// Leave an unresolved request reserved forever until an explicit receipt arrives.
func (c *Controller) ReplaySpool(ctx context.Context) (ReplayResult, error) {
	var out ReplayResult
	if c == nil || c.store == nil || strings.TrimSpace(c.policy.SpoolDir) == "" {
		return out, ErrAccountingUnavailable
	}
	entries, err := os.ReadDir(c.policy.SpoolDir)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var failures []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		out.Pending++
		if out.Pending+out.Replayed > 10000 {
			failures = append(failures, errors.New("费用补记文件过多，剩余记录待下一批处理"))
			break
		}
		path := filepath.Join(c.policy.SpoolDir, entry.Name())
		if err = c.replaySpoolFile(ctx, path); err != nil && !os.IsNotExist(err) {
			if len(failures) < 10 {
				failures = append(failures, err)
			}
			if ctx.Err() != nil {
				break
			}
			continue
		}
		out.Replayed++
		out.Pending--
	}
	return out, errors.Join(failures...)
}

func (c *Controller) replaySpoolFile(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 65537))
	file.Close()
	if readErr != nil {
		return readErr
	}
	if len(data) > 65536 {
		return errors.New("费用补记文件过大")
	}
	var event spoolEvent
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&event); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("费用补记文件含多余内容")
	}
	if event.Version != 1 || event.At.IsZero() || (event.Request == nil) == (event.Receipt == nil) {
		return errors.New("费用补记记录无效")
	}
	if event.Request != nil {
		r, e := NormalizeRequest(*event.Request)
		if e != nil {
			return e
		}
		if r.Priority != PriorityNecessary {
			return errors.New("可选请求不应降级执行")
		}
		_, err = c.store.ReserveLiveCost(ctx, r, c.policy, event.At)
	} else {
		if e := ValidateReceipt(*event.Receipt); e != nil {
			return e
		}
		r := *event.Receipt
		err = c.store.FinishLiveCost(ctx, Scope{TenantID: r.TenantID, RoomID: r.RoomID}, event.Key, r, event.At)
	}
	if err != nil {
		return fmt.Errorf("补记尚未完成: %w", err)
	}
	if event.Request != nil {
		return os.Rename(path, path+".done")
	}
	return os.Remove(path)
}
