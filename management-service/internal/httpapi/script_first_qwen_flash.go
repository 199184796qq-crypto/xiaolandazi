package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"livecompanion/management/internal/ttsgateway"
)

func splitQwenFlashParagraph(text string) []string {
	runes := []rune(text)
	const limit = ttsgateway.QwenFlashMaxInputRunes
	const sentenceEnd = "。！？.!?；;\n"
	const clauseEnd = "，、,:：\t "
	var parts []string
	for len(runes) > limit {
		end := limit
		// Prefer a natural pause near the request limit. If none is near,
		// preserve an earlier pause before splitting an unpunctuated run.
	findPause:
		for _, boundary := range []struct {
			marks string
			start int
		}{{sentenceEnd, limit / 2}, {clauseEnd, limit / 2}, {sentenceEnd + clauseEnd, 0}} {
			for i := limit - 1; i >= boundary.start; i-- {
				r := runes[i]
				if (r == '.' || r == ',') && i > 0 && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1]) {
					continue
				}
				if strings.ContainsRune(boundary.marks, r) {
					end = i + 1
					break findPause
				}
			}
		}
		for end < limit && strings.ContainsRune("”’）》」』】", runes[end]) {
			end++
		}
		parts = append(parts, string(runes[:end]))
		runes = runes[end:]
	}
	return append(parts, string(runes))
}

// The caller holds the paragraph lock. Each part has its own paid checkpoint
// so a failed or cancelled part never invalidates its completed neighbours.
func synthesizeQwenFlashParagraph(ctx context.Context, unit volcengineScriptUnit, path string,
	synthesize func(context.Context, volcengineScriptUnit, string) (ttsgateway.SynthesizeResponse, error),
) (ttsgateway.SynthesizeResponse, error) {
	var output ttsgateway.SynthesizeResponse
	texts := splitQwenFlashParagraph(unit.Text)
	results := make([]ttsgateway.SynthesizeResponse, len(texts))
	parts := make([]volcengineScriptUnit, len(texts))
	for i, text := range texts {
		key := fmt.Sprintf("%s-qwen-flash-%d", unit.Key, i+1)
		parts[i] = volcengineScriptUnit{Key: key, Text: text, Dir: filepath.Join(unit.Dir, key)}
	}
	work, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	const concurrency = 3
	for worker := range min(concurrency, len(parts)) {
		workers.Go(func() {
			for i := worker; i < len(parts) && work.Err() == nil; i += concurrency {
				part := parts[i]
				if strings.TrimSpace(part.Text) == "" {
					continue
				}
				result, err := prepareScriptFirstPaid(work, part, func(c context.Context, file string) (ttsgateway.SynthesizeResponse, error) {
					return synthesize(c, part, file)
				})
				if err != nil {
					cancel(fmt.Errorf("阿里云话术第 %d/%d 段合成失败：%w", i+1, len(parts), err))
					return
				}
				results[i] = result
			}
		})
	}
	workers.Wait()
	if err := context.Cause(work); err != nil {
		return output, err
	}
	var pcm []int16
	for i, result := range results {
		if result.AudioFile == "" {
			continue
		}
		samples, _, err := readWAVPCM16Mono(result.AudioFile)
		if err != nil {
			// The normal Qwen Flash output is already 24 kHz mono PCM.
			// Convert other valid WAV layouts for playback without repurchasing.
			if _, invalid := volcengineWAVDuration(result.AudioFile); invalid != nil {
				if backupErr := retireUnavailableScriptFiles(ctx, parts[i], "paragraph-job.json", "paragraph.wav"); backupErr != nil {
					return output, backupErr
				}
				return output, fmt.Errorf("阿里云话术第 %d 段声音无效：%w", i+1, err)
			}
			normalized, conversionErr := normalizeAudioToCoreWAV(ctx, result.AudioFile)
			if conversionErr != nil {
				return output, conversionErr
			}
			samples, _, err = readWAVPCM16Mono(normalized)
			_ = os.Remove(normalized)
			if err != nil {
				return output, err
			}
		}
		if int64(len(pcm)+len(samples))*2 > ttsgateway.MaxLongTextAudioBytes {
			return output, errors.New("阿里云合成的整片音频过大")
		}
		pcm = append(pcm, samples...)
		output.VoiceID = result.VoiceID
	}
	if len(pcm) == 0 {
		return output, errors.New("阿里云合成的整片音频为空")
	}
	if err := os.WriteFile(path, scriptFirstPCMToWAV(pcm), 0600); err != nil {
		return output, err
	}
	output.AudioFile = path
	return output, nil
}
