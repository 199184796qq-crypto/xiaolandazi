package model

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// 行业目录（文件即源）：configs/industries/{biz_domain}.json 维护行业树，
// 每个节点可配四件套（挖掘md/侧面json/事实分类json/p0补充md），子级空则继承父级。
// 本文件提供目录加载 + 树解析 + 行业选项提取，供行业下拉框与四件套加载共用。

// IndustryPackPaths 是目录 json 里一个节点的四件套相对路径（相对 configs/industries/）。
type IndustryPackPaths struct {
	SellingDimensions string `json:"selling_dimensions"`
	Sides             string `json:"sides"`
	FactCategories    string `json:"fact_categories"`
	P0Extra           string `json:"p0_extra"`
}

// IndustryCatalogNode 是行业树节点。四件套路径平铺在节点顶层，children 递归。
type IndustryCatalogNode struct {
	Code     string                `json:"code"`
	Name     string                `json:"name"`
	NameEN   string                `json:"name_en,omitempty"`
	IndustryPackPaths
	Children []IndustryCatalogNode `json:"children"`
}

// IndustryCatalog 是每个业务域的目录 json（ecommerce.json / local-service.json）。
type IndustryCatalog struct {
	SchemaVersion int                   `json:"schema_version"`
	BizDomain     string                `json:"biz_domain"`
	Name          string                `json:"name"`
	NameEN        string                `json:"name_en,omitempty"`
	Defaults      IndustryPackPaths     `json:"defaults"`
	Nodes         []IndustryCatalogNode `json:"nodes"`
}

// IndustryConfigDir 返回 configs/industries/ 的绝对路径（文件即源）。
func IndustryConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("INDUSTRY_CONFIG_DIR")); dir != "" {
		return dir
	}
	// ANCHOR_STYLE_PROMPT_DIR 指向 configs/prompts/anchor-style，往上两级即 configs。
	if dir := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_PROMPT_DIR")); dir != "" {
		return filepath.Join(dir, "..", "..", "industries")
	}
	if cwd, err := os.Getwd(); err == nil {
		current := cwd
		for range 7 {
			p := filepath.Join(current, "configs", "industries")
			if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
				return p
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	if exe, err := os.Executable(); err == nil {
		for _, p := range []string{
			filepath.Join(filepath.Dir(exe), "..", "configs", "industries"),
			filepath.Join(filepath.Dir(exe), "configs", "industries"),
		} {
			if info, statErr := os.Stat(p); statErr == nil && info.IsDir() {
				return p
			}
		}
	}
	return ""
}

// LoadIndustryCatalog 读某业务域的目录 json。
func LoadIndustryCatalog(bizDomain string) (IndustryCatalog, error) {
	var catalog IndustryCatalog
	dir := IndustryConfigDir()
	if dir == "" {
		return catalog, errors.New("找不到行业配置目录 configs/industries")
	}
	name := bizDomain + ".json"
	if bizDomain == "local_service" {
		name = "local-service.json"
	}
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return catalog, errors.New("读取行业目录失败: " + err.Error())
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return catalog, errors.New("行业目录 JSON 格式错误: " + err.Error())
	}
	if catalog.SchemaVersion != 2 {
		return catalog, errors.New("行业目录 schema_version 必须为 2")
	}
	return catalog, nil
}

// MergeIndustryPackPaths 用 override 的非空项覆盖 base，实现子级继承父级。
func MergeIndustryPackPaths(base, override IndustryPackPaths) IndustryPackPaths {
	if override.SellingDimensions != "" {
		base.SellingDimensions = override.SellingDimensions
	}
	if override.Sides != "" {
		base.Sides = override.Sides
	}
	if override.FactCategories != "" {
		base.FactCategories = override.FactCategories
	}
	if override.P0Extra != "" {
		base.P0Extra = override.P0Extra
	}
	return base
}

// Resolve 在行业树中递归查找 code，返回节点名 + 继承合并后的四件套路径。
func (c IndustryCatalog) Resolve(code string) (string, IndustryPackPaths, bool) {
	code = strings.TrimSpace(code)
	var foundPaths *IndustryPackPaths
	var foundName string
	var visit func(node IndustryCatalogNode, inherited IndustryPackPaths)
	visit = func(node IndustryCatalogNode, inherited IndustryPackPaths) {
		if foundPaths != nil {
			return
		}
		merged := MergeIndustryPackPaths(inherited, node.IndustryPackPaths)
		if node.Code == code {
			p := merged
			foundPaths = &p
			foundName = node.Name
			return
		}
		for _, child := range node.Children {
			visit(child, merged)
			if foundPaths != nil {
				return
			}
		}
	}
	for _, node := range c.Nodes {
		visit(node, c.Defaults)
		if foundPaths != nil {
			break
		}
	}
	if foundPaths == nil {
		return "", IndustryPackPaths{}, false
	}
	return foundName, *foundPaths, true
}

// CatalogIndustryOptions 从目录 json 提取某业务域的行业选项（下拉框数据源）。
// 树形展开全部节点（code/name/name_en）。目录缺失或解析失败返回 nil，调用方回退兜底枚举。
func CatalogIndustryOptions(bizDomain string) []PlanBusinessIndustry {
	catalog, err := LoadIndustryCatalog(bizDomain)
	if err != nil {
		return nil
	}
	var out []PlanBusinessIndustry
	var visit func(n IndustryCatalogNode)
	visit = func(n IndustryCatalogNode) {
		out = append(out, PlanBusinessIndustry{Code: n.Code, Label: n.Name})
		for _, c := range n.Children {
			visit(c)
		}
	}
	for _, n := range catalog.Nodes {
		visit(n)
	}
	return out
}
