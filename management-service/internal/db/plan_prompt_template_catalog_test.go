package db

import (
	"context"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
	"reflect"
	"sync"
	"testing"
)

func TestCatalogGenericSettingSaveCannotBypassRevision(t *testing.T) {
	s := &Store{}
	for _, key := range planprompt.TemplateKeys {
		if s.UpdateSystemSettings(context.Background(), []model.SystemSettingUpdate{{Key: " " + planPromptCatalogPrefix + key + " ", Value: "[]"}}, 17) == nil {
			t.Fatal("generic bypass")
		}
	}
}
func TestPlanPromptCatalogMySQLRoundTripAtomicCAS(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE mgmt_system_settings ADD COLUMN setting_group VARCHAR(64) NOT NULL DEFAULT 'general', ADD COLUMN label VARCHAR(160) NOT NULL DEFAULT '', ADD COLUMN input_type VARCHAR(32) NOT NULL DEFAULT 'text', ADD COLUMN sort_order INT NOT NULL DEFAULT 0, ADD COLUMN created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), ADD COLUMN updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3), ADD KEY idx_mgmt_system_settings_group (setting_group,sort_order,setting_key)`); err != nil {
		t.Fatal(err)
	}
	empty := planprompt.EmptyTemplateCatalog()
	got, err := s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, empty) {
		t.Fatal(got, err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_system_settings WHERE setting_group=?`, planPromptCatalogGroup).Scan(&count); err != nil || count != 0 {
		t.Fatal("read seeded rows", count, err)
	}
	next := planprompt.EmptyTemplateCatalog()
	for _, key := range planprompt.TemplateKeys {
		next.Pages[key] = []planprompt.PromptTemplate{{ID: key + "-a", Name: "第一份", Content: "  【节点】\n名字：小林😀\n{{直播间名}}  "}, {ID: key + "-b", Name: "空模板", Content: ""}}
	}
	if _, err = s.SavePlanPromptTemplateCatalog(ctx, next, empty.Revision(), 17); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, next) {
		t.Fatal("raw originals", got, err)
	}
	if _, err = s.SavePlanPromptTemplateCatalog(ctx, empty, empty.Revision(), 18); !errors.Is(err, ErrPlanPromptCatalogRevision) {
		t.Fatal("stale accepted", err)
	}
	settings, err := s.ListSystemSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range settings {
		if item.Group == planPromptCatalogGroup {
			t.Fatal("generic leak")
		}
	}
	if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER catalog_fail BEFORE UPDATE ON mgmt_system_settings FOR EACH ROW BEGIN IF NEW.setting_key='plan_prompt_catalog_red_lines_md' AND NEW.value_text='[]' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePlanPromptTemplateCatalog(ctx, empty, next.Revision(), 19); err == nil {
		t.Fatal("trigger did not fail")
	}
	got, err = s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, next) {
		t.Fatal("partial save", err)
	}
	if _, err = s.db.ExecContext(ctx, `DROP TRIGGER catalog_fail`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePlanPromptTemplateCatalog(ctx, empty, next.Revision(), 20); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, empty) {
		t.Fatal("deleted templates resurrected", err)
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM mgmt_system_settings WHERE setting_group=?`, planPromptCatalogGroup); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			c := planprompt.EmptyTemplateCatalog()
			c.Pages["persona_md"] = []planprompt.PromptTemplate{{ID: fmt.Sprint("winner-", i), Name: "并发模板"}}
			_, err := s.SavePlanPromptTemplateCatalog(ctx, c, empty.Revision(), int64(30+i))
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrPlanPromptCatalogRevision) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("lost update", wins)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text='null' WHERE setting_key=?`, planPromptCatalogPrefix+"persona_md"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetPlanPromptTemplateCatalog(ctx); err == nil {
		t.Fatal("corrupt catalog silently resurrected")
	}
}

func TestPlanPromptCatalogMySQLSavedLegacyCompatibility(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE mgmt_system_settings ADD COLUMN setting_group VARCHAR(64) NOT NULL DEFAULT 'general', ADD COLUMN label VARCHAR(160) NOT NULL DEFAULT '', ADD COLUMN input_type VARCHAR(32) NOT NULL DEFAULT 'text', ADD COLUMN sort_order INT NOT NULL DEFAULT 0, ADD COLUMN created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), ADD COLUMN updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3), ADD KEY idx_mgmt_system_settings_group (setting_group,sort_order,setting_key)`); err != nil {
		t.Fatal(err)
	}
	want := planprompt.EmptyTemplateCatalog()
	for _, page := range planprompt.TemplateKeys {
		raw := "  【" + page + "】\n内容：{{主播身份}} 😀\n"
		if page == "facts_md" {
			raw = "" // An explicitly saved empty template remains selectable.
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO mgmt_system_settings(setting_key,setting_group,value_text) VALUES(?,?,?)`, planPromptTemplatePrefix+page, planPromptTemplateGroup, raw); err != nil {
			t.Fatal(err)
		}
		want.Pages[page] = []planprompt.PromptTemplate{{ID: "legacy_" + page, Name: "默认模板", Content: raw}}
	}
	got, err := s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("saved legacy originals were not selectable", got, err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_system_settings WHERE setting_group=?`, planPromptCatalogGroup).Scan(&count); err != nil || count != 0 {
		t.Fatal("compatibility GET wrote catalog rows", count, err)
	}
	// A change through the old maintenance page invalidates a stale first save.
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text='new legacy' WHERE setting_key=?`, planPromptTemplatePrefix+"persona_md"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePlanPromptTemplateCatalog(ctx, want, want.Revision(), 17); !errors.Is(err, ErrPlanPromptCatalogRevision) {
		t.Fatal("stale legacy revision accepted", err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_system_settings WHERE setting_group=?`, planPromptCatalogGroup).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected first save left rows behind", count, err)
	}
	want.Pages["persona_md"][0].Content = "new legacy"
	got, err = s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	// Persist a second named template while explicitly deleting another page.
	next := planprompt.EmptyTemplateCatalog()
	for page, items := range want.Pages {
		next.Pages[page] = append([]planprompt.PromptTemplate{}, items...)
	}
	next.Pages["persona_md"] = append(next.Pages["persona_md"], planprompt.PromptTemplate{ID: "second", Name: "第二份", Content: "自定义正文"})
	next.Pages["facts_md"] = []planprompt.PromptTemplate{}
	if _, err = s.SavePlanPromptTemplateCatalog(ctx, next, want.Revision(), 18); err != nil {
		t.Fatal("could not save legacy-backed draft", err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text='later legacy' WHERE setting_group=?`, planPromptTemplateGroup); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, next) {
		t.Fatal("legacy overwrote catalog or resurrected a deleted page", got, err)
	}
	if _, err = s.SavePlanPromptTemplateCatalog(ctx, planprompt.EmptyTemplateCatalog(), next.Revision(), 19); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetPlanPromptTemplateCatalog(ctx)
	if err != nil || !reflect.DeepEqual(got, planprompt.EmptyTemplateCatalog()) {
		t.Fatal("explicit empty catalog resurrected legacy", got, err)
	}
	var preserved string
	if err = s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, planPromptTemplatePrefix+"persona_md").Scan(&preserved); err != nil || preserved != "later legacy" {
		t.Fatal("catalog save changed legacy storage", preserved, err)
	}
}
