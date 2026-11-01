package ttsgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const ProviderVolcengine = "volcengine"
const VolcengineICL2 = "seed-icl-2.0"

// The profile is part of paid-audio identities: legacy audio must not be
// mistaken for a synthesis made with the restored reference speaking style.
const VolcengineSynthesisProfile = "tone-fidelity-v1"
const volcengineFidelityAdditions = `{"tone_fidelity":true}`
const volcengineTTSEndpoint = "https://openspeech.bytedance.com/api/v3/tts/unidirectional"

var volcengineSpeaker = regexp.MustCompile(`^S_[A-Za-z0-9_-]{1,128}$`)

func ValidVolcengineVoiceID(id string) bool { return volcengineSpeaker.MatchString(id) }

type VolcengineConfig struct {
	APIKey           string
	Endpoint         string
	Client           *http.Client
	Publish          func(context.Context, []byte) (string, error)
	LongEndpoint     string
	LongPollInterval time.Duration
	LongMaxWait      time.Duration
}
type volcengineProvider struct{ config VolcengineConfig }

func VolcengineConfigFromEnv() VolcengineConfig {
	return VolcengineConfig{APIKey: strings.TrimSpace(os.Getenv("VOLCENGINE_TTS_API_KEY"))}
}
func NewVolcengineProvider(c VolcengineConfig) Provider {
	if c.Endpoint == "" {
		c.Endpoint = volcengineTTSEndpoint
	}
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 90 * time.Second}
	}
	if c.Publish == nil {
		c.Publish = cacheTencentWAV
	} // Shared bounded loopback WAV handoff, then existing archival.
	return &volcengineProvider{config: c}
}
func (p *volcengineProvider) Name() string { return ProviderVolcengine }
func (p *volcengineProvider) CloneVoice(context.Context, CloneRequest) (CloneResponse, error) {
	return CloneResponse{}, errors.New("当前火山接入仅使用已有音色ID，不执行复刻训练")
}
func (p *volcengineProvider) withAccountKey(key string) Provider {
	c := p.config
	c.APIKey = key
	return NewVolcengineProvider(c)
}

// Official V3 HTTP Chunked contract: JSON audio frames followed by code=20000000.
// https://docs.volcengine.com/docs/doubao-voice/http-chunked-sse-unidirectional-streaming-v3
func (p *volcengineProvider) SynthesizeURL(ctx context.Context, r SynthesizeRequest) (out SynthesizeResponse, err error) {
	if r.LongText != nil {
		return p.synthesizeLongText(ctx, r)
	}
	out = SynthesizeResponse{Provider: ProviderVolcengine, Model: r.Model, VoiceID: r.VoiceID, Rate: r.Rate}
	if r.Model != VolcengineICL2 || !ValidVolcengineVoiceID(r.VoiceID) {
		return out, notSent(errors.New("火山声音复刻2.0需要seed-icl-2.0模型及对应的S_音色ID"))
	}
	if strings.TrimSpace(r.Text) == "" || utf8.RuneCountInString(r.Text) > 10000 {
		return out, notSent(errors.New("火山单次合成文字须为1到10000字符"))
	}
	if r.Rate == 0 {
		r.Rate = 1
		out.Rate = 1
	}
	if math.IsNaN(r.Rate) || math.IsInf(r.Rate, 0) || r.Rate < .5 || r.Rate > 2 {
		return out, notSent(errors.New("火山合成语速须为0.5到2倍"))
	}
	if strings.TrimSpace(p.config.APIKey) == "" {
		return out, notSent(errors.New("请在TTS账户池配置火山豆包语音API Key与音色对应关系"))
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return out, notSent(err)
	}
	requestID := hex.EncodeToString(id[:])
	requestID = requestID[:8] + "-" + requestID[8:12] + "-" + requestID[12:16] + "-" + requestID[16:20] + "-" + requestID[20:]
	body, _ := json.Marshal(map[string]any{
		"user":       map[string]string{"uid": "xiaolan-tts"},
		"req_params": map[string]any{"text": r.Text, "speaker": r.VoiceID, "model": "seed-tts-2.0-standard", "additions": volcengineFidelityAdditions, "audio_params": map[string]any{"format": "pcm", "sample_rate": 24000, "speech_rate": int(math.Round((r.Rate - 1) * 100))}},
	})
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, p.config.Endpoint, bytes.NewReader(body))
	if e != nil {
		return out, notSent(errors.New("火山合成请求配置错误"))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", p.config.APIKey)
	req.Header.Set("X-Api-Resource-Id", VolcengineICL2)
	req.Header.Set("X-Api-Request-Id", requestID)
	req.Header.Set("X-Control-Require-Usage-Tokens-Return", "text_words")
	// Never forward the speech key on redirects, including cross-host responses.
	client := *p.config.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, errors.New("火山合成连接失败，未自动重试")
	}
	defer resp.Body.Close()
	out.ProviderRequestID = resp.Header.Get("X-Tt-Logid")
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("火山合成HTTP %d，请检查账号权限或配额", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 48<<20))
	var pcm bytes.Buffer
	for {
		var frame struct {
			Code  *int                       `json:"code"`
			Data  string                     `json:"data"`
			Usage map[string]json.RawMessage `json:"usage"`
		}
		if e = decoder.Decode(&frame); e != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			return out, errors.New("火山声音流未完整结束，已丢弃未完成音频")
		}
		if frame.Code == nil || (*frame.Code != 0 && *frame.Code != 20000000) {
			code := 0
			if frame.Code != nil {
				code = *frame.Code
			}
			return out, fmt.Errorf("火山合成业务失败（code=%d），请检查账号和音色权限", code)
		}
		if frame.Data != "" {
			chunk, e := base64.StdEncoding.DecodeString(frame.Data)
			if e != nil || pcm.Len()+len(chunk) > (32<<20)-44 {
				return out, errors.New("火山声音数据无效或过大")
			}
			pcm.Write(chunk)
		}
		if *frame.Code != 20000000 {
			continue
		}
		if raw, ok := frame.Usage["text_words"]; ok {
			out.Usage.Present = true
			var words int64
			if string(raw) == "null" || json.Unmarshal(raw, &words) != nil || words < 0 {
				out.Usage.Invalid = true
			} else {
				out.Usage.Characters = &words
				out.Usage.CharacterField = "usage.text_words"
			}
		}
		if pcm.Len() == 0 || pcm.Len()%2 != 0 {
			return out, errors.New("火山返回的完整PCM为空或无效")
		}
		if e = ctx.Err(); e != nil {
			return out, e
		}
		wav := tencentPCMToWAV(pcm.Bytes())
		binary.LittleEndian.PutUint32(wav[24:], 24000)
		binary.LittleEndian.PutUint32(wav[28:], 48000)
		out.AudioURL, err = p.config.Publish(ctx, wav)
		return out, err
	}
}
