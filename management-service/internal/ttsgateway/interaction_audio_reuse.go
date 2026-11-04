package ttsgateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/voiceasset"
)

const UsageAudioReuse = "existing_audio_reuse"

const PaidSynthesisDrainTimeout = 90 * time.Second

type InteractionAudioPolicy struct {
	Enabled   bool
	Retention time.Duration
}

type interactionAudioScope struct {
	TenantID, PlanID int64
	Policy           func(context.Context) (InteractionAudioPolicy, error)
	Guard            func(context.Context) error
}
type interactionAudioKey struct{}

// Scope comes from the running program, never from model output. Fresh content,
// P0 and validity checks remain the caller's responsibility on every event.
func WithInteractionAudioReuse(ctx context.Context, tenant, plan int64, policy func(context.Context) (InteractionAudioPolicy, error), guard func(context.Context) error) context.Context {
	return context.WithValue(ctx, interactionAudioKey{}, interactionAudioScope{tenant, plan, policy, guard})
}

type interactionAudioEntry struct {
	Stamp                *voiceasset.Stamp `json:"voice_cache_stamp,omitempty"`
	Version              int               `json:"version"`
	Key                  string            `json:"key"`
	CreatedAt, ExpiresAt time.Time
	Audio                SynthesizeResponse `json:"audio"`
}

// Metadata survives process restarts on the shared media filesystem. We retain
// the original supplier/archival URL, not a new copy or a renewed signed URL.
// Each hit must read actual audio bytes. Missing/expired/unrecoverable sources
// fall back to synthesis, following the user's availability-first instruction.
type InteractionAudioCache struct {
	epochs voiceasset.EpochReader
	root   string
	client *http.Client
	now    func() time.Time
	mu     sync.Mutex
	rescue map[string]interactionAudioEntry
}

func (c *InteractionAudioCache) WithEpochReader(reader voiceasset.EpochReader) *InteractionAudioCache {
	c.epochs = reader
	return c
}
func (c *InteractionAudioCache) cleared(ctx context.Context, entry interactionAudioEntry) bool {
	return c.epochs != nil && entry.Stamp != nil && errors.Is(c.epochs.CheckVoiceCacheStamp(ctx, *entry.Stamp), voiceasset.ErrCleared)
}

func NewInteractionAudioCache(mediaRoot, realm string) *InteractionAudioCache {
	return &InteractionAudioCache{
		root:   filepath.Join(mediaRoot, "interaction-audio-reuse-v1", digestSynthesisText(realm)),
		client: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now:    time.Now, rescue: make(map[string]interactionAudioEntry),
	}
}

// Configure once before starting the worker. Other gateways and unscoped calls
// keep their original behavior; mainline generation never enters this cache.
func (g *Gateway) WithInteractionAudioCache(cache *InteractionAudioCache) *Gateway {
	g.interactionAudioCache = cache
	return g
}

// Only expired metadata owned by this cache is removed. Supplier audio, paid
// archives, lock files and unreadable evidence are never deleted here.
func (c *InteractionAudioCache) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		c.prune(work)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *InteractionAudioCache) prune(ctx context.Context) {
	c.mu.Lock()
	for key, entry := range c.rescue {
		if !c.now().Before(entry.ExpiresAt) || c.cleared(ctx, entry) {
			delete(c.rescue, key)
		}
	}
	c.mu.Unlock()
	dir, err := os.Open(c.root)
	if err != nil {
		return
	}
	defer dir.Close()
	for {
		files, readErr := dir.ReadDir(256)
		for _, file := range files {
			if ctx.Err() != nil {
				return
			}
			name := file.Name()
			if !file.Type().IsRegular() || !strings.HasSuffix(name, ".json") || len(name) != 69 {
				continue
			}
			key := strings.TrimSuffix(name, ".json")
			if strings.IndexFunc(key, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef", r) }) >= 0 {
				continue
			}
			release, acquired, e := sharedfilelock.TryLock(filepath.Join(c.root, "locks", key+".lock"))
			if e != nil || !acquired {
				continue
			}
			path := filepath.Join(c.root, name)
			entry, ok, e := c.read(path, key)
			if e == nil && ok && entry.Version == 1 && entry.Key == key && (!c.now().Before(entry.ExpiresAt) || c.cleared(ctx, entry)) {
				_ = os.Remove(path)
			}
			release()
		}
		if readErr != nil {
			return
		}
	}
}

