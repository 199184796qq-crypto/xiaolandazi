package httpapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"livecompanion/management/internal/model"
)

// 行业配置包采用「文件即源」：configs/industries/ 下的目录 json + 每个行业四件套，
// 运行时直接读文件。改 md/json 后随部署生效（复用现有母版 MANIFEST + scp 机制），
// 不改 Go 代码。

// industryPackPaths 是目录 json 里一个节点的四件套相对路径（相对 configs/industries/）。
type industryPackPaths struct {
	SellingDimensions string `json:"selling_dimensions"`
	Sides             string `json:"sides"`
	FactCategories    string `json:"fact_categories"`
	P0Extra           string `json:"p0_extra"`
}

// industryCatalogNode 是行业树节点。四件套路径平铺在节点顶层，children 递归。
type industryCatalogNode struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	NameEN   string `json:"name_en,omitempty"`
	industryPackPaths
	Children []industryCatalogNode `json:"children"`
}

// industryCatalog 是每个业务域的目录 json（ecommerce.json / local-service.json）。
type industryCatalog struct {
	SchemaVersion int               `json:"schema_version"`
	BizDomain     string            `json:"biz_domain"`
	Name          string            `json:"name"`
	NameEN        string            `json:"name_en,omitempty"`
	Defaults      industryPackPaths `json:"defaults"`
	Nodes         []industryCatalogNode `json:"nodes"`
}

// IndustryPack 是「一个行业解析后的完整四件套」，供卖点挖掘/侧面筛事实/事实分类使用。
type IndustryPack struct {
	BizDomain         string
	IndustryCode      string
	IndustryName      string
	SellingDimensions string                      // 挖掘维度 md 内容
	Sides             model.IndustrySidesConfig   // 侧面池 + 分类→侧面映射
	FactCategories    []model.LiveFactCategory    // 事实分类枚举
	P0Extra           string                      // 行业 P0 补充 md 内容
}

// industryConfigDir 返回 configs/industries/ 的绝对路径（文件即源）。
func industryConfigDir() string {
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

// loadIndustryCatalog 读某业务域的目录 json。
func loadIndustryCatalog(bizDomain string) (industryCatalog, error) {
	var catalog industryCatalog
	dir := industryConfigDir()
	if dir == "" {
		return catalog, errors.New("找不到行业配置目录 configs/industries")
	}
	name := bizDomain + ".json"
	if bizDomain == "local_service" {
		name = "local-service.json"
	}
	path := filepath.Join(dir, name)
	raw, err := os.ReadFile(path)
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

// mergePackPaths 用 override 的非空项覆盖 base，实现子级继承父级。
func mergePackPaths(base, override industryPackPaths) industryPackPaths {
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

// resolveIndustryNode 在行业树中递归查找 code，返回节点名 + 继承合并后的四件套路径。
func resolveIndustryNode(catalog industryCatalog, code string) (string, industryPackPaths, bool) {
	code = strings.TrimSpace(code)
	var foundPaths *industryPackPaths
	var foundName string
	var visit func(node industryCatalogNode, inherited industryPackPaths)
	visit = func(node industryCatalogNode, inherited industryPackPaths) {
		if foundPaths != nil {
			return
		}
		merged := mergePackPaths(inherited, node.industryPackPaths)
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
	for _, node := range catalog.Nodes {
		visit(node, catalog.Defaults)
		if foundPaths != nil {
			break
		}
	}
	if foundPaths == nil {
		return "", industryPackPaths{}, false
	}
	return foundName, *foundPaths, true
}

// loadIndustryPack 按业务域 + 行业 code 加载完整四件套（含树继承）。
// 找不到行业或四件套文件不完整时报错。
func loadIndustryPack(bizDomain, industryCode string) (*IndustryPack, error) {
	catalog, err := loadIndustryCatalog(bizDomain)
	if err != nil {
		return nil, err
	}
	name, paths, ok := resolveIndustryNode(catalog, industryCode)
	if !ok {
		return nil, errors.New("行业不存在: " + bizDomain + "/" + industryCode)
	}
	dir := industryConfigDir()
	read := func(rel string) (string, error) {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			return "", nil
		}
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", errors.New("读取行业文件失败 " + rel + ": " + err.Error())
		}
		return strings.TrimSpace(string(raw)), nil
	}
	pack := &IndustryPack{
		BizDomain:    bizDomain,
		IndustryCode: industryCode,
		IndustryName: name,
	}
	if pack.SellingDimensions, err = read(paths.SellingDimensions); err != nil {
		return nil, err
	}
	if pack.P0Extra, err = read(paths.P0Extra); err != nil {
		return nil, err
	}
	sidesRaw, err := read(paths.Sides)
	if err != nil {
		return nil, err
	}
	if sidesRaw != "" {
		if pack.Sides, err = model.ParseIndustrySides(sidesRaw); err != nil {
			return nil, err
		}
	}
	factsRaw, err := read(paths.FactCategories)
	if err != nil {
		return nil, err
	}
	if factsRaw != "" {
		if pack.FactCategories, err = model.ParseIndustryFactCategories(factsRaw); err != nil {
			return nil, err
		}
	}
	if pack.SellingDimensions == "" {
		return nil, errors.New("行业缺少挖掘维度: " + bizDomain + "/" + industryCode)
	}
	return pack, nil
}

// packFissionSides 把行业侧面池转成「短名：描述」格式，供 State.FissionSides 使用
// （与现有 fissionAngleSides 格式一致，向下兼容）。
func packFissionSides(sides model.IndustrySidesConfig) []string {
	out := make([]string, 0, len(sides.Sides))
	for _, s := range sides.Sides {
		desc := strings.TrimSpace(s.Description)
		if desc == "" {
			out = append(out, s.Name)
		} else {
			out = append(out, s.Name+"："+desc)
		}
	}
	return out
}

// packFactSideMap 把行业分类→侧面 code 映射，转成 分类→侧面短名（name），
// 供 State.FactSideMap 使用（与现有 factCategorySides 用中文短名对齐，向下兼容）。
func packFactSideMap(sides model.IndustrySidesConfig) map[string][]string {
	codeToName := make(map[string]string, len(sides.Sides))
	for _, s := range sides.Sides {
		codeToName[s.Code] = s.Name
	}
	out := make(map[string][]string, len(sides.CategorySides))
	for cat, codes := range sides.CategorySides {
		names := make([]string, 0, len(codes))
		for _, c := range codes {
			if n, ok := codeToName[c]; ok {
				names = append(names, n)
			}
		}
		out[cat] = names
	}
	return out
}
