package deviceops

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

const MaxBatchBytes = 32 << 10
const MaxBatchEvents = 128
const GrantTTL = 10 * time.Minute
const MaxDailyBytes = 16 << 20
const MaxDailyBatches = 1024
const MaxBatchesPerMinute = 6

var ErrForbidden = errors.New("设备归属或连接已变化，请重新连接后重试")
var ErrConflict = errors.New("日志批次编号已用于不同内容")
var ErrQuota = errors.New("设备日志上传过于频繁或已达到保留额度，请稍后重试")
var hexID = regexp.MustCompile(`^[a-f0-9]+$`)
var versionText = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,47}$`)
var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)
var connectionID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func HexID(value string, min, max int) bool {
	return len(value) >= min && len(value) <= max && hexID.MatchString(value)
}
func OwnerEpoch(secret string, tenant, device int64, claimedAt time.Time) string {
	h := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(h, "device-owner-v1\x00%d\x00%d\x00%s", tenant, device, claimedAt.UTC().Format(time.RFC3339Nano))
	return hex.EncodeToString(h.Sum(nil))
}

type Profile struct {
	Board           string `json:"board"`
	Chip            string `json:"chip"`
	FlashBytes      int64  `json:"flash_bytes"`
	PartitionLayout string `json:"partition_layout"`
	FirmwareVersion string `json:"firmware_version"`
	FirmwareSHA256  string `json:"firmware_sha256"`
	BootID          string `json:"boot_id"`
	Storage         string `json:"storage"`
}

func (p Profile) Validate() error {
	if p.Board != "bread-compact-wifi-s3cam" || p.Chip != "esp32s3" || p.FlashBytes != 16<<20 || !safeName.MatchString(p.PartitionLayout) || !versionText.MatchString(p.FirmwareVersion) || !HexID(p.FirmwareSHA256, 64, 64) || !HexID(p.BootID, 16, 64) {
		return errors.New("设备维护资料无效或硬件型号不匹配")
	}
	if p.Storage != "partition" && p.Storage != "rtc" && p.Storage != "ram" {
		return errors.New("设备日志存储类型无效")
	}
	return nil
}

type Identity struct {
	TenantID, DeviceID            int64
	MAC, ConnectionID, OwnerEpoch string
	Busy                          bool
}
type Event struct {
	Seq      uint64 `json:"seq"`
	BootID   string `json:"boot_id"`
	UptimeMS uint64 `json:"uptime_ms"`
	Code     string `json:"code"`
	Reason   *int64 `json:"reason,omitempty"`
	Value    *int64 `json:"value,omitempty"`
}
type Batch struct {
	Version         int     `json:"version"`
	BatchID         string  `json:"batch_id"`
	OwnerEpoch      string  `json:"owner_epoch"`
	FirmwareVersion string  `json:"firmware_version"`
	FirmwareSHA256  string  `json:"firmware_sha256"`
	Storage         string  `json:"storage"`
	Events          []Event `json:"events"`
}

var EventCodes = map[string]bool{"boot": true, "reset": true, "wifi_connected": true, "wifi_disconnected": true, "wifi_reconnect": true, "ws_connected": true, "ws_closed": true, "ws_timeout": true, "ota_check": true, "ota_start": true, "ota_verified": true, "ota_failed": true, "ota_complete": true, "log_dropped": true, "heap": true, "storage": true}

// StrictJSON rejects duplicate and unknown fields, trailing values and deep data.
func StrictJSON(raw []byte, out any, limit int) error {
	if len(raw) == 0 || len(raw) > limit {
		return errors.New("请求内容为空或超过大小限制")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueValue(d, 0); err != nil {
		return errors.New("请求内容格式错误或包含重复字段")
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("请求只能包含一份内容")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return errors.New("请求字段或数据类型无效")
	}
	return nil
}
func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 8 {
		return errors.New("内容层级过深")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return errors.New("重复字段")
			}
			seen[key] = true
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("内容无效")
	}
	_, err = d.Token()
	return err
}
func DecodeBatch(raw []byte) (Batch, error) {
	var b Batch
	if err := StrictJSON(raw, &b, MaxBatchBytes); err != nil {
		return b, err
	}
	if b.Version != 1 || !HexID(b.BatchID, 32, 64) || !HexID(b.OwnerEpoch, 64, 64) || !versionText.MatchString(b.FirmwareVersion) || !HexID(b.FirmwareSHA256, 64, 64) || len(b.Events) == 0 || len(b.Events) > MaxBatchEvents {
		return b, errors.New("日志批次资料无效")
	}
	if b.Storage != "partition" && b.Storage != "rtc" && b.Storage != "ram" {
		return b, errors.New("日志存储类型无效")
	}
	var seq, uptime uint64
	boot := ""
	for i, e := range b.Events {
		if !HexID(e.BootID, 16, 64) || !EventCodes[e.Code] || e.Seq > 1<<63-1 || e.UptimeMS > 1<<63-1 {
			return b, errors.New("日志记录包含未允许的内容")
		}
		if i > 0 && (e.BootID != boot || e.Seq <= seq || e.UptimeMS < uptime) {
			return b, errors.New("同一日志批次的启动标识或记录顺序无效")
		}
		if (e.Reason != nil && (*e.Reason < -1<<31 || *e.Reason > 1<<31-1)) || (e.Value != nil && (*e.Value < -1<<31 || *e.Value > 1<<31-1)) {
			return b, errors.New("日志数值超过允许范围")
		}
		seq, uptime, boot = e.Seq, e.UptimeMS, e.BootID
	}
	return b, nil
}
func BatchChecksum(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func BatchObjectKey(id Identity, b Batch, checksum string) string {
	return fmt.Sprintf("tenants/%d/devices/%d/diagnostics/%s/%s-%s.json", id.TenantID, id.DeviceID, b.OwnerEpoch, b.BatchID, checksum)
}
func ValidConnectionID(id string) bool {
	return connectionID.MatchString(id)
}

type StoredBatch struct {
	ID              int64     `json:"id"`
	TenantID        int64     `json:"-"`
	DeviceID        int64     `json:"device_id"`
	OwnerEpoch      string    `json:"owner_epoch"`
	BatchID         string    `json:"batch_id"`
	BootID          string    `json:"boot_id"`
	FirmwareVersion string    `json:"firmware_version"`
	FirmwareSHA256  string    `json:"firmware_sha256"`
	Storage         string    `json:"storage"`
	SeqStart        uint64    `json:"seq_start"`
	SeqEnd          uint64    `json:"seq_end"`
	EventCount      int       `json:"event_count"`
	SizeBytes       int64     `json:"size_bytes"`
	ChecksumSHA256  string    `json:"checksum_sha256"`
	ObjectKey       string    `json:"-"`
	StorageDriver   string    `json:"-"`
	ReceivedAt      time.Time `json:"received_at"`
}
