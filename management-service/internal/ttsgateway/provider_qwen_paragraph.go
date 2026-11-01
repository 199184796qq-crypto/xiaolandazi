package ttsgateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Qwen keeps its current HTTP non-streaming interface with no subtitle timing.
func SupportsScriptParagraphs(model string) bool {
	switch model {
	case VolcengineICL2, "qwen-audio-3.1-tts-flash", "qwen-audio-3.1-tts-plus", "qwen-audio-3.0-tts-flash", "qwen-audio-3.0-tts-plus":
		return true
	}
	return false
}

func retainQwenParagraphWAV(ctx context.Context, client *http.Client, address, path string) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = downloadQwenParagraphWAV(ctx, client, address, path); err == nil {
			return nil
		}
		if attempt == 2 {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

// GET retries reuse the same paid result and never invoke synthesis again.
func downloadQwenParagraphWAV(ctx context.Context, client *http.Client, address, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return errors.New("标准声音下载地址无效")
	}
	reader := *client
	reader.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := reader.Do(req)
	if err != nil {
		return errors.New("标准声音下载失败，已保留任务，未重复合成")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("标准声音下载未成功，未重复合成")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".paragraph-*.wav")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		var n int64
		n, err = io.Copy(f, io.LimitReader(resp.Body, (32<<20)+1))
		if n > 32<<20 || n <= 44 {
			err = errors.New("标准声音文件为空或过大")
		}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
