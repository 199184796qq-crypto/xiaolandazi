package ttsgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// LongTextJob and the staging file are server-owned. No vendor URL or key is
// persisted. Once submitted, retries query the same task, never submit again.
type LongTextJob struct {
	StatePath, SourceVoiceID, AccountID string
	// RecoveryWait bounds queries of an old unavailable task. A caller may
	// retain its evidence and start a replacement after this attempt returns.
	RecoveryWait time.Duration `json:"-"`
}
type SpeechWord struct {
	Text  string  `json:"word"`
	Start float64 `json:"startTime"`
	End   float64 `json:"endTime"`
}
type SpeechSubtitle struct {
	Text  string       `json:"text"`
	Start float64      `json:"startTime"`
	End   float64      `json:"endTime"`
	Words []SpeechWord `json:"words,omitempty"`
}
type longTextState struct {
	AccountID     string           `json:"account_id,omitempty"`
	VoiceID       string           `json:"voice_id"`
	Version       int              `json:"version"`
	InputSHA      string           `json:"input_sha"`
	CredentialSHA string           `json:"credential_sha"`
	TaskID        string           `json:"task_id"`
	Complete      bool             `json:"complete"`
	AudioSHA      string           `json:"audio_sha,omitempty"`
	Characters    *int64           `json:"characters,omitempty"`
	Subtitles     []SpeechSubtitle `json:"subtitles,omitempty"`
}

const MaxLongTextAudioBytes int64 = 128 << 20

type longTextUncertainError struct{}

func (*longTextUncertainError) Error() string {
	return "长文本任务提交结果未确认；仅查询原任务，不重复提交"
}

