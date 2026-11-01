package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
)

// Matches the header, encoding and placeholder lengths inspected on the paid
// session595 file. Samples are synthetic; no customer audio is downloaded.
func qwenPlaceholderWAVFixture() ([]byte, []byte, []int16) {
	pcm := make([]int16, 48000)
	for i := range pcm {
		pcm[i] = int16(i % 8192)
	}
	canonical := scriptFirstPCMToWAV(pcm)
	provider := bytes.Clone(canonical)
	binary.LittleEndian.PutUint32(provider[4:8], 0x7fffffbf)
	binary.LittleEndian.PutUint32(provider[40:44], 0x7fffff9b)
	return provider, canonical, pcm
}

func TestQwenPaidParagraphPlaceholderHeaderCompatibility(t *testing.T) {
	provider, canonical, pcm := qwenPlaceholderWAVFixture()
	path := filepath.Join(t.TempDir(), "paragraph.wav")
	if err := os.WriteFile(path, provider, 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("actual duration", func(t *testing.T) {
		got, err := volcengineWAVDuration(path)
		if err != nil || got != 2000 {
			t.Fatal(got, err)
		}
	})
	t.Run("acoustic block samples", func(t *testing.T) {
		got, rate, err := readWAVPCM16Mono(path)
		if err != nil || rate != 24000 || !slices.Equal(got, pcm) {
			t.Fatal("paid PCM must survive the block reader", len(got), rate, err)
		}
	})
	t.Run("canonical archive", func(t *testing.T) {
		registry, err := storage.NewRegistry(storage.Config{Driver: "local", LocalRoot: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		var input model.CreateMediaAssetInput
		create := func(_ context.Context, in model.CreateMediaAssetInput) (model.MediaAsset, error) {
			input = in
			return model.MediaAsset{ID: 9}, nil
		}
		v := model.LiveAgentPlanVersion{PlanID: 4, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash"}}
		if _, err := archiveUntimedScriptFirstAsset(t.Context(), registry, create, 14, 29, v, "synthetic-key", "原文", "voice", provider, 2000); err != nil {
			t.Fatal(err)
		}
		r, err := registry.Current().Open(t.Context(), input.ObjectKey)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := io.ReadAll(r)
		r.Close()
		sum := sha256.Sum256(canonical)
		if err != nil || !bytes.Equal(saved, canonical) || input.ChecksumSHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("archive must contain canonical headers and identical PCM", err)
		}
	})
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(retained, provider) {
		t.Fatal("paid source or its checksum identity changed", err)
	}
}

func TestWAVPlaceholderCompatibilityStillRejectsMalformedAudio(t *testing.T) {
	provider, canonical, _ := qwenPlaceholderWAVFixture()
	for _, name := range []string{"normal declared truncation", "odd unknown payload", "mismatched sentinel", "stereo", "wrong byte rate", "wrong block alignment", "wrong sample rate", "wrong bits", "empty payload"} {
		t.Run(name, func(t *testing.T) {
			raw := bytes.Clone(provider)
			switch name {
			case "normal declared truncation":
				raw = bytes.Clone(canonical[:len(canonical)-2])
			case "odd unknown payload":
				raw = raw[:len(raw)-1]
			case "mismatched sentinel":
				binary.LittleEndian.PutUint32(raw[40:44], uint32(len(raw)))
			case "stereo":
				binary.LittleEndian.PutUint16(raw[22:24], 2)
			case "wrong byte rate":
				binary.LittleEndian.PutUint32(raw[28:32], 96000)
			case "wrong block alignment":
				binary.LittleEndian.PutUint16(raw[32:34], 4)
			case "wrong sample rate":
				binary.LittleEndian.PutUint32(raw[24:28], 48000)
			case "wrong bits":
				binary.LittleEndian.PutUint16(raw[34:36], 8)
			case "empty payload":
				raw = raw[:44]
			}
			path := filepath.Join(t.TempDir(), "malformed.wav")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := volcengineWAVDuration(path); err == nil {
				t.Fatal("bad WAV accepted for playback duration")
			}
			if _, _, err := readWAVPCM16Mono(path); err == nil {
				t.Fatal("bad WAV accepted by PCM reader")
			}
		})
	}
}
