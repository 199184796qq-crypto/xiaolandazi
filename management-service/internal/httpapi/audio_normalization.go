package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	customMainlineCloneSampleSeconds = 12
	maxAutoCloneSampleBytes          = 5 << 20
)

func ffmpegExecutable() string {
	if configured := strings.TrimSpace(os.Getenv("FFMPEG_PATH")); configured != "" {
		return configured
	}
	return "ffmpeg"
}

func copyAudioReaderToTemp(source io.Reader, suffix string) (string, error) {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		suffix = ".audio"
	}
	file, err := os.CreateTemp("", "xiaolan-audio-source-*"+suffix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := io.Copy(file, source); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func normalizeAudioToCoreWAV(ctx context.Context, inputPath string) (string, error) {
	output, err := os.CreateTemp("", "xiaolan-core-mainline-*.wav")
	if err != nil {
		return "", err
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return "", err
	}
	_ = os.Remove(outputPath)

	cmd := exec.CommandContext(
		ctx,
		ffmpegExecutable(),
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", "24000",
		"-c:a", "pcm_s16le",
		outputPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(outputPath)
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("音频转换失败：%s", detail)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		_ = os.Remove(outputPath)
		return "", err
	}
	if info.Size() <= 44 {
		_ = os.Remove(outputPath)
		return "", fmt.Errorf("音频转换后没有可用声音")
	}
	return outputPath, nil
}

func extractCloneSampleFromAudioPath(ctx context.Context, inputPath string) ([]byte, error) {
	output, err := os.CreateTemp("", "xiaolan-clone-sample-*.wav")
	if err != nil {
		return nil, err
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return nil, err
	}
	defer os.Remove(outputPath)
	_ = os.Remove(outputPath)

	cmd := exec.CommandContext(
		ctx,
		ffmpegExecutable(),
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-t", fmt.Sprintf("%d", customMainlineCloneSampleSeconds),
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		outputPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("提取声音样本失败：%s", detail)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	if len(raw) <= 44 || len(raw) > maxAutoCloneSampleBytes {
		return nil, fmt.Errorf("自动提取的声音样本大小不合法")
	}
	return raw, nil
}

func normalizeUploadedVoiceSample(ctx context.Context, inputPath string) ([]byte, int64, error) {
	output, err := os.CreateTemp("", "xiaolan-uploaded-voice-sample-*.wav")
	if err != nil {
		return nil, 0, err
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return nil, 0, err
	}
	defer os.Remove(outputPath)
	_ = os.Remove(outputPath)

	cmd := exec.CommandContext(
		ctx,
		ffmpegExecutable(),
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		outputPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		if detail == "" {
			detail = err.Error()
		}
		return nil, 0, fmt.Errorf("声音样本转为规范格式失败：%s", detail)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) <= 44 || len(raw) > maxCloneAudioBytes {
		return nil, 0, fmt.Errorf("转换后的声音样本大小不合法")
	}
	durationMS, err := generatedVoiceWAVDurationMS(raw)
	if err != nil {
		return nil, 0, fmt.Errorf("读取声音样本时长失败：%w", err)
	}
	if durationMS < 5000 || durationMS > 10000 {
		return nil, 0, fmt.Errorf("声音样本必须为 5–10 秒，当前约 %.1f 秒", float64(durationMS)/1000)
	}
	return raw, durationMS, nil
}
