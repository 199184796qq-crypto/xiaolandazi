package deviceops

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testProfile() Profile {
	return Profile{Board: "bread-compact-wifi-s3cam", Chip: "esp32s3", FlashBytes: 16 << 20, PartitionLayout: "v2-16m", FirmwareVersion: "2.2.6", FirmwareSHA256: strings.Repeat("c", 64), BootID: strings.Repeat("b", 32), Storage: "ram"}
}
func testBatch() Batch {
	return Batch{Version: 1, BatchID: strings.Repeat("a", 32), OwnerEpoch: strings.Repeat("e", 64), FirmwareVersion: "2.2.7", FirmwareSHA256: strings.Repeat("f", 64), Storage: "partition", Events: []Event{{Seq: 1, BootID: strings.Repeat("b", 32), UptimeMS: 12, Code: "boot"}, {Seq: 2, BootID: strings.Repeat("b", 32), UptimeMS: 14, Code: "ws_closed"}}}
}
func TestDiagnosticBatchStrictAndBounded(t *testing.T) {
	valid, _ := json.Marshal(testBatch())
	if _, err := DecodeBatch(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Batch){func(b *Batch) { b.Version = 2 }, func(b *Batch) { b.Events[0].Code = "raw_log" }, func(b *Batch) { b.Events[0].Reason = new(int64); *b.Events[0].Reason = 1 << 40 }, func(b *Batch) { b.Events[1].Seq = 1 }, func(b *Batch) { b.Events[1].BootID = strings.Repeat("c", 32) }, func(b *Batch) { b.Events[1].UptimeMS = 1 }, func(b *Batch) { b.Events = make([]Event, 129) }, func(b *Batch) { b.OwnerEpoch = "../14" }, func(b *Batch) { b.Storage = "wifi_nvs" }} {
		b := testBatch()
		change(&b)
		raw, _ := json.Marshal(b)
		if _, err := DecodeBatch(raw); err == nil {
			t.Fatal("unsafe batch accepted", string(raw))
		}
	}
	for _, raw := range [][]byte{append(valid, []byte(" {}")...), []byte(strings.Replace(string(valid), `"version":1`, `"version":1,"version":1`, 1)), []byte(strings.Replace(string(valid), `"version":1`, `"version":1,"tenant_id":15`, 1)), []byte(strings.Replace(string(valid), `"code":"boot"`, `"code":"boot","text":"私有内容"`, 1)), append(valid, []byte(strings.Repeat(" ", MaxBatchBytes))...)} {
		if _, err := DecodeBatch(raw); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	if StrictJSON([]byte(strings.Repeat("[", 16)+strings.Repeat("]", 16)), new(any), 100) == nil {
		t.Fatal("deep JSON accepted")
	}
}
func TestDeviceMaintenancePurposeExpiryAndOwnershipEpoch(t *testing.T) {
	secret := "maintenance-test-secret-012345"
	now := time.Now().Truncate(time.Second)
	signer, err := NewSigner(secret)
	if err != nil {
		t.Fatal(err)
	}
	epoch := OwnerEpoch(secret, 14, 7, now)
	g := Grant{Purpose: PurposeDiagnostics, TenantID: 14, DeviceID: 7, OwnerEpoch: epoch}
	token, err := signer.Issue(g, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := signer.Verify(token, PurposeDiagnostics, now.Add(9*time.Minute))
	if err != nil || got.OwnerEpoch != epoch {
		t.Fatal(got, err)
	}
	for _, tc := range []struct {
		token, purpose string
		at             time.Time
	}{{token, PurposeFirmware, now}, {token, PurposeDiagnostics, now.Add(10 * time.Minute)}, {token + "a", PurposeDiagnostics, now}, {"public-weak-websocket-token", PurposeDiagnostics, now}} {
		if _, err := signer.Verify(tc.token, tc.purpose, tc.at); err == nil {
			t.Fatal("invalid grant accepted")
		}
	}
	if epoch == OwnerEpoch(secret, 14, 7, now.Add(time.Millisecond)) || epoch == OwnerEpoch(secret, 15, 7, now) {
		t.Fatal("ownership transfer did not rotate epoch")
	}
}
func writeCatalog(t *testing.T, root string, e FirmwareEntry) {
	t.Helper()
	os.MkdirAll(filepath.Join(root, "device-firmware"), 0700)
	raw, _ := json.Marshal(Catalog{Version: 1, Entries: []FirmwareEntry{e}})
	if err := os.WriteFile(filepath.Join(root, "device-firmware/catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestFirmwareCatalogDefaultsCompatibilityAndAppOnly(t *testing.T) {
	root := t.TempDir()
	catalog, err := ReadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if offer, err := catalog.Select(testProfile(), 7, ""); err != nil || offer != nil {
		t.Fatal(offer, err)
	}
	image := make([]byte, 320)
	image[0] = 0xe9
	binary.LittleEndian.PutUint16(image[12:14], 9)
	binary.LittleEndian.PutUint32(image[32:36], 0xabcd5432)
	copy(image[48:80], "2.2.7")
	e := FirmwareEntry{Enabled: true, Version: "2.2.7", SHA256: BatchChecksum(image), Size: int64(len(image)), Board: "bread-compact-wifi-s3cam", Chip: "esp32s3", FlashBytes: 16 << 20, PartitionLayout: "v2-16m", FromVersions: []string{"2.2.6"}, DeviceIDs: []int64{7}}
	writeCatalog(t, root, e)
	path, _ := FirmwarePath(root, e.SHA256)
	os.WriteFile(path, image, 0600)
	catalog, err = ReadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := catalog.Select(testProfile(), 7, "")
	if err != nil || offer == nil {
		t.Fatal(offer, err)
	}
	if _, err := ValidateFirmware(root, *offer); err != nil {
		t.Fatal(err)
	}
	if found, _ := catalog.Select(testProfile(), 8, ""); found != nil {
		t.Fatal("unlisted device offered")
	}
	p := testProfile()
	p.PartitionLayout = "legacy"
	if found, _ := catalog.Select(p, 7, ""); found != nil {
		t.Fatal("wrong partition offered")
	}
	e.Enabled = false
	writeCatalog(t, root, e)
	catalog, _ = ReadCatalog(root)
	if found, _ := catalog.Select(testProfile(), 7, ""); found != nil {
		t.Fatal("disabled firmware offered")
	}
	image[32] = 0
	bad := e
	bad.Enabled = true
	bad.SHA256 = BatchChecksum(image)
	badpath, _ := FirmwarePath(root, bad.SHA256)
	os.WriteFile(badpath, image, 0600)
	if _, err := ValidateFirmware(root, bad); err == nil {
		t.Fatal("bootloader/merged image accepted")
	}
	os.WriteFile(path, []byte("truncated"), 0600)
	if _, err := ValidateFirmware(root, e); err == nil {
		t.Fatal("corrupt artifact accepted")
	}
}
func TestDiagnosticPrivateImmutableWrite(t *testing.T) {
	root := t.TempDir()
	b := testBatch()
	raw, _ := json.Marshal(b)
	id := Identity{TenantID: 14, DeviceID: 7}
	key := BatchObjectKey(id, b, BatchChecksum(raw))
	if err := SaveBatch(root, key, raw); err != nil {
		t.Fatal(err)
	}
	if err := SaveBatch(root, key, raw); err != nil {
		t.Fatal(err)
	}
	if err := SaveBatch(root, key, []byte("different")); err != ErrConflict {
		t.Fatal(err)
	}
	for _, key := range []string{"../outside", "/etc/passwd", ""} {
		if _, err := PrivatePath(root, key); err == nil {
			t.Fatal("unsafe path accepted", key)
		}
	}
	path, _ := PrivatePath(root, key)
	got, _ := os.ReadFile(path)
	if string(got) != string(raw) {
		t.Fatal("immutable object changed")
	}
	if err := SyncPrivateDirectories(root, filepath.Dir(root)); err == nil {
		t.Fatal("directory sync escaped private root")
	}
}
