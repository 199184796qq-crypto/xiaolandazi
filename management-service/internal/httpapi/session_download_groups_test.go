package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"livecompanion/management/internal/model"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSessionDownloadGroupingKeepsScopeOrderAndStableSequence(t *testing.T) {
	at := time.Now().UTC()
	asset := func(id int64, kind, ref, text string) model.SessionSpeechAsset {
		played := at.Add(time.Duration(id) * time.Second)
		return model.SessionSpeechAsset{ID: id, TenantID: 14, RoomID: 26, SessionID: 283, Kind: kind, SourceRef: ref, Text: text, PlayedAt: &played, Available: true, DurationMS: 1000}
	}
	assets := []model.SessionSpeechAsset{
		asset(1, "mainline", "m1", "开场"), asset(2, "interaction", "reply:a", "回答A"),
		asset(3, "mainline", "m2", "继续主线"), asset(4, "interaction", "reply:b", "回答B"),
		asset(5, "bridge", "bridge:a", "回归A"), asset(6, "bridge", "bridge:b", "回归B"),
		asset(7, "interaction", "reply:c", "没有桥"), asset(8, "bridge", "bridge:orphan", "不能猜配"),
	}
	wrong := asset(9, "bridge", "bridge:c", "其它房间")
	wrong.RoomID = 27
	assets = append(assets, wrong)
	wrong = asset(10, "bridge", "bridge:c", "其它场次")
	wrong.SessionID = 284
	assets = append(assets, wrong)
	wrong = asset(11, "bridge", "bridge:c", "其它客户")
	wrong.TenantID = 15
	assets = append(assets, wrong)
	unplayed := asset(12, "bridge", "bridge:c", "尚未播出")
	unplayed.PlayedAt = nil
	assets = append(assets, unplayed)
	items := sessionDownloadItems(assets)
	if len(items) != 5 || items[0].Sequence != 1 || items[2].Sequence != 2 {
		t.Fatalf("incorrect grouped count/sequence: %+v", items)
	}
	if items[1].Text != "回答A\n回归A" || items[3].Text != "回答B\n回归B" || items[4].Text != "没有桥" || items[1].DurationMS != 2000 {
		t.Fatal("bridge crossed tasks or scopes")
	}
	from, to := at.Add(2*time.Second), at.Add(5*time.Second)
	filtered := filterSessionDownloadItems(items, &from, &to)
	if len(filtered) != 3 || filtered[0].Text != "回答A\n回归A" || filtered[1].Sequence != 2 {
		t.Fatal("time filter split pair or renumbered mainline")
	}
	if got := sessionTranscript(assets); strings.Contains(got, "不能猜配") || !strings.Contains(got, "回答A\n回归A\n\n继续主线") {
		t.Fatal("transcript grouping differs from list")
	}
	assets[4].Available = false
	if sessionDownloadItems(assets)[1].Available {
		t.Fatal("must not advertise a partial download")
	}
}

func sessionDownloadTestWAV(t *testing.T, rate int, sample int16, count int) []byte {
	t.Helper()
	f := make([]byte, 16)
	binary.LittleEndian.PutUint16(f[0:2], 1)
	binary.LittleEndian.PutUint16(f[2:4], 1)
	binary.LittleEndian.PutUint32(f[4:8], uint32(rate))
	binary.LittleEndian.PutUint32(f[8:12], uint32(rate*2))
	binary.LittleEndian.PutUint16(f[12:14], 2)
	binary.LittleEndian.PutUint16(f[14:16], 16)
	pcm := make([]byte, count*2)
	for i := 0; i < count; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
	}
	raw, err := encodePreviewWAV(f, pcm)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSessionDownloadMergedWAVFinalizesStreamingHeadersAndKeepsSamples(t *testing.T) {
	a, b := sessionDownloadTestWAV(t, 24000, 111, 2400), sessionDownloadTestWAV(t, 24000, 222, 1200)
	binary.LittleEndian.PutUint32(a[4:8], 0x7fffffff)
	binary.LittleEndian.PutUint32(a[40:44], 0x7fffffff)
	merged, err := joinSessionDownloadWAVs(context.Background(), [][]byte{a, b})
	if err != nil {
		t.Fatal(err)
	}
	_, pcm, rate, err := parseGeneratedVoiceWAV(merged)
	if err != nil || rate != 48000 || len(pcm) != 7200 || int(binary.LittleEndian.Uint32(merged[4:8])) != len(merged)-8 {
		t.Fatal("invalid complete WAV")
	}
	if !bytes.Equal(pcm, append(append([]byte(nil), a[44:]...), b[44:]...)) {
		t.Fatal("samples missing, reordered or guard silence added")
	}
}

func TestSessionDownloadMergedWAVNormalizesDifferentRates(t *testing.T) {
	if _, err := exec.LookPath(ffmpegExecutable()); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	a, b := sessionDownloadTestWAV(t, 16000, 111, 1600), sessionDownloadTestWAV(t, 24000, 222, 2400)
	merged, err := joinSessionDownloadWAVs(context.Background(), [][]byte{a, b})
	if err != nil {
		t.Fatal(err)
	}
	_, pcm, rate, err := parseGeneratedVoiceWAV(merged)
	if err != nil || rate != 48000 || len(pcm) != 9600 {
		t.Fatal("rate conversion lost or stretched audio", len(pcm), rate, err)
	}
}

type sessionDownloadTransport func(*http.Request) (*http.Response, error)

func (f sessionDownloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSessionDownloadHTTPJoinsBothSourcesOrReturnsNoPartialFile(t *testing.T) {
	a, b := sessionDownloadTestWAV(t, 24000, 111, 2400), sessionDownloadTestWAV(t, 24000, 222, 1200)
	asset := model.SessionSpeechAsset{Available: true, AudioURL: "http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/reply.wav?Signature=fixture"}
	bridge := asset
	bridge.AudioURL = "https://archive.oss-cn-hangzhou.aliyuncs.com/bridge.wav"
	item := sessionDownloadItem{SessionSpeechAsset: asset, Parts: []model.SessionSpeechAsset{asset, bridge}}
	for _, failure := range []bool{false, true} {
		calls := 0
		client := &http.Client{Transport: sessionDownloadTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Scheme != "https" {
				t.Fatal("plaintext source request")
			}
			raw := a
			status := 200
			if calls == 2 {
				raw = b
				if failure {
					status = 403
				}
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
		})}
		w := httptest.NewRecorder()
		writeSessionDownloadAudio(context.Background(), w, item, client)
		if calls != 2 {
			t.Fatal("did not fetch both parts")
		}
		if failure {
			if w.Code != 410 || strings.HasPrefix(w.Body.String(), "RIFF") {
				t.Fatal("partial audio reported as successful")
			}
			continue
		}
		_, pcm, _, err := parseGeneratedVoiceWAV(w.Body.Bytes())
		if w.Code != 200 || err != nil || len(pcm) != 7200 {
			t.Fatal("download not merged")
		}
	}
	item.Parts = item.Parts[:1]
	w := httptest.NewRecorder()
	client := &http.Client{Transport: sessionDownloadTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(a))}, nil
	})}
	writeSessionDownloadAudio(context.Background(), w, item, client)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), a) {
		t.Fatal("answer without bridge changed")
	}
}
