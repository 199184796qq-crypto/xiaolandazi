package coreaudio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"livecompanion/core/internal/roomaudio"
)

func ffmpegExecutable() string {
	if configured := strings.TrimSpace(os.Getenv("FFMPEG_PATH")); configured != "" {
		return configured
	}
	return "ffmpeg"
}

func streamAudioURLAsCorePCM(
	ctx context.Context,
	audioURL string,
	startMS int,
	onFrame func([]byte, int) error,
) error {
	audioURL = strings.TrimSpace(audioURL)
	if audioURL == "" {
		return errors.New("audio url is required")
	}
	if onFrame == nil {
		return errors.New("frame callback is required")
	}
	if startMS < 0 {
		startMS = 0
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
	}
	if startMS > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", float64(startMS)/1000))
	}
	args = append(args,
		"-i", audioURL,
		"-vn",
		"-ac", "1",
		"-ar", "24000",
		"-f", "s16le",
		"pipe:1",
	)

	cmd := exec.CommandContext(ctx, ffmpegExecutable(), args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg audio normalization: %w", err)
	}

	frame := make([]byte, roomaudio.PCMBytesPerFrame)
	cursorMS := startMS
	emitted := 0
	for {
		_, readErr := io.ReadFull(stdout, frame)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
				break
			}
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return readErr
		}
		if err := onFrame(append([]byte(nil), frame...), cursorMS); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		emitted++
		cursorMS += roomaudio.FrameDurationMS
	}

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 500 {
			detail = detail[len(detail)-500:]
		}
		if detail == "" {
			detail = waitErr.Error()
		}
		return fmt.Errorf("ffmpeg audio normalization failed: %s", detail)
	}
	if emitted == 0 {
		return errors.New("ffmpeg audio normalization produced no pcm frames")
	}
	return nil
}
