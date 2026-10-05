package model

import "testing"

func TestNormalizeHardwareMAC(t *testing.T) {
	for _, raw := range []string{"1C:29:04:31:0E:B8", "1c-29-04-31-0e-b8", "1C2904310EB8", "1c29.0431.0eb8"} {
		mac, err := NormalizeHardwareMAC(raw)
		if err != nil || mac != "1c:29:04:31:0e:b8" {
			t.Fatal(raw, mac, err)
		}
	}
	for _, raw := range []string{"", "aa:bb", "01:02:03:04:05:06:07:08", "zz:00:00:00:00:00"} {
		if _, err := NormalizeHardwareMAC(raw); err == nil {
			t.Fatal("invalid MAC accepted", raw)
		}
	}
}

func TestDeviceDefaultNameSequenceCompatibility(t *testing.T) {
	for name, want := range map[string]int64{"小蓝搭子": 1, "小蓝搭子02": 2, "小蓝直播助手": 1, "小蓝直播助手23": 23} {
		got, ok := DeviceDefaultNameSequence(name)
		if !ok || got != want {
			t.Fatal(name, got, ok)
		}
	}
	for _, name := range []string{"小蓝搭子01", "小蓝搭子2", "小蓝搭子002", "小蓝直播助手02自定义", "美妆助手"} {
		if _, ok := DeviceDefaultNameSequence(name); ok {
			t.Fatal("custom name treated as automatic", name)
		}
	}
}
