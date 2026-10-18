package httpapi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"livecompanion/management/internal/model"
)

// 行业配置包采用「文件即源」：configs/industries/ 下的目录 json + 每个行业四件套，
// 运行时直接读文件。改 md/json 后随部署生效（复用现有母版 MANIFEST + scp 机制），
// 不改 Go 代码。目录加载与树解析在 model 包（IndustryCatalog），本文件只做四件套装载。

// IndustryPack 是「一个行业解析后的完整四件套」，供卖点挖掘/侧面筛事实/事实分类使用。
type IndustryPack struct {
	BizDomain         string
	IndustryCode      string
	IndustryName      string
	SellingDimensions string                    // 挖掘维度 md 内容
	Sides             model.IndustrySidesConfig // 侧面池 + 分类→侧面映射
	FactCategories    []model.LiveFactCategory  // 事实分类枚举
	P0Extra           string                    // 行业 P0 补充 md 内容
}

// loadIndustryPack 按业务域 + 行业 code 加载完整四件套（含树继承）。
// 找不到行业或四件套文件不完整时报错。
func loadIndustryPack(bizDomain, industryCode string) (*IndustryPack, error) {
	catalog, err := model.LoadIndustryCatalog(bizDomain)
	if err != nil {
		return nil, err
	}
	name, paths, ok := catalog.Resolve(industryCode)
	if !ok {
		return nil, errors.New("行业不存在: " + bizDomain + "/" + industryCode)
	}
	dir := model.IndustryConfigDir()
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

// packFactFamilyMap 把行业事实分类枚举转成 分类 value → family 映射，
// 供 State.FactFamilyMap 使用：直播时据此区分公共资料是「品牌背书」（只进公共段）
// 还是「交易/履约条件」（商品段按需引用）。
func packFactFamilyMap(categories []model.LiveFactCategory) map[string]string {
	out := make(map[string]string, len(categories))
	for _, c := range categories {
		value := strings.ToLower(strings.TrimSpace(c.Value))
		family := strings.ToLower(strings.TrimSpace(c.Family))
		if value != "" && family != "" {
			out[value] = family
		}
	}
	return out
}
