package httpapi

import (
	"os"
	"path/filepath"
	"testing"

	"livecompanion/management/internal/model"
)

func TestResolveIndustryNodeInheritance(t *testing.T) {
	catalog := industryCatalog{
		SchemaVersion: 2,
		Defaults: industryPackPaths{
			SellingDimensions: "common/selling-dimensions.md",
			Sides:             "common/sides.json",
		},
		Nodes: []industryCatalogNode{
			{
				Code: "food",
				Name: "食品",
				industryPackPaths: industryPackPaths{
					SellingDimensions: "food/selling-dimensions.md",
					Sides:             "food/sides.json",
				},
				Children: []industryCatalogNode{
					{
						Code: "snack",
						Name: "休闲零食",
						industryPackPaths: industryPackPaths{
							FactCategories: "food/snack/fact-categories.json",
						},
					},
				},
			},
		},
	}
	name, paths, ok := resolveIndustryNode(catalog, "snack")
	if !ok {
		t.Fatal("snack 应能解析到")
	}
	if name != "休闲零食" {
		t.Fatalf("name 错误: %s", name)
	}
	if paths.SellingDimensions != "food/selling-dimensions.md" {
		t.Fatalf("selling_dimensions 未继承父级: %s", paths.SellingDimensions)
	}
	if paths.Sides != "food/sides.json" {
		t.Fatalf("sides 未继承父级: %s", paths.Sides)
	}
	if paths.FactCategories != "food/snack/fact-categories.json" {
		t.Fatalf("fact_categories 未用子级覆盖: %s", paths.FactCategories)
	}
	if paths.P0Extra != "" {
		t.Fatalf("p0_extra 应继承 defaults 空值: %s", paths.P0Extra)
	}
}

func TestPackFissionSidesAndMap(t *testing.T) {
	raw := `{
		"schema_version": 2,
		"sides": [
			{"code": "scenario", "name": "到店场景", "description": "约会聚餐"},
			{"code": "location", "name": "地理位置", "description": "好停车"}
		],
		"category_sides": {"location": ["location"]}
	}`
	sides, err := model.ParseIndustrySides(raw)
	if err != nil {
		t.Fatal(err)
	}
	pool := packFissionSides(sides)
	if len(pool) != 2 || pool[0] != "到店场景：约会聚餐" {
		t.Fatalf("侧面池格式错误: %v", pool)
	}
	mapping := packFactSideMap(sides)
	if got := mapping["location"]; len(got) != 1 || got[0] != "地理位置" {
		t.Fatalf("分类→侧面名转换错误: %v", mapping["location"])
	}
}

func TestLoadIndustryPackRestaurant(t *testing.T) {
	dir := findIndustriesDir(t)
	t.Setenv("INDUSTRY_CONFIG_DIR", dir)
	pack, err := loadIndustryPack("local_service", "restaurant")
	if err != nil {
		t.Fatal(err)
	}
	if pack.IndustryName != "餐饮（到店）" {
		t.Fatalf("行业名错误: %s", pack.IndustryName)
	}
	if len(pack.Sides.Sides) != 9 {
		t.Fatalf("餐饮侧面应为 9 个，got %d", len(pack.Sides.Sides))
	}
	if len(pack.FactCategories) != 17 {
		t.Fatalf("餐饮事实分类应为 17 个，got %d", len(pack.FactCategories))
	}
	if pack.SellingDimensions == "" || pack.P0Extra == "" {
		t.Fatal("挖掘维度或 P0 补充为空")
	}
}

func TestLoadIndustryPackFood(t *testing.T) {
	dir := findIndustriesDir(t)
	t.Setenv("INDUSTRY_CONFIG_DIR", dir)
	pack, err := loadIndustryPack("ecommerce", "food")
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Sides.Sides) != 7 {
		t.Fatalf("食品侧面应为 7 个，got %d", len(pack.Sides.Sides))
	}
	if len(pack.FactCategories) != 42 {
		t.Fatalf("食品事实分类应为 42 个，got %d", len(pack.FactCategories))
	}
}

func findIndustriesDir(t *testing.T) string {
	t.Helper()
	cwd, _ := os.Getwd()
	current := cwd
	for range 8 {
		p := filepath.Join(current, "configs", "industries")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	t.Fatal("找不到 configs/industries 目录")
	return ""
}