func (c *InteractionAudioCache) synthesize(ctx context.Context, p Provider, request SynthesizeRequest, account string, next func(context.Context) (SynthesizeResponse, error)) (SynthesizeResponse, error) {
	s, ok := ctx.Value(interactionAudioKey{}).(interactionAudioScope)
	if c == nil || !ok || s.TenantID <= 0 || s.PlanID <= 0 || s.Policy == nil || request.LongText != nil || request.ParagraphPath != "" {
		return next(ctx)
	}
	guard := func() error {
		if err := voiceasset.Check(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.Guard != nil {
			return s.Guard(ctx)
		}
		return nil
	}
	if err := guard(); err != nil {
		return SynthesizeResponse{}, notSent(err)
	}
	policy, err := s.Policy(ctx)
	if err != nil {
		return SynthesizeResponse{}, notSent(errors.New("互动声音缓存策略读取失败，未购买"))
	}
	identity := interactionProviderIdentity(p)
	if !policy.Enabled || identity == "" {
		return next(ctx)
	}
	if policy.Retention < time.Hour || policy.Retention > 168*time.Hour {
		return SynthesizeResponse{}, notSent(errors.New("互动声音缓存保留时长无效，未购买"))
	}
	raw, _ := json.Marshal([]any{"interaction-audio-v1", s.TenantID, s.PlanID, account, identity, request.Provider, request.Model, request.VoiceID, request.Rate, request.Instruction, request.Text})
	key := digestSynthesisText(string(raw))
	if stamp, ok := voiceasset.FromContext(ctx); ok {
		key = stamp.Key(key)
	}
	path := filepath.Join(c.root, key+".json")
	release, err := sharedfilelock.Lock(ctx, filepath.Join(c.root, "locks", key+".lock"))
	if err != nil {
		return SynthesizeResponse{}, notSent(errors.New("互动声音共享锁不可用，未购买"))
	}
	defer release()
	// Waiting for another room/process cannot renew validity or bypass a newly
	// disabled interaction/cache policy.
	if err = guard(); err != nil {
		return SynthesizeResponse{}, notSent(err)
	}
	policy, err = s.Policy(ctx)
	if err != nil {
		return SynthesizeResponse{}, notSent(errors.New("互动声音缓存策略复核失败，未购买"))
	}
	if !policy.Enabled {
		return next(ctx)
	}
	if policy.Retention < time.Hour || policy.Retention > 168*time.Hour {
		return SynthesizeResponse{}, notSent(errors.New("互动声音缓存保留时长无效，未购买"))
	}
	entry, found, err := c.read(path, key)
	if err != nil {
		// Preserve unreadable metadata before the availability-first recovery.
		// A disk failure never authorizes reuse of unverified old audio.
		if _, statErr := os.Lstat(path); statErr == nil {
			if renameErr := os.Rename(path, path+".unreadable-"+strconv.FormatInt(c.now().UnixNano(), 10)); renameErr != nil {
				return SynthesizeResponse{}, notSent(errors.New("互动声音缓存损坏且无法保留原件，未购买"))
			}
		}
		found = false
		log.Printf("interaction audio metadata unreadable; preserved for recovery tenant=%d plan=%d key=%s", s.TenantID, s.PlanID, key)
	}
	if found {
		now := c.now()
		until := entry.ExpiresAt
		if end := entry.CreatedAt.Add(policy.Retention); end.Before(until) {
			until = end
		}
		if entry.Version == 1 && entry.Key == key && !entry.CreatedAt.After(now) && now.Add(10*time.Second).Before(until) && c.readable(ctx, entry.Audio.AudioURL) {
			if err = guard(); err != nil {
				return SynthesizeResponse{}, notSent(err)
			}
			latest, e := s.Policy(ctx)
			if e != nil {
				return SynthesizeResponse{}, notSent(errors.New("互动声音缓存策略复核失败，未购买"))
			}
			if !latest.Enabled {
				return next(ctx)
			}
			if latest.Retention < time.Hour || latest.Retention > 168*time.Hour {
				return SynthesizeResponse{}, notSent(errors.New("互动声音缓存保留时长无效，未购买"))
			}
			policy = latest
			if c.now().Add(10*time.Second).Before(entry.CreatedAt.Add(policy.Retention)) && c.now().Add(10*time.Second).Before(entry.ExpiresAt) {
				audio := entry.Audio
				// Original measurements belong only to the original paid request.
				audio.Usage, audio.ProviderRequestID = SynthesisUsage{}, ""
				audio.UsageSource, audio.AudioReused = UsageAudioReuse, true
				log.Printf("interaction audio reused tenant=%d plan=%d key=%s", s.TenantID, s.PlanID, key)
				return audio, nil
			}
		}
		log.Printf("interaction audio unavailable; synthesis fallback tenant=%d plan=%d key=%s", s.TenantID, s.PlanID, key)
	}
	if err = guard(); err != nil {
		return SynthesizeResponse{}, notSent(err)
	}
	audio, err := next(ctx)
	if err != nil || strings.TrimSpace(audio.AudioURL) == "" {
		return audio, err
	}
	// Receipt/accounting is already complete. A clear cannot cancel that cost,
	// but it must prevent the late paid result from reviving a reusable entry.
	if err := voiceasset.Check(context.WithoutCancel(ctx)); err != nil {
		return audio, err
	}
	now := c.now()
	entry = interactionAudioEntry{Version: 1, Key: key, CreatedAt: now, ExpiresAt: interactionAudioExpiry(audio.AudioURL, now.Add(policy.Retention), now), Audio: audio}
	if stamp, ok := voiceasset.FromContext(ctx); ok {
		entry.Stamp = &stamp
	}
	// A late paid result is retained even if its original event has expired.
	// A failed cache write must never discard a successful paid synthesis.
	if err := writeInteractionAudioEntry(path, entry); err != nil {
		c.mu.Lock()
		if len(c.rescue) < 256 {
			c.rescue[key] = entry
		}
		c.mu.Unlock()
		log.Printf("interaction audio cache persistence failed; paid result retained tenant=%d plan=%d key=%s", s.TenantID, s.PlanID, key)
	} else {
		c.mu.Lock()
		delete(c.rescue, key)
		c.mu.Unlock()
	}
	return audio, nil
}

func (c *InteractionAudioCache) read(path, key string) (interactionAudioEntry, bool, error) {
	c.mu.Lock()
	saved, ok := c.rescue[key]
	c.mu.Unlock()
	if ok {
		return saved, true, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return interactionAudioEntry{}, false, nil
	}
	if err != nil {
		return interactionAudioEntry{}, false, err
	}
	defer f.Close()
	var entry interactionAudioEntry
	err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&entry)
	return entry, err == nil, err
}

