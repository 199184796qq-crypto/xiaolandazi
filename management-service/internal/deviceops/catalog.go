package deviceops

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const MaxFirmwareBytes = 0x3f0000

type FirmwareEntry struct {
	Enabled         bool     `json:"enabled"`
	Version         string   `json:"version"`
	SHA256          string   `json:"sha256"`
	Size            int64    `json:"size"`
	Board           string   `json:"board"`
	Chip            string   `json:"chip"`
	FlashBytes      int64    `json:"flash_bytes"`
	PartitionLayout string   `json:"partition_layout"`
	FromVersions    []string `json:"from_versions"`
	DeviceIDs       []int64  `json:"device_ids"`
}
type Catalog struct {
	Version int             `json:"version"`
	Entries []FirmwareEntry `json:"entries"`
}
type Offer struct {
	Version         string `json:"version"`
	URL             string `json:"url"`
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	Board           string `json:"board"`
	Chip            string `json:"chip"`
	FlashBytes      int64  `json:"flash_bytes"`
	PartitionLayout string `json:"partition_layout"`
}

func ReadCatalog(root string) (Catalog, error) {
	var c Catalog
	file, err := PrivatePath(root, "device-firmware/catalog.json")
	if err != nil {
		return c, err
	}
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return Catalog{Version: 1}, nil
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return c, err
	}
	if err := StrictJSON(raw, &c, 65536); err != nil {
		return c, err
	}
	if c.Version != 1 || len(c.Entries) > 32 {
		return c, errors.New("固件清单版本或条目数量无效")
	}
	seen := map[string]bool{}
	for _, e := range c.Entries {
		if !versionText.MatchString(e.Version) || !HexID(e.SHA256, 64, 64) || e.Size < 288 || e.Size > MaxFirmwareBytes || e.Board != "bread-compact-wifi-s3cam" || e.Chip != "esp32s3" || e.FlashBytes != 16<<20 || !safeName.MatchString(e.PartitionLayout) || len(e.DeviceIDs) == 0 || len(e.DeviceIDs) > 256 || len(e.FromVersions) == 0 || len(e.FromVersions) > 32 || seen[e.SHA256] {
			return c, errors.New("固件清单资料无效")
		}
		seen[e.SHA256] = true
		devices := map[int64]bool{}
		for _, id := range e.DeviceIDs {
			if id <= 0 || devices[id] {
				return c, errors.New("固件设备范围无效")
			}
			devices[id] = true
		}
		for _, v := range e.FromVersions {
			if !newerVersion(e.Version, v) {
				return c, errors.New("固件来源版本范围无效")
			}
		}
	}
	return c, nil
}
func newerVersion(next, current string) bool {
	a, ok := numericVersion(next)
	if !ok {
		return false
	}
	b, ok := numericVersion(current)
	if !ok {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
func numericVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 99999 || strconv.Itoa(n) != p {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
func (c Catalog) Select(profile Profile, deviceID int64, artifact string) (*FirmwareEntry, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	var found *FirmwareEntry
	for _, e := range c.Entries {
		if !e.Enabled || e.Board != profile.Board || e.Chip != profile.Chip || e.FlashBytes != profile.FlashBytes || e.PartitionLayout != profile.PartitionLayout || (artifact != "" && artifact != e.SHA256) {
			continue
		}
		allowed := false
		for _, id := range e.DeviceIDs {
			allowed = allowed || id == deviceID
		}
		source := false
		for _, v := range e.FromVersions {
			source = source || v == profile.FirmwareVersion
		}
		if !allowed || !source || !newerVersion(e.Version, profile.FirmwareVersion) {
			continue
		}
		if found == nil || newerVersion(e.Version, found.Version) {
			copy := e
			found = &copy
		}
	}
	return found, nil
}
func (e FirmwareEntry) Offer(url string) Offer {
	return Offer{Version: e.Version, URL: url, SHA256: e.SHA256, Size: e.Size, Board: e.Board, Chip: e.Chip, FlashBytes: e.FlashBytes, PartitionLayout: e.PartitionLayout}
}
func FirmwarePath(root, sha string) (string, error) {
	if !HexID(sha, 64, 64) {
		return "", errors.New("固件编号无效")
	}
	return PrivatePath(root, "device-firmware/"+sha+".bin")
}

// Require an ESP32-S3 application image with an app descriptor at the first
// segment. A merged flash/bootloader image is not a valid OTA application.
func ValidateFirmware(root string, e FirmwareEntry) (string, error) {
	path, err := FirmwarePath(root, e.SHA256)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxFirmwareBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(raw)) != e.Size || BatchChecksum(raw) != e.SHA256 {
		return "", errors.New("固件大小或校验值不匹配")
	}
	if len(raw) < 288 || raw[0] != 0xe9 || binary.LittleEndian.Uint16(raw[12:14]) != 9 || binary.LittleEndian.Uint32(raw[32:36]) != 0xabcd5432 {
		return "", errors.New("只允许匹配设备芯片的应用固件")
	}
	version := string(bytes.TrimRight(raw[48:80], "\x00"))
	if version != e.Version {
		return "", fmt.Errorf("应用固件版本与清单不匹配")
	}
	return path, nil
}
