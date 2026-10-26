package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/auth"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/deviceops"
)

type maintenanceFakeStore struct {
	mu          sync.Mutex
	identity    deviceops.Identity
	fail        error
	entries     map[string]deviceops.StoredBatch
	writes      int
	resolvedCID string
}

func (f *maintenanceFakeStore) ResolveDeviceMaintenanceIdentity(_ context.Context, mac, cid, secret string) (deviceops.Identity, error) {
	f.resolvedCID = cid
	if f.fail != nil {
		return deviceops.Identity{}, f.fail
	}
	if mac != "1c:29:04:31:0e:b8" || (cid != "" && cid != "connection-current-0001") {
		return deviceops.Identity{}, deviceops.ErrForbidden
	}
	return f.identity, nil
}
func (f *maintenanceFakeStore) CheckDeviceMaintenanceOwner(_ context.Context, id deviceops.Identity, _ string) (deviceops.Identity, error) {
	if f.fail != nil {
		return deviceops.Identity{}, f.fail
	}
	if id.TenantID != f.identity.TenantID || id.DeviceID != f.identity.DeviceID || id.OwnerEpoch != f.identity.OwnerEpoch {
		return deviceops.Identity{}, deviceops.ErrForbidden
	}
	return f.identity, nil
}
func (f *maintenanceFakeStore) RecordDeviceDiagnosticBatch(ctx context.Context, b deviceops.StoredBatch, secret string, write func() error) (deviceops.StoredBatch, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.CheckDeviceMaintenanceOwner(ctx, deviceops.Identity{TenantID: b.TenantID, DeviceID: b.DeviceID, OwnerEpoch: b.OwnerEpoch}, secret); err != nil {
		return b, false, err
	}
	key := b.OwnerEpoch + b.BatchID
	if old, ok := f.entries[key]; ok {
		if old.ChecksumSHA256 != b.ChecksumSHA256 {
			return b, false, deviceops.ErrConflict
		}
		if err := write(); err != nil {
			return b, false, err
		}
		return old, true, nil
	}
	if err := write(); err != nil {
		return b, false, err
	}
	f.writes++
	b.ID = int64(len(f.entries) + 1)
	b.ReceivedAt = time.Now().UTC()
	f.entries[key] = b
	return b, false, nil
}
func (f *maintenanceFakeStore) ListDeviceDiagnosticBatches(_ context.Context, tenant, device, before int64, limit int) ([]deviceops.StoredBatch, error) {
	out := []deviceops.StoredBatch{}
	for _, b := range f.entries {
		if b.TenantID == tenant && b.DeviceID == device {
			out = append(out, b)
		}
	}
	return out, nil
}
func (f *maintenanceFakeStore) GetDeviceDiagnosticBatch(_ context.Context, tenant, device int64, epoch, batch string) (deviceops.StoredBatch, error) {
	b, ok := f.entries[epoch+batch]
	if !ok || b.TenantID != tenant || b.DeviceID != device {
		return b, sql.ErrNoRows
	}
	return b, nil
}
func maintenanceTestServer(t *testing.T) (*Server, *maintenanceFakeStore) {
	t.Helper()
	signer, err := deviceops.NewSigner("maintenance-test-secret-0123")
	if err != nil {
		t.Fatal(err)
	}
	f := &maintenanceFakeStore{identity: deviceops.Identity{TenantID: 14, DeviceID: 7, OwnerEpoch: strings.Repeat("e", 64)}, entries: map[string]deviceops.StoredBatch{}}
	s := &Server{auth: auth.NewResolver("test", nil), xiaozhiInternalToken: "maintenance-test-secret-0123", deviceMaintenance: &deviceMaintenanceState{root: t.TempDir(), base: "https://example.invalid", secret: "maintenance-test-secret-0123", signer: signer, store: f}}
	return s, f
}
func maintenanceProfile() deviceops.Profile {
	return deviceops.Profile{Board: "bread-compact-wifi-s3cam", Chip: "esp32s3", FlashBytes: 16 << 20, PartitionLayout: "v2-16m", FirmwareVersion: "2.2.6", FirmwareSHA256: strings.Repeat("f", 64), BootID: strings.Repeat("b", 32), Storage: "rtc"}
}
func maintenancePayload(epoch string) []byte {
	b := deviceops.Batch{Version: 1, BatchID: strings.Repeat("a", 32), OwnerEpoch: epoch, FirmwareVersion: "2.2.7", FirmwareSHA256: strings.Repeat("d", 64), Storage: "partition", Events: []deviceops.Event{{Seq: 1, BootID: strings.Repeat("c", 32), UptimeMS: 1, Code: "boot"}, {Seq: 2, BootID: strings.Repeat("c", 32), UptimeMS: 2, Code: "wifi_disconnected"}}}
	raw, _ := json.Marshal(b)
	return raw
}
func uploadMaintenance(s *Server, token string, raw []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/device-diagnostics/upload", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.deviceDiagnosticsUpload(w, r)
	return w
}
func issueTestDiagnostics(t *testing.T, s *Server, id deviceops.Identity, at time.Time) string {
	t.Helper()
	token, err := s.deviceMaintenance.signer.Issue(deviceops.Grant{Purpose: deviceops.PurposeDiagnostics, TenantID: id.TenantID, DeviceID: id.DeviceID, OwnerEpoch: id.OwnerEpoch}, at)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func TestDeviceMaintenanceInternalCIDDefaultNoOfferAndUnavailable(t *testing.T) {
	s, f := maintenanceTestServer(t)
	input := maintenanceInput{HardwareMAC: "1c:29:04:31:0e:b8", ConnectionID: "connection-current-0001", DeviceProfile: maintenanceProfile()}
	raw, _ := json.Marshal(input)
	call := func(token string, check bool, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/internal/v1/xiaozhi/maintenance", bytes.NewReader(body))
		r.Header.Set("X-Xiaozhi-Internal-Token", token)
		w := httptest.NewRecorder()
		if check {
			s.deviceFirmwareCheck(w, r)
		} else {
			s.deviceMaintenanceGrant(w, r)
		}
		return w
	}
	if w := call("", false, raw); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := call(s.xiaozhiInternalToken, false, raw)
	if w.Code != 200 || f.resolvedCID != input.ConnectionID {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Diagnostics struct {
			Token, OwnerEpoch string
			MaxBatchBytes     int `json:"max_batch_bytes"`
			MaxBatchEvents    int `json:"max_batch_events"`
		} `json:"diagnostics"`
		Firmware any `json:"firmware"`
	}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response.Firmware != nil || response.Diagnostics.Token == "" || response.Diagnostics.MaxBatchBytes != 32768 || response.Diagnostics.MaxBatchEvents != 128 {
		t.Fatal(w.Body.String())
	}
	g, err := s.deviceMaintenance.signer.Verify(response.Diagnostics.Token, deviceops.PurposeDiagnostics, time.Now())
	if err != nil || g.TenantID != 14 || g.DeviceID != 7 {
		t.Fatal(g, err)
	}
	w = call(s.xiaozhiInternalToken, true, raw)
	if w.Code != 200 || strings.Contains(w.Body.String(), "diagnostics") {
		t.Fatal("startup query issued log grant", w.Body.String())
	}
	input.ConnectionID = "connection-old-0001"
	raw, _ = json.Marshal(input)
	if w = call(s.xiaozhiInternalToken, false, raw); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.fail = errors.New("private upstream details")
	if w = call(s.xiaozhiInternalToken, false, raw); w.Code != 503 || strings.Contains(w.Body.String(), "upstream") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestDeviceMaintenanceUploadGrantDedupeOwnerIsolationAndNoFalseACK(t *testing.T) {
	s, f := maintenanceTestServer(t)
	token := issueTestDiagnostics(t, s, f.identity, time.Now())
	raw := maintenancePayload(f.identity.OwnerEpoch)
	for i := 0; i < 2; i++ {
		w := uploadMaintenance(s, token, raw)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"accepted":true`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if f.writes != 1 || len(f.entries) != 1 {
		t.Fatal("duplicate stored twice")
	}
	different := bytes.Replace(raw, []byte(`"uptime_ms":2`), []byte(`"uptime_ms":3`), 1)
	if w := uploadMaintenance(s, token, different); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := uploadMaintenance(s, "public-websocket-token", raw); w.Code != 401 {
		t.Fatal(w.Code)
	}
	old := issueTestDiagnostics(t, s, f.identity, time.Now().Add(-11*time.Minute))
	if w := uploadMaintenance(s, old, raw); w.Code != 401 {
		t.Fatal(w.Code)
	}
	bad := bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"tenant_id":15,"path":"/tmp/other"`), 1)
	if w := uploadMaintenance(s, token, bad); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := uploadMaintenance(s, token, append(raw, bytes.Repeat([]byte(" "), 32768)...)); w.Code != 400 {
		t.Fatal(w.Code)
	}
	f.identity.TenantID = 15
	if w := uploadMaintenance(s, token, raw); w.Code != 403 {
		t.Fatal("old-owner grant accepted", w.Code, w.Body.String())
	}
	f.identity.TenantID = 14
	f.identity.OwnerEpoch = strings.Repeat("d", 64)
	if w := uploadMaintenance(s, token, raw); w.Code != 403 {
		t.Fatal("old epoch accepted", w.Code)
	}
	f.identity.OwnerEpoch = strings.Repeat("e", 64)
	f.fail = errors.New("database index commit failed")
	if w := uploadMaintenance(s, token, raw); w.Code != 503 || strings.Contains(w.Body.String(), `"accepted":true`) {
		t.Fatal("unavailable ledger ACKed", w.Code, w.Body.String())
	}
}
func TestDeviceMaintenanceTenantListsAndDownloadsArePrivate(t *testing.T) {
	s, f := maintenanceTestServer(t)
	raw := maintenancePayload(f.identity.OwnerEpoch)
	token := issueTestDiagnostics(t, s, f.identity, time.Now())
	if w := uploadMaintenance(s, token, raw); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	request := func(path string) *http.Request {
		r := httptest.NewRequest("GET", path, nil)
		r.SetPathValue("deviceID", "7")
		r.SetPathValue("batchID", strings.Repeat("a", 32))
		return r
	}
	for _, h := range []http.HandlerFunc{s.deviceDiagnosticsList, s.deviceDiagnosticsDownload} {
		w := httptest.NewRecorder()
		h(w, request("/"))
		if w.Code != 401 {
			t.Fatal("unauthenticated diagnostics read", w.Code)
		}
	}
	for _, tenant := range []int64{14, 15} {
		w := httptest.NewRecorder()
		s.deviceDiagnosticsListForTenant(w, request("/?tenant_id=14"), tenant)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		var body struct {
			Items []deviceops.StoredBatch `json:"items"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		if tenant == 14 && len(body.Items) != 1 || tenant == 15 && len(body.Items) != 0 {
			t.Fatal("cross-tenant list", tenant, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "object_key") {
			t.Fatal("private key exposed")
		}
		w = httptest.NewRecorder()
		s.deviceDiagnosticsDownloadForTenant(w, request("/?owner_epoch="+f.identity.OwnerEpoch+"&tenant_id=14"), tenant)
		if tenant == 14 && (w.Code != 200 || !bytes.Equal(w.Body.Bytes(), raw)) {
			t.Fatal(w.Code, w.Body.String())
		}
		if tenant == 15 && w.Code != 404 {
			t.Fatal("cross-tenant file read", w.Code)
		}
	}
}
func TestDeviceMaintenancePrivateRootOutsideGenericMedia(t *testing.T) {
	media := filepath.Join(t.TempDir(), "media")
	os.MkdirAll(media, 0755)
	s := &Server{store: &appdb.Store{}, env: "test", publicWebURL: "https://example.invalid", xiaozhiInternalToken: "maintenance-test-secret-0123"}
	if err := s.InitDeviceMaintenance(media); err != nil {
		t.Fatal(err)
	}
	if s.deviceMaintenance.root != media+"-device-private" {
		t.Fatal(s.deviceMaintenance.root)
	}
	b := maintenancePayload(strings.Repeat("e", 64))
	key := "tenants/14/devices/7/diagnostics/private.json"
	if err := deviceops.SaveBatch(s.deviceMaintenance.root, key, b); err != nil {
		t.Fatal(err)
	}
	public := http.StripPrefix("/media/", http.FileServer(http.Dir(media)))
	w := httptest.NewRecorder()
	public.ServeHTTP(w, httptest.NewRequest("GET", "/media/"+key, nil))
	if w.Code != 404 {
		t.Fatal("generic media exposed private device logs", w.Code)
	}
	s.registerDeviceMaintenanceRoutes(http.NewServeMux())
}
func TestDeviceMaintenanceFirmwareNoLiveOrWrongPurpose(t *testing.T) {
	s, f := maintenanceTestServer(t)
	image := make([]byte, 320)
	image[0] = 0xe9
	binary.LittleEndian.PutUint16(image[12:14], 9)
	binary.LittleEndian.PutUint32(image[32:36], 0xabcd5432)
	copy(image[48:80], "2.2.7")
	e := deviceops.FirmwareEntry{Enabled: true, Version: "2.2.7", SHA256: deviceops.BatchChecksum(image), Size: int64(len(image)), Board: "bread-compact-wifi-s3cam", Chip: "esp32s3", FlashBytes: 16 << 20, PartitionLayout: "v2-16m", FromVersions: []string{"2.2.6"}, DeviceIDs: []int64{7}}
	dir := filepath.Join(s.deviceMaintenance.root, "device-firmware")
	os.MkdirAll(dir, 0700)
	catalog, _ := json.Marshal(deviceops.Catalog{Version: 1, Entries: []deviceops.FirmwareEntry{e}})
	os.WriteFile(filepath.Join(dir, "catalog.json"), catalog, 0600)
	path, _ := deviceops.FirmwarePath(s.deviceMaintenance.root, e.SHA256)
	os.WriteFile(path, image, 0600)
	offer, err := s.deviceMaintenance.firmwareOffer(f.identity, maintenanceProfile(), time.Now())
	if err != nil || offer == nil {
		t.Fatal(offer, err)
	}
	u, _ := url.Parse(offer.URL)
	call := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", u.RequestURI(), nil)
		r.SetPathValue("sha256", e.SHA256)
		w := httptest.NewRecorder()
		s.deviceFirmwareDownload(w, r)
		return w
	}
	if w := call(); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), image) {
		t.Fatal(w.Code, w.Body.String())
	}
	f.identity.Busy = true
	if w := call(); w.Code != 409 {
		t.Fatal("active live allowed upgrade", w.Code)
	}
	if offer, err := s.deviceMaintenance.firmwareOffer(f.identity, maintenanceProfile(), time.Now()); err != nil || offer != nil {
		t.Fatal("busy offer", offer, err)
	}
	f.identity.Busy = false
	f.identity.OwnerEpoch = strings.Repeat("c", 64)
	if w := call(); w.Code != 403 {
		t.Fatal("old owner firmware grant", w.Code)
	}
	g, _ := s.deviceMaintenance.signer.Issue(deviceops.Grant{Purpose: deviceops.PurposeFirmware, TenantID: f.identity.TenantID, DeviceID: f.identity.DeviceID, OwnerEpoch: f.identity.OwnerEpoch, ArtifactSHA256: e.SHA256, Profile: func() *deviceops.Profile { p := maintenanceProfile(); return &p }()}, time.Now())
	if w := uploadMaintenance(s, g, maintenancePayload(f.identity.OwnerEpoch)); w.Code != 401 {
		t.Fatal("firmware grant uploaded logs", w.Code)
	}
}
