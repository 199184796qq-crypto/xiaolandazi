package httpapi

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"livecompanion/management/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnchorRecommendationRoutesRequireAuthentication(t *testing.T) {
	server := &Server{auth: auth.NewResolver("test", nil)}
	mux := http.NewServeMux()
	server.registerAnchorRecommendationRoutes(mux)
	for _, route := range [][2]string{
		{"POST", "/api/v1/live-anchor-styles/7/avatar-image"}, {"GET", "/api/v1/live-anchor-styles/current?plan_id=4"},
		{"GET", "/api/v1/live/anchor-recommendations"}, {"POST", "/api/v1/live/anchor-recommendations/import"},
		{"GET", "/api/v1/system/anchor-recommendations"}, {"GET", "/api/v1/system/anchor-recommendations/sources"},
		{"POST", "/api/v1/system/anchor-recommendations/preview"}, {"POST", "/api/v1/system/anchor-recommendations"}, {"POST", "/api/v1/system/anchor-recommendations/1/withdraw"},
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(route[0], route[1], strings.NewReader("{}")))
		if recorder.Code != 401 {
			t.Fatalf("route %v exposed data or mutation: %d", route, recorder.Code)
		}
	}
}

func TestRecommendationSummaryProtocolAnd50UnicodeCharacters(t *testing.T) {
	expected := strings.Repeat("风格自然", 12) + "亲切"
	got, err := parseAnchorRecommendationSummary(`{"summary":"` + expected + `"}`)
	if err != nil || got != expected {
		t.Fatal(got, err)
	}
	for _, raw := range []string{`not json`, `{"summary":"简短的风格"}`, `{"summary":"` + strings.Repeat("长", 51) + `"}`, `{"summary":"` + expected + `","unknown":true}`} {
		if _, err := parseAnchorRecommendationSummary(raw); err == nil {
			t.Fatal("invalid model protocol accepted", raw)
		}
	}
	raw := `{"summary":"保留未经截断的原始返回"}`
	got, err = parseAnchorRecommendationSummary(raw)
	if err == nil || got != "保留未经截断的原始返回" {
		t.Fatal("invalid length must retain original output")
	}
}
func TestAnchorAvatarNormalizesImageWithoutTrustingFileName(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 24, 24))
	img.Set(3, 3, color.RGBA{R: 255, A: 255})
	var raw bytes.Buffer
	_ = png.Encode(&raw, img)
	avatar, err := normalizeAnchorAvatar(raw.Bytes())
	if err != nil || !strings.HasPrefix(avatar, "data:image/jpeg;base64,") {
		t.Fatal(avatar, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(avatar, "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || format != "jpeg" || cfg.Width != 24 {
		t.Fatal(cfg, format, err)
	}
	for _, bad := range [][]byte{[]byte(`<svg onload="alert(1)"/>`), []byte("image.png"), make([]byte, (1<<20)+1)} {
		if _, err := normalizeAnchorAvatar(bad); err == nil {
			t.Fatal("invalid or oversized image accepted")
		}
	}
	raw.Reset()
	_ = png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 1025, 1)))
	if _, err := normalizeAnchorAvatar(raw.Bytes()); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}
