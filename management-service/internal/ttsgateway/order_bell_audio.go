package ttsgateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const orderAudioRate = 24000
const orderAudioMaxBytes = 24 << 20
const orderBellGapMS = 100

var ErrOrderBellMissing = errors.New("order bell asset unavailable")

// OrderBellAudio describes one WAV containing the real bell, silence and speech.
// The URL uses the existing private, expiring audio buffer; it is never a second
// speech task or an input to a provider, scheduler or billing ledger.
type OrderBellAudio struct {
	AudioURL       string
	BellDurationMS int
	GapDurationMS  int
	DurationMS     int
}

type orderBellComposer struct {
	mu      sync.Mutex
	bellPCM []byte
	load    func() ([]byte, error)
	ffmpeg  string
	client  *http.Client
	publish func(context.Context, []byte) (string, error)
}

var defaultOrderBellComposer = orderBellComposer{load: readOrderBellAsset, client: &http.Client{Timeout: 8 * time.Second}, publish: cacheTencentWAV}

// PrependOrderBell is local audio processing after a successful synthesis.
// The source speech remains untouched and is still usable on any failure.
func PrependOrderBell(ctx context.Context, speechURL string) (OrderBellAudio, error) {
	return defaultOrderBellComposer.compose(ctx, speechURL)
}

func readOrderBellAsset() ([]byte, error) {
	paths := []string{}
	if dir := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_PROMPT_DIR")); dir != "" {
		// The explicit installation is authoritative, as it is for prompt files.
		paths = append(paths, filepath.Join(dir, "..", "..", "audio", "order-bell.wav"))
	} else {
		cwd, _ := os.Getwd()
		for i := 0; i < 8 && cwd != ""; i++ {
			paths = append(paths, filepath.Join(cwd, "configs", "audio", "order-bell.wav"))
			parent := filepath.Dir(cwd)
			if parent == cwd {
				break
			}
			cwd = parent
		}
		exe, _ := os.Executable()
		paths = append(paths, filepath.Join(filepath.Dir(exe), "..", "configs", "audio", "order-bell.wav"))
	}
	for _, path := range paths {
		file, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, ErrOrderBellMissing
		}
		raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		_ = file.Close()
		if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
			return nil, ErrOrderBellMissing
		}
		return raw, nil
	}
	return nil, ErrOrderBellMissing
}

func (c *orderBellComposer) bell(ctx context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(c.bellPCM) > 0 {
		return c.bellPCM, nil
	}
	raw, err := c.load()
	if err != nil {
		return nil, ErrOrderBellMissing
	}
	pcm, err := c.normalize(ctx, raw)
	if err != nil || len(pcm) > 5*orderAudioRate*2 {
		return nil, ErrOrderBellMissing
	}
	// Cache only successful preparation. A canceled first request or a missing
	// asset must not poison every later order until the service restarts.
	c.bellPCM = pcm
	return pcm, nil
}

