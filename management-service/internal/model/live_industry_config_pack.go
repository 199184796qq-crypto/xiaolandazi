package model

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// IndustryConfigPack 是「一个行业 = 一套配置包」的落库模型。
//
// 每个行业包包含两类资产，各司其职：
//   - SellingDimensionsMD：卖点挖掘维度（Markdown，给模型当 prompt 注入）
//   - SidesJSON：侧面池 + 事实分类→侧面映射（JSON，给程序做「侧面筛事实」）
//   - P0ExtraMD：行业专属 P0 补充（可选，给模型，团购类目的高危承诺清单）
//
// 业务域 biz_domain 取值 ecommerce（电商）/ local_service（本地生活/团购）。
// 一个方案选定业务域 + 行业后，程序用对应配置包来挖掘卖点×角度、做事实分类，
// 与电商流程一致；改行业配置只需改对应记录，不用动 Go 代码。
type IndustryConfigPack struct {
	ID                  int64     `json:"id"`
	BizDomain           string    `json:"biz_domain"`
	IndustryCode        string    `json:"industry_code"`
	IndustryName        string    `json:"industry_name"`
	SellingDimensionsMD string    `json:"selling_dimensions_md"`
	SidesJSON           string    `json:"sides_json"`
	P0ExtraMD           string    `json:"p0_extra_md,omitempty"`
	Version             string    `json:"version"`
	Status              string    `json:"status"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// IndustrySide 是侧面池中的单个侧面。Code 英文内部（程序用），Name/NameEN 中英文显示，
// Description 描述（给模型/提示）。
type IndustrySide struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	NameEN      string `json:"name_en,omitempty"`
	Description string `json:"description"`
}

// IndustrySidesConfig 是 sides.json 的结构化视图。
// Sides 是侧面池（英文 code + 中英文名）；CategorySides 是事实分类 value → 侧面 code
// 的映射，替代原先写死在 Go 里的 factCategorySides。
type IndustrySidesConfig struct {
	SchemaVersion int                 `json:"schema_version"`
	Sides         []IndustrySide      `json:"sides"`
	CategorySides map[string][]string `json:"category_sides"`
}

// IndustryFactCategoriesConfig 是 fact-categories.json 的结构化视图，
// 替代全局 fact-categories.json，作为行业的事实分类枚举（表单 + 校验用）。
type IndustryFactCategoriesConfig struct {
	SchemaVersion int                `json:"schema_version"`
	Categories    []LiveFactCategory `json:"categories"`
}

// ParseIndustryFactCategories 解析并校验行业事实分类 JSON。
func ParseIndustryFactCategories(raw string) ([]LiveFactCategory, error) {
	var cfg IndustryFactCategoriesConfig
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("事实分类配置不能为空")
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, errors.New("事实分类 JSON 格式错误: " + err.Error())
	}
	if cfg.SchemaVersion != 1 {
		return nil, errors.New("事实分类 schema_version 必须为 1")
	}
	if len(cfg.Categories) < 2 || len(cfg.Categories) > 200 {
		return nil, errors.New("事实分类须为 2~200 个")
	}
	seen := map[string]bool{}
	for _, c := range cfg.Categories {
		if strings.TrimSpace(c.Value) == "" || seen[c.Value] || strings.TrimSpace(c.Label) == "" {
			return nil, errors.New("事实分类含无效或重复条目: " + c.Value)
		}
		seen[c.Value] = true
	}
	return cfg.Categories, nil
}

// ParseIndustrySides 解析并校验侧面配置 JSON。
func ParseIndustrySides(raw string) (IndustrySidesConfig, error) {
	var cfg IndustrySidesConfig
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cfg, errors.New("侧面配置不能为空")
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, errors.New("侧面配置 JSON 格式错误: " + err.Error())
	}
	if cfg.SchemaVersion != 2 {
		return cfg, errors.New("侧面配置 schema_version 必须为 2")
	}
	if len(cfg.Sides) < 2 || len(cfg.Sides) > 20 {
		return cfg, errors.New("侧面池须为 2~20 个侧面")
	}
	seen := map[string]bool{}
	codeRe := regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	for _, side := range cfg.Sides {
		code := strings.TrimSpace(side.Code)
		name := strings.TrimSpace(side.Name)
		if !codeRe.MatchString(code) || seen[code] {
			return cfg, errors.New("侧面池含无效或重复 code: " + code)
		}
		if name == "" || utf8.RuneCountInString(name) > 12 {
			return cfg, errors.New("侧面池含无效名称: " + name)
		}
		seen[code] = true
	}
	for cat, sides := range cfg.CategorySides {
		if strings.TrimSpace(cat) == "" {
			return cfg, errors.New("分类映射含空分类 key")
		}
		for _, s := range sides {
			if !seen[strings.TrimSpace(s)] {
				return cfg, errors.New("分类 " + cat + " 引用了侧面池中不存在的侧面 code: " + s)
			}
		}
	}
	return cfg, nil
}