func writeInteractionAudioEntry(path string, entry interactionAudioEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".audio-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		err = json.NewEncoder(f).Encode(entry)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	return err
}

// Only stamped metadata can be attributed to a selected plan/voice. Supplier
// URLs are read for identity here, never fetched, logged or deleted.
func (c *InteractionAudioCache) ScopedVoices(ctx context.Context, scope voiceasset.Scope) ([]map[string]string, error) {
	files, e := os.ReadDir(c.root)
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	out := []map[string]string{}
	for _, file := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !file.Type().IsRegular() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(file.Name(), ".json")
		if !voiceasset.DigestValid(key) {
			continue
		}
		entry, ok, e := c.read(filepath.Join(c.root, file.Name()), key)
		if e != nil {
			return nil, e
		}
		if !ok || entry.Stamp == nil {
			continue
		}
		st := entry.Stamp.Scope
		if st.Validate() != nil {
			return nil, voiceasset.ErrConflict
		}
		if st.Tenant != scope.Tenant || st.Plan != scope.Plan || st.Category != scope.Category || st.Voice == "" || seen[st.Voice] {
			continue
		}
		seen[st.Voice] = true
		out = append(out, map[string]string{"key": st.Voice, "label": entry.Audio.VoiceID + " · " + st.Voice[:12]})
	}
	return out, nil
}
func (c *InteractionAudioCache) ClearScopedFiles(ctx context.Context, j voiceasset.Job) error {
	files, e := os.ReadDir(c.root)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, file := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		key := strings.TrimSuffix(file.Name(), ".json")
		if !file.Type().IsRegular() || !strings.HasSuffix(file.Name(), ".json") || !voiceasset.DigestValid(key) {
			continue
		}
		release, ok, e := sharedfilelock.TryLock(filepath.Join(c.root, "locks", key+".lock"))
		if e != nil {
			return e
		}
		if !ok {
			return voiceasset.ErrBusy
		}
		path := filepath.Join(c.root, file.Name())
		entry, found, e := c.read(path, key)
		if e == nil && found && entry.Stamp != nil {
			scope := entry.Stamp.Scope
			if scope.Validate() != nil {
				release()
				return voiceasset.ErrConflict
			}
			match := scope.Tenant == j.Scope.Tenant && scope.Plan == j.Scope.Plan && scope.Category == j.Scope.Category && (j.Scope.Voice == "" || scope.Voice == j.Scope.Voice)
			remove := j.Mode == "local_only" && !entry.CreatedAt.After(j.Created)
			if match && j.Mode != "local_only" && c.epochs != nil {
				check := c.epochs.CheckVoiceCacheStamp(ctx, *entry.Stamp)
				if check != nil && !errors.Is(check, voiceasset.ErrCleared) {
					release()
					return check
				}
				remove = errors.Is(check, voiceasset.ErrCleared)
			}
			if match && remove {
				e = os.Remove(path)
			}
		}
		release()
		if e != nil {
			return e
		}
	}
	return nil
}

func interactionAudioExpiry(address string, until, now time.Time) time.Time {
	u, err := url.Parse(address)
	if err != nil {
		return now
	}
	known := false
	for _, name := range []string{"Expires", "expires", "expires_at"} {
		if n, e := strconv.ParseInt(u.Query().Get(name), 10, 64); e == nil && n > 0 {
			known = true
			if expiry := time.Unix(n, 0); expiry.Before(until) {
				until = expiry
			}
		}
	}
	// Unknown signature formats cannot be assumed valid for the full policy TTL.
	if !known && u.RawQuery != "" && now.Add(2*time.Minute).Before(until) {
		until = now.Add(2 * time.Minute)
	}
	return until
}

func (c *InteractionAudioCache) readable(ctx context.Context, address string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return false
	}
	request.Header.Set("Range", "bytes=0-63")
	response, err := c.client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64))
	if err != nil || len(raw) < 12 {
		return false
	}
	return string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WAVE" || string(raw[:4]) == "fLaC" || string(raw[:4]) == "OggS" || string(raw[:3]) == "ID3" || raw[0] == 0xff && raw[1]&0xe0 == 0xe0
}
