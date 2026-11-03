package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

func TestTemplateGenericSettingSaveCannotBypassRevision(t *testing.T) {
	s := &Store{}
	for _, key := range planprompt.TemplateKeys {
		if err := s.UpdateSystemSettings(context.Background(), []model.SystemSettingUpdate{{Key: " " + planPromptTemplatePrefix + key + " ", Value: "overwrite"}}, 17); err == nil {
			t.Fatal("generic editor bypassed template guard", key)
		}
	}
}

func TestPlanPromptTemplatesMySQLRoundTripAtomicCAS(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	// The shared sales fixture supplies a minimal settings table; extend only
	// this disposable fixture to the production system-settings schema.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE mgmt_system_settings
		ADD COLUMN setting_group VARCHAR(64) NOT NULL DEFAULT 'general',
		ADD COLUMN label VARCHAR(160) NOT NULL DEFAULT '',
		ADD COLUMN input_type VARCHAR(32) NOT NULL DEFAULT 'text',
		ADD COLUMN sort_order INT NOT NULL DEFAULT 0,
		ADD COLUMN created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		ADD COLUMN updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		ADD KEY idx_mgmt_system_settings_group (setting_group,sort_order,setting_key)`); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateSystemSettings(ctx); err != nil {
		t.Fatal(err)
	}
	defaults := planprompt.Templates{Sections: planprompt.Sections{PersonaMD: "默认人设", FactsMD: "默认事实", RedLinesMD: "默认红线", SpeechRequirementsMD: "默认主线", InteractionRequirementsMD: "默认互动"}, RewritePlanningMD: "默认规划\n{{已有开场}}"}
	got, err := s.GetPlanPromptTemplates(ctx, defaults)
	if err != nil || got != defaults {
		t.Fatal(got, err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_system_settings WHERE setting_group=?`, planPromptTemplateGroup).Scan(&count); err != nil || count != 0 {
		t.Fatal("read seeded template rows", count, err)
	}
	next := defaults
	next.PersonaMD = "  新人设\n😀  "
	next.RedLinesMD = ""
	next.RewritePlanningMD = "  写作规则\n{{已有开场}}\n{{开场方式}}\n  "
	got, err = s.SavePlanPromptTemplates(ctx, defaults, next, defaults.Revision(), 17)
	if err != nil || got != next {
		t.Fatal(got, err)
	}
	stored, err := s.GetPlanPromptTemplates(ctx, defaults)
	if err != nil || stored != next {
		t.Fatal("raw originals not preserved", stored, err)
	}
	settings, err := s.ListSystemSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range settings {
		if setting.Group == planPromptTemplateGroup {
			t.Fatal("dedicated templates leaked into the generic settings editor")
		}
	}
	if _, err = s.SavePlanPromptTemplates(ctx, defaults, defaults, defaults.Revision(), 18); !errors.Is(err, ErrPlanPromptTemplateRevision) {
		t.Fatal("stale edit accepted", err)
	}
	stored, err = s.GetPlanPromptTemplates(ctx, defaults)
	if err != nil || stored != next {
		t.Fatal("stale edit mutated rows", stored, err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_system_settings WHERE setting_group=? AND updated_by_user_id=17`, planPromptTemplateGroup).Scan(&count); err != nil || count != 6 {
		t.Fatal("six rows or administrator not preserved", count, err)
	}
	// Fail after earlier updates have executed: all six originals must roll back.
	_, err = s.db.ExecContext(ctx, `CREATE TRIGGER template_fail_redline BEFORE UPDATE ON mgmt_system_settings FOR EACH ROW BEGIN IF NEW.setting_key='plan_prompt_template_red_lines_md' AND NEW.value_text='force-failure' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture'; END IF; END`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS template_fail_redline`) })
	failing := next
	failing.PersonaMD = "必须回滚"
	failing.FactsMD = "必须回滚的事实"
	failing.RedLinesMD = "force-failure"
	if _, err = s.SavePlanPromptTemplates(ctx, defaults, failing, next.Revision(), 19); err == nil {
		t.Fatal("trigger fixture did not fail")
	}
	stored, err = s.GetPlanPromptTemplates(ctx, defaults)
	if err != nil || stored != next {
		t.Fatal("partial save escaped transaction", stored, err)
	}
	if _, err = s.db.ExecContext(ctx, `DROP TRIGGER template_fail_redline`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM mgmt_system_settings WHERE setting_group=?`, planPromptTemplateGroup); err != nil {
		t.Fatal(err)
	}
	// Simultaneous first saves must still compare one shared original revision.
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			v := defaults
			v.PersonaMD = fmt.Sprintf("并发母版%d", i)
			_, err := s.SavePlanPromptTemplates(ctx, defaults, v, defaults.Revision(), int64(30+i))
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
		} else if !errors.Is(err, ErrPlanPromptTemplateRevision) {
			t.Fatal("unexpected concurrent error", err)
		}
	}
	if wins != 1 {
		t.Fatal("simultaneous first-save lost update", wins)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_system_settings WHERE setting_group=?`, planPromptTemplateGroup).Scan(&count); err != nil || count != 6 {
		t.Fatal("incomplete first save", count, err)
	}
}
