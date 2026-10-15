package model

import "testing"

func TestParseIndustrySidesAcceptsObjectSides(t *testing.T) {
	raw := `{
		"schema_version": 2,
		"sides": [
			{"code": "scenario", "name": "到店场景", "name_en": "Dine-in Scenario", "description": "什么场合去"},
			{"code": "value", "name": "利益算账", "name_en": "Value", "description": "省多少"}
		],
		"category_sides": {"price": ["value", "scenario"]}
	}`
	cfg, err := ParseIndustrySides(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sides) != 2 || cfg.Sides[0].Code != "scenario" || cfg.Sides[0].NameEN != "Dine-in Scenario" {
		t.Fatalf("sides 解析错误: %+v", cfg.Sides)
	}
	if got := cfg.CategorySides["price"]; len(got) != 2 || got[0] != "value" {
		t.Fatalf("category_sides 解析错误: %v", got)
	}
}

func TestParseIndustryFactCategories(t *testing.T) {
	raw := `{
		"schema_version": 1,
		"categories": [
			{"value": "price", "label": "团购价", "label_en": "Price", "group": "价格权益", "group_en": "Price & Rights", "family": "trade", "description": "价格"},
			{"value": "location", "label": "门店位置", "label_en": "Location", "group": "门店信息", "group_en": "Store Info", "family": "identity_location", "description": "地址"}
		]
	}`
	cats, err := ParseIndustryFactCategories(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0].Value != "price" || cats[0].LabelEN != "Price" {
		t.Fatalf("fact_categories 解析错误: %+v", cats)
	}
}

func TestParseIndustrySidesRejectsBadSideRef(t *testing.T) {
	// category_sides 引用了不存在的 side code，应报错。
	raw := `{
		"schema_version": 2,
		"sides": [{"code": "value", "name": "利益算账", "description": "省多少"}],
		"category_sides": {"price": ["nonexistent"]}
	}`
	if _, err := ParseIndustrySides(raw); err == nil {
		t.Fatal("引用不存在的侧面 code 应报错")
	}
}

func TestParseIndustrySidesRejectsSchemaV1(t *testing.T) {
	raw := `{"schema_version": 1, "sides": [{"code":"value","name":"利益算账","description":"x"}]}`
	if _, err := ParseIndustrySides(raw); err == nil {
		t.Fatal("schema_version 1 应报错（已升级到 2）")
	}
}
