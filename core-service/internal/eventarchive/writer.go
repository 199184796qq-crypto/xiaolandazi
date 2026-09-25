package eventarchive

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"livecompanion/core/internal/model"
)

const (
	defaultQueueSize = 8192
	defaultBatchSize = 100
	defaultFlush     = 500 * time.Millisecond
	defaultRetry     = 2 * time.Second
	maxRetry         = time.Minute
)

type Status struct {
	QueueDepth      int       `json:"queue_depth"`
	QueueCapacity   int       `json:"queue_capacity"`
	SpoolFiles      int       `json:"spool_files"`
	SpooledBatches  uint64    `json:"spooled_batches"`
	ReplayedBatches uint64    `json:"replayed_batches"`
	PersistFailures uint64    `json:"persist_failures"`
	DroppedEvents   uint64    `json:"dropped_events"`
	BackoffUntil    time.Time `json:"backoff_until,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

type Writer struct {
	db        *sql.DB
	queue     chan model.RoomEvent
	batchSize int
	flush     time.Duration
	spoolDir  string
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	dropped   atomic.Uint64
	spooled   atomic.Uint64
	replayed  atomic.Uint64
	failures  atomic.Uint64
	sequence  atomic.Uint64

	stateMu      sync.RWMutex
	backoffUntil time.Time
	retryDelay   time.Duration
	lastError    string
}

func New(db *sql.DB) *Writer {
	return NewWithSpool(db, "data/event-archive-spool")
}

func NewWithSpool(db *sql.DB, spoolDir string) *Writer {
	spoolDir = strings.TrimSpace(spoolDir)
	if spoolDir == "" {
		spoolDir = "data/event-archive-spool"
	}
	return &Writer{
		db:         db,
		queue:      make(chan model.RoomEvent, defaultQueueSize),
		batchSize:  defaultBatchSize,
		flush:      defaultFlush,
		spoolDir:   spoolDir,
		retryDelay: defaultRetry,
	}
}

// Start runs the durable archive writer off the collection hot path.
func (w *Writer) Start(parent context.Context) {
	if w == nil || w.db == nil || w.cancel != nil {
		return
	}
	if err := os.MkdirAll(w.spoolDir, 0o755); err != nil {
		w.setError(fmt.Errorf("create event archive spool dir: %w", err))
		log.Printf("event archive spool dir=%q unavailable: %v", w.spoolDir, err)
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(ctx)
	}()
}

// Enqueue is intentionally non-blocking. Redis/in-memory collection must never
// wait on cloud MySQL. A large bounded queue absorbs normal RDS latency spikes.
func (w *Writer) Enqueue(event model.RoomEvent) {
	if w == nil || w.db == nil || event.RoomID <= 0 || event.TenantID <= 0 {
		return
	}
	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "":
		return
	}
	select {
	case w.queue <- cloneEvent(event):
	default:
		dropped := w.dropped.Add(1)
		if dropped == 1 || dropped%1000 == 0 {
			log.Printf("event archive queue full dropped=%d room=%d type=%s", dropped, event.RoomID, event.EventType)
		}
	}
}

func (w *Writer) Close() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
	w.cancel = nil
}

func (w *Writer) run(ctx context.Context) {
	ticker := time.NewTicker(w.flush)
	defer ticker.Stop()
	retryTicker := time.NewTicker(time.Second)
	defer retryTicker.Stop()
	batch := make([]model.RoomEvent, 0, w.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		items := append([]model.RoomEvent(nil), batch...)
		batch = batch[:0]
		if w.inBackoff(time.Now()) {
			if err := w.spoolBatch(items); err != nil {
				w.dropBatch(items, fmt.Errorf("spool during database backoff: %w", err))
			}
			return
		}
		flushCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := w.persistBatch(flushCtx, items); err != nil {
			w.failures.Add(1)
			w.scheduleBackoff(err)
			log.Printf("event archive persist batch size=%d: %v", len(items), err)
			if spoolErr := w.spoolBatch(items); spoolErr != nil {
				w.dropBatch(items, fmt.Errorf("persist failed: %v; spool failed: %w", err, spoolErr))
			}
			return
		}
		w.resetBackoff()
	}

	w.replayAvailable(ctx, 8)
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case event := <-w.queue:
					batch = append(batch, event)
					if len(batch) >= w.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case event := <-w.queue:
			batch = append(batch, event)
			if len(batch) >= w.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-retryTicker.C:
			if !w.inBackoff(time.Now()) {
				w.replayAvailable(ctx, 8)
			}
		}
	}
}

func (w *Writer) spoolBatch(items []model.RoomEvent) error {
	if len(items) == 0 {
		return nil
	}
	if err := os.MkdirAll(w.spoolDir, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(w.spoolDir, ".archive-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		_ = tmp.Close()
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	name := fmt.Sprintf("%020d-%06d.json", time.Now().UTC().UnixNano(), w.sequence.Add(1))
	finalPath := filepath.Join(w.spoolDir, name)
	if err := os.Rename(tmpName, finalPath); err != nil {
		return err
	}
	removeTemp = false
	w.spooled.Add(1)
	return nil
}

func (w *Writer) replayAvailable(ctx context.Context, maxFiles int) {
	if maxFiles <= 0 {
		return
	}
	files, err := w.spoolFiles()
	if err != nil {
		w.setError(fmt.Errorf("list archive spool: %w", err))
		return
	}
	if len(files) == 0 {
		return
	}
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	for _, path := range files {
		if ctx.Err() != nil {
			return
		}
		items, err := readSpoolBatch(path)
		if err != nil {
			w.setError(fmt.Errorf("read archive spool %s: %w", filepath.Base(path), err))
			log.Printf("event archive unreadable spool file=%q: %v", path, err)
			return
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err = w.persistBatch(persistCtx, items)
		cancel()
		if err != nil {
			w.failures.Add(1)
			w.scheduleBackoff(err)
			return
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			w.setError(fmt.Errorf("remove replayed archive spool %s: %w", filepath.Base(path), err))
			return
		}
		w.replayed.Add(1)
		w.resetBackoff()
	}
}

func readSpoolBatch(path string) ([]model.RoomEvent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []model.RoomEvent
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (w *Writer) spoolFiles() ([]string, error) {
	entries, err := os.ReadDir(w.spoolDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		files = append(files, filepath.Join(w.spoolDir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func (w *Writer) inBackoff(now time.Time) bool {
	w.stateMu.RLock()
	until := w.backoffUntil
	w.stateMu.RUnlock()
	return !until.IsZero() && now.Before(until)
}

func (w *Writer) scheduleBackoff(err error) {
	w.stateMu.Lock()
	delay := w.retryDelay
	if delay <= 0 {
		delay = defaultRetry
	}
	w.backoffUntil = time.Now().Add(delay)
	if delay < maxRetry {
		delay *= 2
		if delay > maxRetry {
			delay = maxRetry
		}
	}
	w.retryDelay = delay
	if err != nil {
		w.lastError = err.Error()
	}
	w.stateMu.Unlock()
}

func (w *Writer) resetBackoff() {
	w.stateMu.Lock()
	w.backoffUntil = time.Time{}
	w.retryDelay = defaultRetry
	w.lastError = ""
	w.stateMu.Unlock()
}

func (w *Writer) setError(err error) {
	w.stateMu.Lock()
	if err == nil {
		w.lastError = ""
	} else {
		w.lastError = err.Error()
	}
	w.stateMu.Unlock()
}

func (w *Writer) dropBatch(items []model.RoomEvent, err error) {
	if len(items) > 0 {
		w.dropped.Add(uint64(len(items)))
	}
	w.setError(err)
	log.Printf("event archive local durability failure dropped=%d: %v", len(items), err)
}

func (w *Writer) Status() Status {
	files, _ := w.spoolFiles()
	w.stateMu.RLock()
	status := Status{
		QueueDepth:      len(w.queue),
		QueueCapacity:   cap(w.queue),
		SpoolFiles:      len(files),
		SpooledBatches:  w.spooled.Load(),
		ReplayedBatches: w.replayed.Load(),
		PersistFailures: w.failures.Load(),
		DroppedEvents:   w.dropped.Load(),
		BackoffUntil:    w.backoffUntil,
		LastError:       w.lastError,
	}
	w.stateMu.RUnlock()
	return status
}

func (w *Writer) persistBatch(ctx context.Context, items []model.RoomEvent) error {
	if len(items) == 0 {
		return nil
	}
	filtered, err := w.filterExistingRoomEvents(ctx, items)
	if err != nil {
		return err
	}
	items = filtered
	if len(items) == 0 {
		return nil
	}
	const prefix = `INSERT IGNORE INTO live_room_event_archive (
		tenant_id, room_id, event_id, event_type, user_id, nickname,
		content, payload_json, occurred_at
	) VALUES `
	var sqlText strings.Builder
	sqlText.WriteString(prefix)
	args := make([]any, 0, len(items)*9)
	for index, item := range items {
		if index > 0 {
			sqlText.WriteByte(',')
		}
		sqlText.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?)")
		var payload any
		if len(item.Payload) > 0 && json.Valid(item.Payload) {
			payload = string(item.Payload)
		}
		args = append(args,
			item.TenantID,
			item.RoomID,
			item.ID,
			strings.TrimSpace(item.EventType),
			strings.TrimSpace(item.UserID),
			strings.TrimSpace(item.Nickname),
			item.Content,
			payload,
			item.OccurredAt.UTC(),
		)
	}
	_, err = w.db.ExecContext(ctx, sqlText.String(), args...)
	return err
}

func (w *Writer) filterExistingRoomEvents(ctx context.Context, items []model.RoomEvent) ([]model.RoomEvent, error) {
	type roomKey struct {
		tenantID int64
		roomID   int64
	}
	keys := make([]roomKey, 0)
	seen := make(map[roomKey]struct{})
	for _, item := range items {
		key := roomKey{tenantID: item.TenantID, roomID: item.RoomID}
		if key.tenantID <= 0 || key.roomID <= 0 {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, nil
	}

	var query strings.Builder
	query.WriteString("SELECT tenant_id, id FROM core_rooms WHERE ")
	args := make([]any, 0, len(keys)*2)
	for index, key := range keys {
		if index > 0 {
			query.WriteString(" OR ")
		}
		query.WriteString("(tenant_id=? AND id=?)")
		args = append(args, key.tenantID, key.roomID)
	}
	rows, err := w.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("verify archive room existence: %w", err)
	}
	defer rows.Close()

	alive := make(map[roomKey]struct{}, len(keys))
	for rows.Next() {
		var tenantID, roomID int64
		if err := rows.Scan(&tenantID, &roomID); err != nil {
			return nil, err
		}
		alive[roomKey{tenantID: tenantID, roomID: roomID}] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	filtered := make([]model.RoomEvent, 0, len(items))
	for _, item := range items {
		if _, exists := alive[roomKey{tenantID: item.TenantID, roomID: item.RoomID}]; exists {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func cloneEvent(event model.RoomEvent) model.RoomEvent {
	copy := event
	copy.Payload = append(json.RawMessage(nil), event.Payload...)
	return copy
}
