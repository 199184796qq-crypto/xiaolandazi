// Provider check: greeting or an explicitly supplied script; no room/plan writes or playback.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"livecompanion/management/internal/ttsgateway"
	"os"
	"strings"
	"time"
)

func main() {
	voice := flag.String("voice", "101030", "Tencent premium voice ID")
	output := flag.String("output", "", "optional WAV output file")
	stdin := flag.Bool("stdin", false, "read requested script from stdin")
	flag.Parse()
	text := "你好，欢迎来到直播间。"
	if *stdin {
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 128<<10))
		if err != nil {
			fmt.Println(`{"ok":false,"error":"读取试听文字失败"}`)
			os.Exit(1)
		}
		text = strings.TrimSpace(string(raw))
	}
	c := ttsgateway.TencentConfigFromEnv()
	var wav []byte
	c.Publish = func(ctx context.Context, raw []byte) (string, error) { wav = raw; return "verified", nil }
	p := ttsgateway.NewTencentProvider(c)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	started := time.Now()
	r, err := p.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{Model: ttsgateway.TencentPremiumModel, VoiceID: *voice, Text: text, Rate: 1})
	if err != nil {
		out, _ := json.Marshal(map[string]any{"ok": false, "error": err.Error()})
		fmt.Println(string(out))
		os.Exit(1)
	}
	if *output != "" {
		if err := os.WriteFile(*output, wav, 0600); err != nil {
			fmt.Println(`{"ok":false,"error":"保存试听声音失败"}`)
			os.Exit(1)
		}
	}
	digest := sha256.Sum256(wav)
	textDigest := sha256.Sum256([]byte(text))
	out, _ := json.Marshal(map[string]any{"ok": true, "provider": r.Provider, "model": r.Model, "voice_id": r.VoiceID, "sample_rate": 16000, "wav_bytes": len(wav), "duration_ms": (len(wav) - 44) * 1000 / 32000, "sha256": hex.EncodeToString(digest[:]), "text_sha256": hex.EncodeToString(textDigest[:]), "text_characters": len([]rune(text)), "elapsed_ms": time.Since(started).Milliseconds(), "live_playback": false})
	fmt.Println(string(out))
}
