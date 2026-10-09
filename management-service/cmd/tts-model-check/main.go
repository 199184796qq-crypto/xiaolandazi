// tts-model-check checks only newly created CosyVoice voices; it does not touch
// database records, active rooms, model defaults, or existing voice IDs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"livecompanion/management/internal/ttsgateway"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func run() (map[string]any, error) {
	model := os.Getenv("TTS_CHECK_MODEL")
	if !ttsgateway.IsCosyVoiceModel(model) {
		return nil, fmt.Errorf("TTS_CHECK_MODEL must be an exact CosyVoice 3.5 model")
	}
	description := os.Getenv("TTS_CHECK_DESCRIPTION")
	text := os.Getenv("TTS_CHECK_TEXT")
	output := os.Getenv("TTS_CHECK_OUTPUT")
	if description == "" || text == "" || output == "" {
		return nil, fmt.Errorf("check description, text and output directory are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	g := ttsgateway.NewFromEnv()
	created := []string{}
	defer func() {
		for _, id := range created {
			cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
			err := g.DeleteCheckVoice(cleanup, model, id)
			stop()
			if err != nil {
				fmt.Fprintln(os.Stderr, "check voice cleanup failed:", err)
			}
		}
	}()
	designed, err := g.DesignVoice(ctx, ttsgateway.DesignRequest{Model: model, PreferredName: "xlcheck", Description: description, PreviewText: text})
	if err != nil {
		return nil, err
	}
	created = append(created, designed.VoiceID)
	if err := os.MkdirAll(output, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(output, model+"-designed.wav"), designed.PreviewAudio, 0600); err != nil {
		return nil, err
	}
	synth, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{Model: model, VoiceID: designed.VoiceID, Text: text, Rate: 1.1, Instruction: "用自然亲切的语气讲解。"})
	if err != nil {
		return nil, err
	}
	clone, err := g.CloneVoice(ctx, ttsgateway.CloneRequest{TargetModel: model, PreferredName: "xlcheck", AudioURL: synth.AudioURL})
	if err != nil {
		return nil, err
	}
	created = append(created, clone.VoiceID)
	cloned, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{Model: model, VoiceID: clone.VoiceID, Text: text, Rate: 1.1, Instruction: "请用四川话表达。"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloned.AudioURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("check audio download HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if len(raw) < 44 || !strings.HasPrefix(string(raw[:4]), "RIFF") {
		return nil, fmt.Errorf("check audio is not WAV")
	}
	if err := os.WriteFile(filepath.Join(output, model+"-cloned-sichuan.wav"), raw, 0600); err != nil {
		return nil, err
	}
	return map[string]any{"model": model, "provider": cloned.Provider, "design": true, "clone": true, "synthesis": true, "sichuan_instruction_sent": true, "designed_wav_bytes": len(designed.PreviewAudio), "cloned_wav_bytes": len(raw), "rate": cloned.Rate, "rooms_changed": false}, nil
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}
