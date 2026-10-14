package ttsgateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"livecompanion/management/internal/voicecatalog"
)

const ProviderTencent = "tencent_tts"
const TencentPremiumModel = "tencent-premium"

type TencentConfig struct {
	AppID, SecretID, SecretKey string
	Endpoint                   string
	Publish                    func(context.Context, []byte) (string, error)
}
type tencentProvider struct{ config TencentConfig }

func TencentConfigFromEnv() TencentConfig {
	return TencentConfig{AppID: strings.TrimSpace(os.Getenv("TENCENT_TTS_APP_ID")), SecretID: strings.TrimSpace(os.Getenv("TENCENT_TTS_SECRET_ID")), SecretKey: strings.TrimSpace(os.Getenv("TENCENT_TTS_SECRET_KEY"))}
}
func TencentConfigured() bool {
	c := TencentConfigFromEnv()
	app, err := strconv.ParseUint(c.AppID, 10, 64)
	return err == nil && app > 0 && c.SecretID != "" && c.SecretKey != ""
}
func NewTencentProvider(c TencentConfig) Provider {
	if c.Endpoint == "" {
		c.Endpoint = "wss://tts.cloud.tencent.com/stream_wsv2"
	}
	if c.Publish == nil {
		c.Publish = cacheTencentWAV
	}
	return &tencentProvider{config: c}
}
func (p *tencentProvider) Name() string { return ProviderTencent }
func (p *tencentProvider) CloneVoice(context.Context, CloneRequest) (CloneResponse, error) {
	return CloneResponse{}, errors.New("腾讯精品音色使用官方预置声音，不支持在此复刻")
}

