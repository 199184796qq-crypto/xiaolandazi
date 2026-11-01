package httpapi

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const maxVolcengineRenderBytes int64 = 160 << 20

// Read only a tenant-owned archived asset URL supplied by our storage layer.
// Local staging eviction must never turn a replay into another paid TTS call.
func downloadVolcengineCachedWAV(ctx context.Context, address, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", errors.New("缓存音频地址无效")
	}
	client := http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("读取缓存音频失败，未重新合成")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("读取缓存音频HTTP %d，未重新合成", resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, ".archived-*.wav")
	if err != nil {
		return "", err
	}
	path := f.Name()
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxVolcengineRenderBytes+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil || n > maxVolcengineRenderBytes {
		os.Remove(path)
		return "", errors.New("缓存音频未完整取得，未重新合成")
	}
	if _, err = volcengineWAVDuration(path); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// ffmpeg may include LIST/JUNK chunks, so do not assume a 44-byte WAV header.
func volcengineWAVDuration(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() <= 44 || info.Size() > maxVolcengineRenderBytes {
		return 0, errors.New("长段音频大小无效")
	}
	header := make([]byte, 12)
	if _, err = io.ReadFull(f, header); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return 0, errors.New("长段音频不是WAV")
	}
	var byteRate int64
	for offset := int64(12); offset+8 <= info.Size(); {
		chunk := make([]byte, 8)
		if _, err = io.ReadFull(f, chunk); err != nil {
			return 0, err
		}
		n := int64(binary.LittleEndian.Uint32(chunk[4:]))
		if n < 0 || offset+8+n > info.Size() {
			return 0, errors.New("长段WAV数据不完整")
		}
		switch string(chunk[:4]) {
		case "fmt ":
			if n < 16 || n > 1024 {
				return 0, errors.New("长段WAV格式无效")
			}
			format := make([]byte, n)
			if _, err = io.ReadFull(f, format); err != nil {
				return 0, err
			}
			if binary.LittleEndian.Uint16(format) != 1 || binary.LittleEndian.Uint16(format[2:]) != 1 || binary.LittleEndian.Uint32(format[4:]) != 24000 || binary.LittleEndian.Uint16(format[14:]) != 16 {
				return 0, errors.New("长段WAV须为24kHz单声道16位PCM")
			}
			byteRate = int64(binary.LittleEndian.Uint32(format[8:]))
			if byteRate != 48000 {
				return 0, errors.New("长段WAV字节速率无效")
			}
		case "data":
			if byteRate == 0 || n <= 0 || n%2 != 0 {
				return 0, errors.New("长段WAV声音数据无效")
			}
			return n * 1000 / byteRate, nil
		default:
			if _, err = f.Seek(n, io.SeekCurrent); err != nil {
				return 0, err
			}
		}
		if n%2 != 0 {
			if _, err = f.Seek(1, io.SeekCurrent); err != nil {
				return 0, err
			}
		}
		offset += 8 + n + n%2
	}
	return 0, errors.New("长段WAV没有声音数据")
}

// atempo changes duration while preserving pitch. No TTS/model is invoked.
func renderVolcengineTempo(ctx context.Context, source string, percent int) (string, error) {
	if percent < 90 || percent > 110 {
		return "", errors.New("循环变速须在0.90至1.10之间")
	}
	f, err := os.CreateTemp(filepath.Dir(source), ".tempo-*.wav")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	cmd := exec.CommandContext(ctx, ffmpegExecutable(), "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", source, "-vn", "-af", fmt.Sprintf("atempo=%.2f", float64(percent)/100), "-ac", "1", "-ar", "24000", "-c:a", "pcm_s16le", path)
	// Paths, URLs, credentials and full provider output never enter errors.
	if err = cmd.Run(); err != nil {
		os.Remove(path)
		return "", errors.New("缓存声音变速失败，未调用TTS")
	}
	if _, err = volcengineWAVDuration(path); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}
