package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// geoTable 读取仓库里的「地理名称表」：把观众提到的地区解析成省份，
// 再把事实条件里的地区分支（川渝地区 / 其它地区 / 7个偏远省份）与省份对齐。
// 这样“我上海的”不会被模型猜成川渝，而是程序确认“上海市 → 其它地区”。
type geoTable struct {
	provinceByKey map[string]string
	alias         map[string][]string
	keys          []string
}

type geoRegions struct {
	Rows []struct {
		Province string `json:"省"`
		Level    string `json:"层级"`
		Path     string `json:"全路径"`
		Keys     string `json:"检索键"`
	} `json:"rows"`
}

func loadGeoTable() *geoTable {
	root := findRepoFile(filepath.Join("地理名称表", "regions.json"))
	if root == "" {
		return nil
	}
	raw, err := os.ReadFile(root)
	if err != nil {
		return nil
	}
	var parsed geoRegions
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	table := &geoTable{provinceByKey: map[string]string{}, alias: map[string][]string{}}
	// 省级优先：先登记省级，市级/区县级只补空缺，避免“朝阳”这类同名把省级覆盖掉。
	for _, row := range parsed.Rows {
		if row.Province == "" || row.Level != "省级" {
			continue
		}
		table.provinceByKey[row.Province] = row.Province
		for _, key := range strings.Split(row.Keys, "|") {
			if key = strings.TrimSpace(key); key != "" {
				table.provinceByKey[key] = row.Province
			}
		}
	}
	for _, row := range parsed.Rows {
		if row.Province == "" || row.Level == "省级" {
			continue
		}
		if _, exists := table.provinceByKey[row.Province]; !exists {
			table.provinceByKey[row.Province] = row.Province
		}
		for _, key := range strings.Split(row.Keys, "|") {
			if key = strings.TrimSpace(key); key != "" {
				if _, exists := table.provinceByKey[key]; !exists {
					table.provinceByKey[key] = row.Province
				}
			}
		}
	}
	for key := range table.provinceByKey {
		if len([]rune(key)) >= 2 {
			table.keys = append(table.keys, key)
		}
	}
	sort.Slice(table.keys, func(i, j int) bool {
		return len([]rune(table.keys[i])) > len([]rune(table.keys[j]))
	})
	if aliasPath := findRepoFile(filepath.Join("地理名称表", "aliases.json")); aliasPath != "" {
		if aliasRaw, err := os.ReadFile(aliasPath); err == nil {
			_ = json.Unmarshal(aliasRaw, &table.alias)
		}
	}
	return table
}

// findRepoFile 从当前目录向上找仓库里的文件（和 interactionpolicy.Read 同样的策略）。
func findRepoFile(relative string) string {
	cwd, _ := os.Getwd()
	for i := 0; i < 8 && cwd != ""; i++ {
		candidate := filepath.Join(cwd, relative)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "..", "..", "..", relative)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// provincesIn 返回文本里提到的省份（去重）；别名（川渝/江浙沪…）展开成多个省。
func (g *geoTable) provincesIn(text string) []string {
	if g == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	found := []string{}
	seen := map[string]bool{}
	add := func(province string) {
		if province != "" && !seen[province] {
			seen[province] = true
			found = append(found, province)
		}
	}
	for alias, provinces := range g.alias {
		if len([]rune(alias)) >= 2 && strings.Contains(text, alias) {
			for _, province := range provinces {
				add(province)
			}
		}
	}
	for _, key := range g.keys {
		if strings.Contains(text, key) {
			add(g.provinceByKey[key])
		}
	}
	return found
}

// expandRegion 把条件值（“川渝地区”“海南省”“其它地区”）解析成省份集合；
// 返回 (省份集合, 是否为“其它地区”分支)。
func (g *geoTable) expandRegion(value string) ([]string, bool) {
	value = strings.TrimSpace(value)
	if g == nil || value == "" {
		return nil, false
	}
	trimmed := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(value, "地区"), "省份"), "省")
	switch {
	case strings.Contains(value, "其它") || strings.Contains(value, "其他") || strings.Contains(value, "其余"):
		return nil, true
	}
	provinces := []string{}
	seen := map[string]bool{}
	add := func(province string) {
		if province != "" && !seen[province] {
			seen[province] = true
			provinces = append(provinces, province)
		}
	}
	for alias, expanded := range g.alias {
		if alias != "" && (strings.Contains(value, alias) || value == alias) {
			for _, province := range expanded {
				add(province)
			}
		}
	}
	if len(provinces) > 0 {
		return provinces, false
	}
	if province, ok := g.provinceByKey[trimmed]; ok {
		add(province)
		return provinces, false
	}
	return nil, false
}