// Tencent documents this piecewise mapping; Rate is a multiplier, Speed is not.
func tencentSpeed(rate float64) (float64, error) {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < .6 || rate > 2 {
		return 0, errors.New("腾讯精品音色语速支持0.6到2倍")
	}
	var speed float64
	switch {
	case rate < 1.2:
		speed = (rate - 1) / .2
	case rate < 1.5:
		speed = 1 + (rate-1.2)/.3
	default:
		speed = 2 + (rate-1.5)/.25
	}
	return math.Round(speed*100) / 100, nil
}
func tencentSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func tencentSignedURL(endpoint, secret string, params url.Values) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" || u.RawQuery != "" {
		return "", errors.New("腾讯合成地址无效")
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+params.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte("GET" + u.Host + u.Path + "?" + strings.Join(pairs, "&")))
	params.Set("Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	u.RawQuery = params.Encode()
	return u.String(), nil
}
func (p *tencentProvider) SynthesizeURL(parent context.Context, r SynthesizeRequest) (SynthesizeResponse, error) {
	if r.Model != TencentPremiumModel {
		return SynthesizeResponse{}, errors.New("腾讯精品音色模型不匹配")
	}
	voice, ok := voicecatalog.Find(r.VoiceID)
	if !ok || voice.Model != TencentPremiumModel {
		return SynthesizeResponse{}, errors.New("请选择腾讯精品音色")
	}
	text := strings.TrimSpace(r.Text)
	if text == "" || utf8.RuneCountInString(text) > 10000 {
		return SynthesizeResponse{}, errors.New("腾讯单次合成文字须为1到10000字符")
	}
	if r.Rate == 0 {
		r.Rate = 1
	}
	speed, err := tencentSpeed(r.Rate)
	if err != nil {
		return SynthesizeResponse{}, err
	}
	c := p.config
	app, err := strconv.ParseUint(c.AppID, 10, 64)
	if err != nil || app == 0 || c.SecretID == "" || c.SecretKey == "" {
		return SynthesizeResponse{}, errors.New("腾讯声音尚未配置AppID和API密钥，请联系运维配置")
	}
	session, err := tencentSessionID()
	if err != nil {
		return SynthesizeResponse{}, err
	}
	now := time.Now().Unix()
	params := url.Values{"Action": {"TextToStreamAudioWSv2"}, "AppId": {c.AppID}, "SecretId": {c.SecretID}, "Timestamp": {strconv.FormatInt(now, 10)}, "Expired": {strconv.FormatInt(now+3600, 10)}, "SessionId": {session}, "VoiceType": {r.VoiceID}, "Codec": {"pcm"}, "SampleRate": {"16000"}, "Speed": {strconv.FormatFloat(speed, 'f', 2, 64)}, "Volume": {"0"}}
	endpoint, err := tencentSignedURL(c.Endpoint, c.SecretKey, params)
	if err != nil {
		return SynthesizeResponse{}, err
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, endpoint, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return SynthesizeResponse{}, ctx.Err()
		}
		return SynthesizeResponse{}, errors.New("连接腾讯声音服务失败，请检查网络与账号配置")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetReadDeadline(deadline)
	conn.SetWriteDeadline(deadline)
	conn.SetReadLimit(32 << 20)
	send := func(action, data string) error {
		id, e := tencentSessionID()
		if e != nil {
			return e
		}
		return conn.WriteJSON(map[string]string{"session_id": session, "message_id": id, "action": action, "data": data})
	}
	var pcm bytes.Buffer
	sent := false
	for {
		kind, raw, e := conn.ReadMessage()
		if e != nil {
			if ctx.Err() != nil {
				return SynthesizeResponse{}, ctx.Err()
			}
			return SynthesizeResponse{}, errors.New("腾讯声音连接提前结束，未收到完整音频")
		}
		if kind == websocket.BinaryMessage {
			if !sent || pcm.Len()+len(raw) > 32<<20 {
				return SynthesizeResponse{}, errors.New("腾讯声音数据顺序或大小异常")
			}
			pcm.Write(raw)
			continue
		}
		if kind != websocket.TextMessage {
			continue
		}
		var event struct {
			Code  *int `json:"code"`
			Ready int  `json:"ready"`
			Final int  `json:"final"`
		}
		if json.Unmarshal(raw, &event) != nil || event.Code == nil {
			return SynthesizeResponse{}, errors.New("腾讯声音响应无效")
		}
		if *event.Code != 0 {
			// Do not return provider messages or the signed handshake URL: either may contain credentials.
			return SynthesizeResponse{}, fmt.Errorf("腾讯声音合成失败（错误码%d），请检查密钥、语音合成服务权限及额度", *event.Code)
		}
		if event.Ready == 1 && !sent {
			if send("ACTION_SYNTHESIS", text) != nil || send("ACTION_COMPLETE", "") != nil {
				return SynthesizeResponse{}, errors.New("发送腾讯合成文字失败")
			}
			sent = true
		}
		if event.Final == 1 {
			if !sent || pcm.Len() == 0 || pcm.Len()%2 != 0 {
				return SynthesizeResponse{}, errors.New("腾讯返回的完整PCM音频为空或无效")
			}
			// Premium voices are 16kHz. Declare the real format; Core normalizes it to its output rate.
			wav := tencentPCMToWAV(pcm.Bytes())
			audioURL, e := c.Publish(ctx, wav)
			if e != nil {
				return SynthesizeResponse{}, e
			}
			return SynthesizeResponse{AudioURL: audioURL, Provider: ProviderTencent, Model: r.Model, VoiceID: r.VoiceID, Rate: r.Rate}, nil
		}
	}
}
func tencentPCMToWAV(pcm []byte) []byte {
	raw := make([]byte, 44+len(pcm))
	copy(raw, "RIFF")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[22:], 1)
	binary.LittleEndian.PutUint32(raw[24:], 16000)
	binary.LittleEndian.PutUint32(raw[28:], 32000)
	binary.LittleEndian.PutUint16(raw[32:], 2)
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], uint32(len(pcm)))
	copy(raw[44:], pcm)
	return raw
}