func (c *orderBellComposer) compose(parent context.Context, speechURL string) (OrderBellAudio, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	bell, err := c.bell(ctx)
	if err != nil {
		return OrderBellAudio{}, err
	}
	u, err := url.Parse(speechURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return OrderBellAudio{}, errors.New("invalid order speech audio URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, speechURL, nil)
	if err != nil {
		return OrderBellAudio{}, errors.New("invalid order speech audio request")
	}
	res, err := c.client.Do(req)
	if err != nil {
		// Never log a provider's signed URL, including URL errors.
		return OrderBellAudio{}, errors.New("order speech audio download failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return OrderBellAudio{}, fmt.Errorf("order speech audio HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, orderAudioMaxBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > orderAudioMaxBytes {
		return OrderBellAudio{}, errors.New("order speech audio is empty, oversized or unreadable")
	}
	speech, err := c.normalize(ctx, raw)
	if err != nil {
		return OrderBellAudio{}, err
	}
	gapBytes := orderAudioRate * 2 * orderBellGapMS / 1000
	if len(bell)+gapBytes+len(speech) > orderAudioMaxBytes {
		return OrderBellAudio{}, errors.New("combined order audio exceeds buffer limit")
	}
	pcm := make([]byte, len(bell)+gapBytes+len(speech))
	copy(pcm, bell)
	copy(pcm[len(bell)+gapBytes:], speech)
	if err := ctx.Err(); err != nil {
		return OrderBellAudio{}, err
	}
	address, err := c.publish(ctx, orderPCMToWAV(pcm))
	if err != nil {
		return OrderBellAudio{}, errors.New("combined order audio publication failed")
	}
	if strings.TrimSpace(address) == "" {
		return OrderBellAudio{}, errors.New("combined order audio URL is empty")
	}
	return OrderBellAudio{AudioURL: address, BellDurationMS: len(bell) * 1000 / (orderAudioRate * 2), GapDurationMS: orderBellGapMS, DurationMS: len(pcm) * 1000 / (orderAudioRate * 2)}, nil
}

type boundedOrderPCM struct{ bytes.Buffer }

func (b *boundedOrderPCM) Write(p []byte) (int, error) {
	if b.Len()+len(p) > orderAudioMaxBytes {
		return 0, errors.New("decoded order audio exceeds buffer limit")
	}
	return b.Buffer.Write(p)
}

func (c *orderBellComposer) normalize(ctx context.Context, raw []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pcm, ok := orderPCMFromWAV(raw); ok {
		return pcm, nil
	}
	ffmpeg := c.ffmpeg
	if ffmpeg == "" {
		ffmpeg = strings.TrimSpace(os.Getenv("FFMPEG_PATH"))
	}
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error", "-protocol_whitelist", "pipe", "-i", "pipe:0", "-vn", "-ac", "1", "-ar", "24000", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
	cmd.Stdin = bytes.NewReader(raw)
	var out boundedOrderPCM
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return nil, errors.New("order audio PCM normalization failed")
	}
	if out.Len() == 0 || out.Len()%2 != 0 {
		return nil, errors.New("order audio PCM is empty or incomplete")
	}
	return out.Bytes(), nil
}

// Parse chunks rather than assuming a 44-byte WAV header. Providers and FFmpeg
// can include LIST/JUNK chunks, or use an unbounded data length on a pipe.
func orderPCMFromWAV(raw []byte) ([]byte, bool) {
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, false
	}
	formatOK := false
	var pcm []byte
	for at := 12; at+8 <= len(raw); {
		kind := string(raw[at : at+4])
		size := uint64(binary.LittleEndian.Uint32(raw[at+4 : at+8]))
		start := at + 8
		if kind == "data" && size == 0xffffffff {
			size = uint64(len(raw) - start)
		}
		if size > uint64(len(raw)-start) {
			return nil, false
		}
		end := start + int(size)
		if kind == "fmt " {
			if size < 16 {
				return nil, false
			}
			f := raw[start:end]
			formatOK = binary.LittleEndian.Uint16(f) == 1 && binary.LittleEndian.Uint16(f[2:]) == 1 && binary.LittleEndian.Uint32(f[4:]) == orderAudioRate && binary.LittleEndian.Uint32(f[8:]) == orderAudioRate*2 && binary.LittleEndian.Uint16(f[12:]) == 2 && binary.LittleEndian.Uint16(f[14:]) == 16
		} else if kind == "data" {
			if pcm != nil {
				return nil, false
			}
			pcm = raw[start:end]
		}
		at = end + int(size%2)
	}
	return pcm, formatOK && len(pcm) > 0 && len(pcm)%2 == 0 && len(pcm) <= orderAudioMaxBytes
}

func orderPCMToWAV(pcm []byte) []byte {
	raw := make([]byte, 44+len(pcm))
	copy(raw, "RIFF")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[22:], 1)
	binary.LittleEndian.PutUint32(raw[24:], orderAudioRate)
	binary.LittleEndian.PutUint32(raw[28:], orderAudioRate*2)
	binary.LittleEndian.PutUint16(raw[32:], 2)
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], uint32(len(pcm)))
	copy(raw[44:], pcm)
	return raw
}
