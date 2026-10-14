package httpapi

import (
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

func TestSessionDownloadTranscriptAllKindsAndPages(t *testing.T) {
	at := time.Now()
	items := []model.SessionSpeechAsset{{Text: "待播稿"}}
	for i := 0; i < 151; i++ {
		ref := ""
		if i%3 == 1 {
			ref = fmt.Sprintf("reply:d-%d", i/3)
		}
		if i%3 == 2 {
			ref = fmt.Sprintf("bridge:d-%d", i/3)
		}
		items = append(items, model.SessionSpeechAsset{ID: int64(i + 1), SourceRef: ref, Kind: []string{"mainline", "interaction", "bridge"}[i%3], Text: "  已播文字  ", PlayedAt: &at})
	}
	got := sessionTranscript(items)
	if strings.Contains(got, "待播稿") || strings.Count(got, "已播文字") != 151 || strings.Contains(got, "mainline") || strings.HasPrefix(got, " ") {
		t.Fatal("export incomplete or contains metadata/unplayed content")
	}
}
func TestSessionDownloadRangeTimezoneAndValidation(t *testing.T) {
	from, to, e := parseSessionDownloadRange("2026-10-14T18:00:00+08:00", "2026-10-14T10:01:00Z")
	if e != nil || from.UTC().Hour() != 10 || to.Sub(*from) != time.Minute {
		t.Fatal(from, to, e)
	}
	for _, v := range [][2]string{{"invalid", ""}, {"2026-10-14T10:00:00Z", "2026-10-14T10:00:00Z"}, {"2026-10-14T11:00:00Z", "2026-10-14T10:00:00Z"}} {
		if _, _, e = parseSessionDownloadRange(v[0], v[1]); e == nil {
			t.Fatal("invalid time accepted", v)
		}
	}
}
func TestSessionAudioURLScope(t *testing.T) {
	if !sessionAudioURLAllowed("https://bucket.oss-cn-hangzhou.aliyuncs.com/a.wav?Expires=1") {
		t.Fatal("valid OSS address rejected")
	}
	for _, address := range []string{"http://bucket.aliyuncs.com/a", "https://aliyuncs.com.evil.test/a", "https://evilaliyuncs.com/a", "https://127.0.0.1/a", "https://user:secret@bucket.aliyuncs.com/a", "file:///etc/passwd"} {
		if sessionAudioURLAllowed(address) {
			t.Fatal("unsafe audio address accepted", address)
		}
	}
}

func TestSessionDownloadUpgradesLegacyDashScopeURLOnly(t *testing.T) {
	address := "http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/prod/cosyvoice-v3.5-flash/a%2Fb.wav?Expires=2000000000&OSSAccessKeyId=fixture&Signature=a%2Bb%2Fc%3D"
	got, ok := sessionAudioDownloadURL(address)
	if !ok || got != "https"+strings.TrimPrefix(address, "http") {
		t.Fatal("legacy provider URL not upgraded without changing its signature")
	}
	secured := "https://source-bucket.oss-cn-hangzhou.aliyuncs.com/a.wav?Signature=fixture%3D"
	if got, ok = sessionAudioDownloadURL(secured); !ok || got != secured {
		t.Fatal("existing secure archive URL changed")
	}
	for _, raw := range []string{
		"http://bucket.aliyuncs.com/a.wav",
		"http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com:80/a.wav",
		"http://user:secret@dashscope-result-bj.oss-cn-beijing.aliyuncs.com/a.wav",
		"http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com.evil.test/a.wav",
		"http://dashscope-result-bj.oss-cn-beijing.evilaliyuncs.com/a.wav",
		"http://127.0.0.1/a.wav", "file:///etc/passwd",
	} {
		if _, ok = sessionAudioDownloadURL(raw); ok {
			t.Fatal("untrusted legacy URL was accepted")
		}
	}
}
