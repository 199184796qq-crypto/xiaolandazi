package danmakutest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type regionRow struct {
	Province string `json:"省"`
	City     string `json:"市"`
	District string `json:"区县"`
}

var (
	regionOnce sync.Once
	regionMap  map[string]string
)

func loadRegionMap() {
	regionMap = map[string]string{}
	paths := []string{}
	if file := strings.TrimSpace(os.Getenv("REGION_DATA_FILE")); file != "" {
		paths = append(paths, file)
	}
	if cwd, err := os.Getwd(); err == nil {
		for i := 0; i < 8 && cwd != ""; i++ {
			paths = append(paths, filepath.Join(cwd, "地理名称表", "regions.json"))
			parent := filepath.Dir(cwd)
			if parent == cwd {
				break
			}
			cwd = parent
		}
	}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "..", "data", "regions.json"))
	}
	paths = append(paths, "/opt/xiaolan/current/data/regions.json")
	var payload struct {
		Rows []regionRow `json:"rows"`
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if json.Unmarshal(raw, &payload) != nil || len(payload.Rows) == 0 {
			continue
		}
		for _, row := range payload.Rows {
			province := strings.TrimSpace(row.Province)
			for _, name := range []string{row.Province, row.City} {
				if key := regionKey(name); key != "" && province != "" {
					if _, exists := regionMap[key]; !exists {
						regionMap[key] = province
					}
				}
			}
		}
		return
	}
}

func regionKey(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "特别行政区")
	value = strings.TrimSuffix(value, "自治区")
	value = strings.TrimSuffix(value, "省")
	value = strings.TrimSuffix(value, "市")
	value = strings.TrimSuffix(value, "区")
	value = strings.TrimSuffix(value, "县")
	return strings.Join(strings.Fields(value), "")
}

func regionProvince(value string) string {
	regionOnce.Do(loadRegionMap)
	key := regionKey(value)
	if key == "" {
		return ""
	}
	return regionMap[key]
}

func regionEquivalent(a, b string) bool {
	pa, pb := regionProvince(a), regionProvince(b)
	if pa != "" && pb != "" {
		return pa == pb
	}
	return regionKey(a) != "" && regionKey(a) == regionKey(b)
}
