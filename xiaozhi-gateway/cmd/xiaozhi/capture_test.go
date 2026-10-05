package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testJPEG(t *testing.T, shade uint8) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{shade, shade, shade, 255})
		}
	}
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCaptureGrantTenantAuthorityAndReplay(t *testing.T) {
	var uploads atomic.Int64
	var tenant atomic.Int64
	tenant.Store(7)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Xiaozhi-Internal-Token") != "internal-unit-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/internal/v1/xiaozhi/provision":
			writeJSON(w, 200, provisioning{DeviceID: 9, TenantID: tenant.Load(), State: "claimed"})
		case "/internal/v1/xiaozhi/capture":
			uploads.Add(1)
			if r.Header.Get("X-Device-MAC") != controlTestMAC || r.Header.Get("X-Request-ID") != "ctl-capture-unit-0001" {
				t.Error("client tenant/device fields influenced internal authority")
			}
			if err := r.ParseMultipartForm(3 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			if _, err := jpeg.Decode(file); err != nil {
				t.Error(err)
			}
			writeJSON(w, 200, captureResponse{Accepted: true, RequestID: r.Header.Get("X-Request-ID"), EventID: 902, BillingMode: "disabled", ChargedBeans: 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	g := newGateway(config{managementBaseURL: backend.URL, internalToken: "internal-unit-token", secret: "private-unit-secret", publicWSURL: "wss://unit.example/xiaozhi/v1/"}, &bindingStore{data: map[string]int64{}})
	now := time.Now()
	lease, err := g.controls.acquire(controlTestMAC, "client-unit", "ctl-capture-unit-0001", provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}, now)
	if err != nil {
		t.Fatal(err)
	}
	address, err := g.captureURL(lease, now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(address)
	if parsed.Scheme != "https" || parsed.Host != "unit.example" || parsed.Path != "/xiaozhi/v1/capture" || strings.Contains(address, "internal-unit-token") {
		t.Fatal("unsafe public capture URL", address)
	}
	signed := parsed.Query().Get("grant")
	if grant, err := g.parseCaptureGrant(signed, now); err != nil || grant.TenantID != 7 || grant.ClientID != "client-unit" {
		t.Fatal(grant, err)
	}
	if _, err := g.parseCaptureGrant(signed, now.Add(121*time.Second)); err == nil {
		t.Fatal("expired grant accepted")
	}
	if _, err := g.parseCaptureGrant("A"+signed[1:], now); err == nil {
		t.Fatal("tampered grant accepted")
	}
	other := newGateway(config{secret: "another-secret"}, &bindingStore{data: map[string]int64{}})
	if _, err := other.parseCaptureGrant(signed, now); err == nil {
		t.Fatal("wrong signing key accepted")
	}
	post := func(address string, data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, _ := form.CreateFormFile("file", "environment.jpg")
		_, _ = file.Write(data)
		_ = form.WriteField("tenant_id", "99999")
		_ = form.WriteField("hardware_mac", "ff:ff:ff:ff:ff:ff")
		_ = form.WriteField("question", `{"tenant_id":99999,"request_id":"attacker-request"}`)
		_ = form.Close()
		r := httptest.NewRequest(http.MethodPost, address, &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		g.capture(w, r)
		return w
	}
	if w := post(address, []byte("not a JPEG")); w.Code != 415 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := post(address, make([]byte, maxCaptureBytes+1)); w.Code != 400 && w.Code != 413 {
		t.Fatal("oversized capture accepted", w.Code)
	}
	data := testJPEG(t, 60)
	w := post(address, data)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response captureResponse
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || !response.Accepted || response.EventID != 902 || response.ChargedBeans != 0 {
		t.Fatal(w.Body.String())
	}
	if !g.controls.captureAccepted(lease) {
		t.Fatal("backend receipt not retained")
	}
	if w := post(address, data); w.Code != 200 || uploads.Load() != 1 {
		t.Fatal("same image retry duplicated backend", w.Code, uploads.Load())
	}
	if w := post(address, testJPEG(t, 190)); w.Code != 409 || uploads.Load() != 1 {
		t.Fatal("different image grant replay accepted", w.Code)
	}
	tenant.Store(8)
	if w := post(address, data); w.Code != 403 {
		t.Fatal("capture crossed customer ownership", w.Code)
	}
	tenant.Store(7)
	g.controls.release(lease)
	if w := post(address, data); w.Code != 409 || uploads.Load() != 1 {
		t.Fatal("closed command grant replay accepted", w.Code)
	}
}

func TestCaptureGrantCannotBeUsedBeforeCommandOrOnOtherDevice(t *testing.T) {
	g := newGateway(config{secret: "private-unit-secret", publicWSURL: "wss://unit.example/xiaozhi/v1/"}, &bindingStore{data: map[string]int64{}})
	now := time.Now()
	lease, err := g.controls.acquire(controlTestMAC, "client-unit", "ctl-capture-unit-0002", provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}, now)
	if err != nil {
		t.Fatal(err)
	}
	defer g.controls.release(lease)
	address, err := g.captureURL(lease, now)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(address)
	grant, err := g.parseCaptureGrant(parsed.Query().Get("grant"), now)
	if err != nil {
		t.Fatal(err)
	}
	changed := grant
	changed.ClientID = "different-client"
	if _, err := g.controls.beginCapture(changed, [32]byte{}); err == nil {
		t.Fatal("grant crossed client identity")
	}
	changed = grant
	changed.MAC = "ff:ff:ff:ff:ff:ff"
	if _, err := g.controls.beginCapture(changed, [32]byte{}); err == nil {
		t.Fatal("grant crossed device identity")
	}
	changed = grant
	changed.TenantID = 999
	if _, err := g.controls.beginCapture(changed, [32]byte{}); err == nil {
		t.Fatal("grant crossed tenant identity")
	}
	if _, err := g.controls.beginCapture(grant, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.controls.beginCapture(grant, [32]byte{1}); err == nil {
		t.Fatal("concurrent upload accepted")
	}
}

func TestCaptureRejectsWrongBillingEvenWithAcceptedReceipt(t *testing.T) {
	for _, mode := range []string{"", "enabled"} {
		t.Run("billing-"+mode, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/internal/v1/xiaozhi/provision" {
					writeJSON(w, 200, provisioning{DeviceID: 9, TenantID: 7, State: "claimed"})
					return
				}
				writeJSON(w, 200, captureResponse{Accepted: true, RequestID: r.Header.Get("X-Request-ID"), EventID: 22, BillingMode: mode, ChargedBeans: 0})
			}))
			defer backend.Close()
			g := newGateway(config{managementBaseURL: backend.URL, secret: "test-secret", publicWSURL: "wss://unit.example/xiaozhi/v1/"}, &bindingStore{data: map[string]int64{}})
			lease, err := g.controls.acquire(controlTestMAC, "client-unit", "ctl-capture-billing-0001", provisioning{DeviceID: 9, TenantID: 7, State: "claimed"}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer g.controls.release(lease)
			address, err := g.captureURL(lease, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			file, _ := form.CreateFormFile("file", "environment.jpg")
			_, _ = file.Write(testJPEG(t, 20))
			_ = form.Close()
			r := httptest.NewRequest(http.MethodPost, address, &body)
			r.Header.Set("Content-Type", form.FormDataContentType())
			w := httptest.NewRecorder()
			g.capture(w, r)
			if w.Code != 502 || g.controls.captureAccepted(lease) {
				t.Fatal("incorrect billing receipt enabled success ACK", w.Code, w.Body.String())
			}
		})
	}
}
