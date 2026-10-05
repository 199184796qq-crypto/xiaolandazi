package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const captureGrantLifetime = 120 * time.Second

type controlLease struct {
	MAC         string
	ClientID    string
	RequestID   string
	DeviceID    int64
	TenantID    int64
	BindingRole string
	StartedAt   time.Time
}

type captureGrant struct {
	MAC       string `json:"mac"`
	ClientID  string `json:"client"`
	RequestID string `json:"request_id"`
	DeviceID  int64  `json:"device_id"`
	TenantID  int64  `json:"tenant_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type captureResponse struct {
	Accepted     bool   `json:"accepted"`
	RequestID    string `json:"request_id"`
	EventID      int64  `json:"event_id"`
	BillingMode  string `json:"billing_mode"`
	ChargedBeans int64  `json:"charged_beans"`
}

type captureAttempt struct {
	grant    captureGrant
	running  bool
	accepted bool
	digest   [32]byte
	response captureResponse
	ctx      context.Context
	cancel   context.CancelFunc
}

// Registry enforces limits across reconnects and concurrent WebSockets, rather
// than relying on an individual session's state. No tenant supplied by a device
// is used as authority: all leases come from provision().
type controlRegistry struct {
	mu       sync.Mutex
	active   map[string]controlLease
	recent   map[string][]time.Time
	captures map[string]*captureAttempt
	seen     map[string]time.Time
}

func newControlRegistry() *controlRegistry {
	return &controlRegistry{active: make(map[string]controlLease), recent: make(map[string][]time.Time), captures: make(map[string]*captureAttempt), seen: make(map[string]time.Time)}
}

var validCommandRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func (registry *controlRegistry) acquire(mac, client, requestID string, p provisioning, now time.Time) (controlLease, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if p.TenantID <= 0 || p.DeviceID <= 0 || (p.State != "claimed" && p.State != "bound") {
		return controlLease{}, errors.New("请先在小蓝搭子添加设备，再使用语音指令")
	}
	if requestID == "" {
		requestID = randomID()
	}
	if !validCommandRequestID.MatchString(requestID) {
		return controlLease{}, errors.New("指令编号无效，请更新设备固件")
	}
	if _, exists := registry.active[mac]; exists {
		return controlLease{}, errors.New("上一条指令正在处理，请稍后再说")
	}
	if len(registry.active) >= 64 {
		return controlLease{}, errors.New("语音服务繁忙，请稍后再说")
	}
	// Clean expired entries, so rejected/unknown identities cannot grow this map.
	for key, times := range registry.recent {
		if len(times) == 0 || now.Sub(times[len(times)-1]) >= time.Minute {
			delete(registry.recent, key)
		}
	}
	for key, attempt := range registry.captures {
		if now.Unix() >= attempt.grant.ExpiresAt {
			if attempt.cancel != nil {
				attempt.cancel()
			}
			delete(registry.captures, key)
		}
	}
	for key, expires := range registry.seen {
		if !now.Before(expires) {
			delete(registry.seen, key)
		}
	}
	if _, duplicate := registry.seen[captureKey(mac, requestID)]; duplicate {
		return controlLease{}, errors.New("这条指令已经处理过，请重新发起")
	}
	var recent []time.Time
	for _, timestamp := range registry.recent[mac] {
		if now.Sub(timestamp) < time.Minute {
			recent = append(recent, timestamp)
		}
	}
	if len(recent) >= 10 || (len(recent) > 0 && now.Sub(recent[len(recent)-1]) < 2*time.Second) {
		return controlLease{}, errors.New("指令太频繁，请稍后再试")
	}
	registry.recent[mac] = append(recent, now)
	lease := controlLease{MAC: mac, ClientID: client, RequestID: requestID, DeviceID: p.DeviceID, TenantID: p.TenantID, BindingRole: p.BindingRole, StartedAt: now}
	registry.active[mac] = lease
	registry.seen[captureKey(mac, requestID)] = now.Add(10 * time.Minute)
	return lease, nil
}

func (registry *controlRegistry) release(lease controlLease) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if current, exists := registry.active[lease.MAC]; exists && current.RequestID == lease.RequestID {
		delete(registry.active, lease.MAC)
		if attempt := registry.captures[captureKey(lease.MAC, lease.RequestID)]; attempt != nil && attempt.cancel != nil {
			attempt.cancel()
		}
	}
}

func captureKey(mac, requestID string) string { return mac + "\n" + requestID }

func (g *gateway) captureURL(lease controlLease, now time.Time) (string, error) {
	endpoint, err := url.Parse(g.cfg.publicWSURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "ws" && endpoint.Scheme != "wss") || endpoint.User != nil {
		return "", errors.New("设备拍照上传地址未配置")
	}
	grant := captureGrant{MAC: lease.MAC, ClientID: lease.ClientID, RequestID: lease.RequestID, DeviceID: lease.DeviceID, TenantID: lease.TenantID, ExpiresAt: now.Add(captureGrantLifetime).Unix()}
	raw, _ := json.Marshal(grant)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(g.cfg.secret))
	_, _ = mac.Write([]byte("xiaolan-capture-v1\n" + payload))
	signed := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if endpoint.Scheme == "wss" {
		endpoint.Scheme = "https"
	} else {
		endpoint.Scheme = "http"
	}
	endpoint.Path, endpoint.RawPath, endpoint.Fragment = "/xiaozhi/v1/capture", "", ""
	endpoint.RawQuery = url.Values{"grant": []string{signed}}.Encode()
	g.controls.mu.Lock()
	defer g.controls.mu.Unlock()
	if current, exists := g.controls.active[lease.MAC]; !exists || current != lease {
		return "", errors.New("拍照指令已取消")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(grant.ExpiresAt, 0))
	g.controls.captures[captureKey(lease.MAC, lease.RequestID)] = &captureAttempt{grant: grant, ctx: ctx, cancel: cancel}
	return endpoint.String(), nil
}

func (g *gateway) parseCaptureGrant(signed string, now time.Time) (captureGrant, error) {
	if len(signed) > 2048 {
		return captureGrant{}, errors.New("invalid grant")
	}
	parts := strings.Split(signed, ".")
	if len(parts) != 2 {
		return captureGrant{}, errors.New("invalid grant")
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return captureGrant{}, errors.New("invalid grant")
	}
	mac := hmac.New(sha256.New, []byte(g.cfg.secret))
	_, _ = mac.Write([]byte("xiaolan-capture-v1\n" + parts[0]))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return captureGrant{}, errors.New("invalid grant")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return captureGrant{}, errors.New("invalid grant")
	}
	var grant captureGrant
	if json.Unmarshal(raw, &grant) != nil || grant.MAC == "" || grant.ClientID == "" || grant.RequestID == "" || grant.DeviceID <= 0 || grant.TenantID <= 0 {
		return captureGrant{}, errors.New("invalid grant")
	}
	if grant.ExpiresAt <= now.Unix() || grant.ExpiresAt > now.Add(captureGrantLifetime).Unix() {
		return captureGrant{}, errors.New("expired grant")
	}
	return grant, nil
}

func (registry *controlRegistry) beginCapture(grant captureGrant, digest [32]byte) (*captureResponse, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	lease, active := registry.active[grant.MAC]
	attempt := registry.captures[captureKey(grant.MAC, grant.RequestID)]
	if !active || lease.RequestID != grant.RequestID || lease.ClientID != grant.ClientID || lease.TenantID != grant.TenantID || lease.DeviceID != grant.DeviceID || attempt == nil || attempt.grant != grant {
		return nil, errors.New("拍照授权已取消或不属于当前设备")
	}
	if attempt.running {
		return nil, errors.New("照片正在上传，请稍后重试")
	}
	if attempt.accepted {
		if attempt.digest != digest {
			return nil, errors.New("此拍照授权已用于另一张照片")
		}
		response := attempt.response
		return &response, nil
	}
	attempt.running, attempt.digest = true, digest
	return nil, nil
}

func (registry *controlRegistry) finishCapture(grant captureGrant, response captureResponse, success bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if attempt := registry.captures[captureKey(grant.MAC, grant.RequestID)]; attempt != nil && attempt.grant == grant {
		attempt.running = false
		if success {
			attempt.accepted, attempt.response = true, response
		}
	}
}

func (registry *controlRegistry) captureAccepted(lease controlLease) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	attempt := registry.captures[captureKey(lease.MAC, lease.RequestID)]
	return attempt != nil && attempt.accepted && attempt.grant.TenantID == lease.TenantID
}

func (registry *controlRegistry) captureContext(grant captureGrant) context.Context {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if attempt := registry.captures[captureKey(grant.MAC, grant.RequestID)]; attempt != nil && attempt.grant == grant {
		return attempt.ctx
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
