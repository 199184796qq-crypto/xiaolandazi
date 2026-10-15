package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPlanBusinessIndustriesMySQLPersistenceAndScope(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT,name VARCHAR(160),description TEXT,status VARCHAR(24) DEFAULT 'active',biz_domain VARCHAR(32) DEFAULT 'ecommerce')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO live_agent_plans(id,tenant_id,name,description,biz_domain) VALUES(4,14,'方案一','原资料','local_service'),(5,14,'方案二','','ecommerce'),(6,24,'其他租户','','ecommerce')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.migratePlanBusinessIndustries(ctx); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := s.GetPlanBusinessSettings(ctx, 14, 4)
	if err != nil || settings.BizDomain != "local_service" || settings.Industries["ecommerce"] != "general" || settings.Industries["local_service"] != "general" {
		t.Fatal("migration reset selection", settings, err)
	}
	for _, pair := range [][2]string{{"ecommerce", "food"}, {"local_service", "dining"}, {"local_service", "dining"}} {
		settings, err = s.SetPlanBusinessIndustry(ctx, 14, 4, pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
	}
	if settings.BizDomain != "local_service" || settings.Industries["ecommerce"] != "food" || settings.Industries["local_service"] != "dining" {
		t.Fatal("industry changed other selections", settings)
	}
	if _, err = s.SetPlanBizDomain(ctx, 14, 4, "ecommerce"); err != nil {
		t.Fatal(err)
	}
	settings, err = (&Store{db: s.db}).GetPlanBusinessSettings(ctx, 14, 4)
	if err != nil || settings.Industries["ecommerce"] != "food" || settings.Industries["local_service"] != "dining" || settings.BizDomain != "ecommerce" {
		t.Fatal("not persisted across reads/mode switch", settings, err)
	}
	for _, bad := range [][2]string{{"ecommerce", "dining"}, {"local_service", "food"}, {"local", "general"}, {"ecommerce", ""}, {"ecommerce", "anything; DROP TABLE live_agent_plans"}} {
		if _, err = s.SetPlanBusinessIndustry(ctx, 14, 4, bad[0], bad[1]); !errors.Is(err, ErrInvalidPlanBusinessIndustry) {
			t.Fatal("invalid industry accepted", bad, err)
		}
	}
	for _, scope := range [][2]int64{{24, 4}, {14, 6}, {14, 99}} {
		if _, err = s.SetPlanBusinessIndustry(ctx, scope[0], scope[1], "local_service", "hotel"); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal("cross-scope write", scope, err)
		}
		if _, err = s.GetPlanBusinessSettings(ctx, scope[0], scope[1]); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal("cross-scope read", scope, err)
		}
	}
	for _, scope := range [][2]int64{{14, 5}, {24, 6}} {
		got, err := s.GetPlanBusinessSettings(ctx, scope[0], scope[1])
		if err != nil || got.Industries["ecommerce"] != "general" || got.Industries["local_service"] != "general" {
			t.Fatal("other plan changed", got, err)
		}
	}
	var name, description string
	if err = s.db.QueryRowContext(ctx, `SELECT name,description FROM live_agent_plans WHERE id=4`).Scan(&name, &description); err != nil || name != "方案一" || description != "原资料" {
		t.Fatal("other fields changed", name, description, err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE live_agent_plans SET status='archived' WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetPlanBusinessIndustry(ctx, 14, 4, "local_service", "hotel"); !errors.Is(err, ErrLiveAgentPlanNotFound) {
		t.Fatal("archived plan writable", err)
	}
}

func TestPlanBusinessIndustryCatalog(t *testing.T) {
	// 主路径：选项来自目录 json（configs/industries/，文件即源），数量随配置变化。
	for _, domain := range []string{"ecommerce", "local_service"} {
		seen := map[string]bool{}
		options := model.PlanBusinessIndustryOptions(domain)
		if len(options) == 0 {
			t.Fatal("empty catalog", domain)
		}
		for _, option := range options {
			if seen[option.Code] || option.Label == "" || !model.ValidPlanBusinessIndustry(domain, option.Code) {
				t.Fatal("invalid catalog", domain, option)
			}
			seen[option.Code] = true
		}
	}
	// 兜底路径：目录不可用时回退内置枚举，须含 general 默认项。
	t.Setenv("INDUSTRY_CONFIG_DIR", filepath.Join(t.TempDir(), "missing"))
	for _, domain := range []string{"ecommerce", "local_service"} {
		options := model.PlanBusinessIndustryOptions(domain)
		if len(options) != 15 || options[0].Code != "general" {
			t.Fatal("fallback catalog missing default", domain, options)
		}
	}
}
