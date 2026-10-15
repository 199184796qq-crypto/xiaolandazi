package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

const maxSessionDownloadPartBytes = 128 << 20

// Preserve the original samples; generation combiners add playback guard tails
// that must not be added again to exported audio.
func joinSessionDownloadWAVs(ctx context.Context, parts [][]byte) ([]byte, error) {
	if len(parts) != 2 {
		return nil, fmt.Errorf("需要回答和对应的回归声音")
	}
	var format, pcm []byte
	compatible := true
	for _, raw := range parts {
		f, samples, rate, err := parseGeneratedVoiceWAV(raw)
		if err != nil {
			return nil, fmt.Errorf("声音文件格式无效")
		}
		align := int(binary.LittleEndian.Uint16(f[12:14]))
		if align == 0 || len(samples)%align != 0 || rate == 0 {
			return nil, fmt.Errorf("声音文件数据不完整")
		}
		if format == nil {
			format = f
		} else if !bytes.Equal(format, f) {
			compatible = false
		}
		pcm = append(pcm, samples...)
	}
	if compatible {
		return encodePreviewWAV(format, pcm)
	}
	// Older voices may use different sample rates. This is local conversion,
	// with no model calls and no changes to the archived source files.
	var normalized [][]byte
	for _, raw := range parts {
		path, err := copyAudioReaderToTemp(bytes.NewReader(raw), ".wav")
		if err != nil {
			return nil, fmt.Errorf("暂存声音失败")
		}
		defer os.Remove(path)
		output, err := normalizeAudioToCoreWAV(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("统一声音格式失败")
		}
		defer os.Remove(output)
		file, err := os.Open(output)
		if err != nil {
			return nil, fmt.Errorf("读取转换声音失败")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxSessionDownloadPartBytes+1))
		_ = file.Close()
		if readErr != nil || len(data) > maxSessionDownloadPartBytes {
			return nil, fmt.Errorf("转换声音过大或读取失败")
		}
		normalized = append(normalized, data)
	}
	f, first, _, err := parseGeneratedVoiceWAV(normalized[0])
	if err != nil {
		return nil, err
	}
	f2, second, _, err := parseGeneratedVoiceWAV(normalized[1])
	if err != nil || !bytes.Equal(f, f2) {
		return nil, fmt.Errorf("声音格式不一致")
	}
	return encodePreviewWAV(f, append(first, second...))
}

func writeSessionDownloadAudio(ctx context.Context, w http.ResponseWriter, item sessionDownloadItem, client *http.Client) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !item.Available {
		writeError(w, 410, "声音文件已到期或不可用")
		return
	}
	parts := make([][]byte, 0, len(item.Parts))
	for _, part := range item.Parts {
		address, allowed := sessionAudioDownloadURL(part.AudioURL)
		if !allowed {
			writeError(w, 502, "声音文件地址不可用")
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			writeError(w, 502, "声音文件地址不可用")
			return
		}
		resp, err := client.Do(req)
		if err != nil {
			writeError(w, 502, "声音下载暂不可用")
			return
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			writeError(w, 410, "声音文件已到期或不可用")
			return
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxSessionDownloadPartBytes+1))
		_ = resp.Body.Close()
		if readErr != nil || len(raw) == 0 || len(raw) > maxSessionDownloadPartBytes {
			writeError(w, 502, "声音文件过大或读取失败")
			return
		}
		parts = append(parts, raw)
	}
	if len(parts) == 0 {
		writeError(w, 404, "本场声音记录不存在")
		return
	}
	raw := parts[0]
	if len(parts) > 1 {
		var err error
		raw, err = joinSessionDownloadWAVs(ctx, parts)
		if err != nil {
			writeError(w, 502, "合并声音失败，请稍后重试")
			return
		}
	}
	name := fmt.Sprintf("live-room-%d-session-%d-interaction-%d.wav", item.RoomID, item.SessionID, item.ID)
	if item.Kind == "mainline" {
		name = fmt.Sprintf("live-room-%d-session-%d-host-%05d.wav", item.RoomID, item.SessionID, item.Sequence)
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	_, _ = w.Write(raw)
}
