package model

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const DefaultDeviceName = "小蓝搭子"

func NormalizeHardwareMAC(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if len(value) == 12 && !strings.ContainsAny(value, ":-.") {
		var parts []string
		for i := 0; i < len(value); i += 2 {
			parts = append(parts, value[i:i+2])
		}
		value = strings.Join(parts, ":")
	}
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 {
		return "", fmt.Errorf("请输入有效的设备 MAC 地址")
	}
	return mac.String(), nil
}

func DefaultDeviceNameForSequence(sequence int64) string {
	if sequence <= 1 {
		return DefaultDeviceName
	}
	return fmt.Sprintf("%s%02d", DefaultDeviceName, sequence)
}

// Only canonical generated names contribute to the monotonic name sequence.
// Stored legacy/user names are never rewritten by this compatibility helper.
func DeviceDefaultNameSequence(name string) (int64, bool) {
	for _, prefix := range []string{DefaultDeviceName, "小蓝直播助手"} {
		if name == prefix {
			return 1, true
		}
		if suffix := strings.TrimPrefix(name, prefix); suffix != name {
			seq, err := strconv.ParseInt(suffix, 10, 64)
			if err == nil && seq >= 2 && seq < 1000000000 && name == fmt.Sprintf("%s%02d", prefix, seq) {
				return seq, true
			}
		}
	}
	return 0, false
}

type DeviceProvisioning struct {
	DeviceID    int64      `json:"device_id,omitempty"`
	DeviceName  string     `json:"device_name,omitempty"`
	TenantID    int64      `json:"tenant_id,omitempty"`
	RoomID      int64      `json:"room_id,omitempty"`
	BindingRole string     `json:"binding_role,omitempty"`
	RoomName    string     `json:"room_name,omitempty"`
	RoomNumber  string     `json:"room_number,omitempty"`
	State       string     `json:"state"`
	Message     string     `json:"message"`
	Code        string     `json:"binding_code,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type ClaimDeviceInput struct {
	BindingCode string `json:"binding_code"`
	DeviceName  string `json:"device_name"`
}
