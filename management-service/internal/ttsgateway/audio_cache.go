package ttsgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Only the same-host synthesis/archive/Core bridge reads these temporary WAVs.
// Formal audio and previews are subsequently archived using existing asset storage.
// No extra disk files or permanent OSS objects are created by the provider.
type cachedTencentAudio struct {
	wav    []byte
	expiry time.Time
}

var tencentAudio = struct {
	sync.Mutex
	items map[string]cachedTencentAudio
	bytes int
}{items: map[string]cachedTencentAudio{}}

const tencentAudioMemoryLimit = 128 << 20

func pruneTencentAudio(now time.Time) {
	for k, v := range tencentAudio.items {
		if !now.Before(v.expiry) {
			delete(tencentAudio.items, k)
			tencentAudio.bytes -= len(v.wav)
		}
	}
}
func cacheTencentWAV(ctx context.Context, raw []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(key[:])
	now := time.Now()
	tencentAudio.Lock()
	defer tencentAudio.Unlock()
	pruneTencentAudio(now)
	if len(raw) == 0 || len(raw) > 32<<20 || tencentAudio.bytes+len(raw) > tencentAudioMemoryLimit {
		return "", errors.New("腾讯临时声音缓冲已满，请稍后再试")
	}
	expiry := now.Add(time.Hour)
	tencentAudio.items[id] = cachedTencentAudio{wav: raw, expiry: expiry}
	tencentAudio.bytes += len(raw)
	time.AfterFunc(time.Hour, func() { tencentAudio.Lock(); defer tencentAudio.Unlock(); pruneTencentAudio(time.Now()) })
	return "http://127.0.0.1:8080/internal/v1/tts-audio/" + id + "?Expires=" + strconv.FormatInt(expiry.Unix(), 10), nil
}

// Archive readers no longer need the temporary copy. Interactive Core/FAQ URLs
// retain their bounded lifetime because Core may probe then stream the same URL.
func ReleaseTemporaryAudio(address string) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "http" || u.Host != "127.0.0.1:8080" {
		return
	}
	const prefix = "/internal/v1/tts-audio/"
	if !strings.HasPrefix(u.Path, prefix) {
		return
	}
	id := strings.TrimPrefix(u.Path, prefix)
	tencentAudio.Lock()
	defer tencentAudio.Unlock()
	if v, ok := tencentAudio.items[id]; ok {
		delete(tencentAudio.items, id)
		tencentAudio.bytes -= len(v.wav)
	}
}
func ServeTencentAudio(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("audioID")
	if len(id) != 64 {
		http.NotFound(w, r)
		return
	}
	tencentAudio.Lock()
	pruneTencentAudio(time.Now())
	item, ok := tencentAudio.items[id]
	tencentAudio.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "generated.wav", time.Time{}, bytes.NewReader(item.wav))
}