func longTextInputSHA(r SynthesizeRequest) string {
	voice := r.VoiceID
	if r.LongText != nil && r.LongText.SourceVoiceID != "" {
		voice = r.LongText.SourceVoiceID
	}
	raw, _ := json.Marshal([]any{VolcengineICL2, voice, r.Text, r.Rate, "pcm-24000-v1", VolcengineSynthesisProfile})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func saveLongTextState(path string, state longTextState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".long-state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
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
func readLongTextState(path string) (longTextState, error) {
	var state longTextState
	f, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state)
	return state, err
}

// LoadLongTextResult recovers paid media before entering the gateway/observer.
// This is a cache read, so it does not reserve another fee or account slot.
func LoadLongTextResult(r SynthesizeRequest) (SynthesizeResponse, bool) {
	if r.LongText == nil {
		return SynthesizeResponse{}, false
	}
	state, err := readLongTextState(r.LongText.StatePath)
	if err != nil || state.Version != 1 || !state.Complete || state.InputSHA != longTextInputSHA(r) {
		return SynthesizeResponse{}, false
	}
	path := r.LongText.StatePath + ".wav"
	f, err := os.Open(path)
	if err != nil {
		return SynthesizeResponse{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() <= 44 || info.Size() > MaxLongTextAudioBytes+44 {
		return SynthesizeResponse{}, false
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil || hex.EncodeToString(h.Sum(nil)) != state.AudioSHA {
		return SynthesizeResponse{}, false
	}
	return longTextResponse(r, state), true
}
func longTextResponse(r SynthesizeRequest, state longTextState) SynthesizeResponse {
	return SynthesizeResponse{Provider: ProviderVolcengine, Model: VolcengineICL2, VoiceID: state.VoiceID, Rate: r.Rate, ProviderRequestID: state.TaskID, AudioFile: r.LongText.StatePath + ".wav", Subtitles: state.Subtitles, Usage: SynthesisUsage{Present: state.Characters != nil, Characters: state.Characters, CharacterField: "synthesize_text_length"}}
}

func (p *volcengineProvider) synthesizeLongText(ctx context.Context, r SynthesizeRequest) (SynthesizeResponse, error) {
	if r.Model != VolcengineICL2 || !ValidVolcengineVoiceID(r.VoiceID) || utf8.RuneCountInString(r.Text) < 1 || utf8.RuneCountInString(r.Text) > 100000 || r.LongText.StatePath == "" {
		return SynthesizeResponse{}, notSent(errors.New("长文本声音参数无效，每段须为1到100000字符"))
	}
	if r.Rate == 0 {
		r.Rate = 1
	}
	if math.IsNaN(r.Rate) || math.IsInf(r.Rate, 0) || r.Rate < .5 || r.Rate > 2 || strings.TrimSpace(p.config.APIKey) == "" {
		return SynthesizeResponse{}, notSent(errors.New("长文本声音账号或语速无效"))
	}
	if out, ok := LoadLongTextResult(r); ok {
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return SynthesizeResponse{}, notSent(err)
	}
	path := r.LongText.StatePath
	state, err := readLongTextState(path)
	fingerprint := digestSynthesisText(p.config.APIKey)
	if err == nil {
		if state.Version != 1 || state.InputSHA != longTextInputSHA(r) || state.VoiceID != r.VoiceID || state.CredentialSHA != fingerprint || state.TaskID == "" {
			return SynthesizeResponse{}, notSent(errors.New("该长文本任务需等待原TTS账号恢复，未重复提交"))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return SynthesizeResponse{}, notSent(errors.New("长文本任务检查点不可读取，未重复提交"))
	}
	newTask := errors.Is(err, os.ErrNotExist)
	endpoint := strings.TrimRight(p.config.LongEndpoint, "/")
	if endpoint == "" {
		endpoint = "https://openspeech.bytedance.com/api/v3/tts"
	}
	client := *p.config.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	wait := p.config.LongMaxWait
	if wait <= 0 {
		wait = 30 * time.Minute
	}
	if !newTask && r.LongText.RecoveryWait > 0 && r.LongText.RecoveryWait < wait {
		wait = r.LongText.RecoveryWait
	}
	// An accepted async task continues in the cloud after live cancellation.
	// Drain it under the still-renewed account permit and retain the paid file;
	// the producer checks its original context before any publication.
	work, done := context.WithTimeout(context.WithoutCancel(ctx), wait)
	defer done()
	post := func(action string, payload any) (map[string]json.RawMessage, error) {
		raw, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(work, http.MethodPost, endpoint+"/"+action, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", p.config.APIKey)
		req.Header.Set("X-Api-Resource-Id", VolcengineICL2)
		req.Header.Set("X-Api-Request-Id", state.TaskID)
		resp, err := client.Do(req)
		if err != nil {
			return nil, &longTextUncertainError{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
			if action == "submit" && volcengineConcurrencyRejection(raw) {
				return nil, notSent(&ConcurrencyLimitError{})
			}
			return nil, fmt.Errorf("长文本任务HTTP %d", resp.StatusCode)
		}
		var envelope struct {
			Code    int                        `json:"code"`
			Data    map[string]json.RawMessage `json:"data"`
			Message string                     `json:"message"`
			Msg     string                     `json:"msg"`
		}
		if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&envelope); err != nil {
			return nil, &longTextUncertainError{}
		}
		if envelope.Code != 20000000 {
			if action == "submit" {
				raw, _ := json.Marshal(map[string]string{"message": envelope.Message, "msg": envelope.Msg})
				if volcengineConcurrencyRejection(raw) {
					return nil, notSent(&ConcurrencyLimitError{})
				}
			}
			return nil, fmt.Errorf("长文本任务失败（code=%d），未重复提交", envelope.Code)
		}
		return envelope.Data, nil
	}
	if newTask {
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return SynthesizeResponse{}, notSent(err)
		}
		state = longTextState{Version: 1, InputSHA: longTextInputSHA(r), CredentialSHA: fingerprint, TaskID: hex.EncodeToString(id[:]), AccountID: r.LongText.AccountID, VoiceID: r.VoiceID}
		if err = saveLongTextState(path, state); err != nil {
			return SynthesizeResponse{}, notSent(err)
		}
		if err = ctx.Err(); err != nil {
			_ = os.Remove(path)
			return SynthesizeResponse{}, notSent(err)
		}
		_, err = post("submit", map[string]any{"user": map[string]string{"uid": "xiaolan-script"}, "unique_id": state.TaskID, "req_params": map[string]any{"text": r.Text, "speaker": r.VoiceID, "model": "seed-tts-2.0-standard", "additions": volcengineFidelityAdditions, "audio_params": map[string]any{"format": "pcm", "sample_rate": 24000, "speech_rate": int(math.Round((r.Rate - 1) * 100)), "enable_timestamp": true}}})
		if err != nil {
			if IsConcurrencyLimit(err) {
				// The vendor explicitly rejected this unique task before acceptance.
				// Do not clear an ambiguous or already accepted checkpoint.
				if e := os.Remove(path); e != nil {
					return SynthesizeResponse{}, errors.New("并发拒绝后的任务状态无法保存，未重新提交")
				}
				return SynthesizeResponse{}, err
			}
			var uncertain *longTextUncertainError
			if !errors.As(err, &uncertain) {
				return SynthesizeResponse{}, err
			}
			// A lost submit response may follow acceptance. Keep the same
			// account permit while querying; no extra cloud task is admitted.
		}
	}
	interval := p.config.LongPollInterval
	if interval <= 0 {
		interval = time.Second
	}
	for {
		if err = work.Err(); err != nil {
			return SynthesizeResponse{}, errors.New("长文本任务仍未确认完成；已保留任务，不重复提交")
		}
		data, queryErr := post("query", map[string]string{"task_id": state.TaskID})
		if queryErr == nil {
			var status int
			_ = json.Unmarshal(data["task_status"], &status)
			if status == 3 {
				return SynthesizeResponse{}, errors.New("长文本声音生成失败；已保留任务回执")
			}
			if status == 2 {
				var address string
				_ = json.Unmarshal(data["audio_url"], &address)
				_ = json.Unmarshal(data["sentences"], &state.Subtitles)
				var count int64
				if json.Unmarshal(data["synthesize_text_length"], &count) == nil && count >= 0 {
					state.Characters = &count
				}
				if err = p.downloadLongPCM(work, &client, address, path+".wav"); err != nil {
					return SynthesizeResponse{}, err
				}
				f, e := os.Open(path + ".wav")
				if e != nil {
					return SynthesizeResponse{}, e
				}
				h := sha256.New()
				_, e = io.Copy(h, f)
				f.Close()
				if e != nil {
					return SynthesizeResponse{}, e
				}
				state.AudioSHA = hex.EncodeToString(h.Sum(nil))
				state.Complete = true
				if err = saveLongTextState(path, state); err != nil {
					return SynthesizeResponse{}, err
				}
				return longTextResponse(r, state), nil
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-work.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (p *volcengineProvider) downloadLongPCM(ctx context.Context, client *http.Client, address, path string) error {
	u, err := url.Parse(address)
	if err != nil || u.User != nil || u.Scheme != "https" {
		return errors.New("长文本声音下载地址无效")
	}
	allowed := false
	for _, suffix := range []string{".bytespeech.com", ".bytedance.com", ".bytedance.net", ".bytetos.com", ".volces.com", ".volcengine.com", ".byteimg.com"} {
		allowed = allowed || strings.HasSuffix(strings.ToLower(u.Hostname()), suffix)
	}
	// Local test transports may serve a synthetic vendor URL; production URLs
	// always retain the vendor-domain and HTTPS checks above.
	if !allowed {
		return errors.New("长文本声音下载地址不属于语音供应商")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("读取已生成长文本声音失败；保留原任务，不重新合成")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("读取长文本声音HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".long-audio-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(make([]byte, 44)); err != nil {
		f.Close()
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, MaxLongTextAudioBytes+1))
	if err != nil || n == 0 || n%2 != 0 || n > MaxLongTextAudioBytes {
		f.Close()
		return errors.New("长文本声音未完整取得或超过128MB，请用====缩短单段；未重新合成")
	}
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(n+36))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], 24000)
	binary.LittleEndian.PutUint32(header[28:], 48000)
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(n))
	if _, err = f.WriteAt(header, 0); err == nil {
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
