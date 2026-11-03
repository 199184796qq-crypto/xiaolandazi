package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"livecompanion/management/internal/planprompt"
)

const planPromptTemplateGroup = "plan_prompt_templates"
const planPromptTemplatePrefix = "plan_prompt_template_"

var ErrPlanPromptTemplateRevision = errors.New("提示词母版已被其他人修改，请重新读取后再保存")

func (s *Store) GetPlanPromptTemplates(ctx context.Context, defaults planprompt.Templates) (planprompt.Templates, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key,value_text FROM mgmt_system_settings WHERE setting_group=? ORDER BY setting_key`, planPromptTemplateGroup)
	if err != nil {
		return planprompt.Templates{}, err
	}
	defer rows.Close()
	return readPlanPromptTemplates(rows, defaults, false)
}

func readPlanPromptTemplates(rows *sql.Rows, defaults planprompt.Templates, requireComplete bool) (planprompt.Templates, error) {
	overrides := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return planprompt.Templates{}, err
		}
		for _, name := range planprompt.TemplateKeys {
			if key == planPromptTemplatePrefix+name {
				overrides[name] = value
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return planprompt.Templates{}, err
	}
	if requireComplete && len(overrides) != len(planprompt.TemplateKeys) {
		return planprompt.Templates{}, errors.New("提示词母版记录不完整")
	}
	return planprompt.TemplatesWithOverrides(defaults, overrides), nil
}

func (s *Store) SavePlanPromptTemplates(ctx context.Context, defaults, next planprompt.Templates, expectedRevision string, actorID int64) (planprompt.Templates, error) {
	if err := next.Validate(); err != nil {
		return planprompt.Templates{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return planprompt.Templates{}, err
	}
	defer tx.Rollback()
	// The same deterministic key order serializes first saves as well as updates.
	values := defaults.Values()
	for i, name := range planprompt.TemplateKeys {
		_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_system_settings(setting_key,setting_group,label,value_text,input_type,sort_order,updated_by_user_id) VALUES(?,?,?,?, 'textarea',?,?) ON DUPLICATE KEY UPDATE setting_key=VALUES(setting_key)`, planPromptTemplatePrefix+name, planPromptTemplateGroup, "方案提示词母版 "+name, values[name], i, actorID)
		if err != nil {
			return planprompt.Templates{}, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT setting_key,value_text FROM mgmt_system_settings WHERE setting_group=? ORDER BY setting_key FOR UPDATE`, planPromptTemplateGroup)
	if err != nil {
		return planprompt.Templates{}, err
	}
	current, err := readPlanPromptTemplates(rows, defaults, true)
	closeErr := rows.Close()
	if err != nil {
		return planprompt.Templates{}, err
	}
	if closeErr != nil {
		return planprompt.Templates{}, closeErr
	}
	if expectedRevision == "" || current.Revision() != expectedRevision {
		return planprompt.Templates{}, ErrPlanPromptTemplateRevision
	}
	for _, name := range planprompt.TemplateKeys {
		result, err := tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=? WHERE setting_key=? AND setting_group=?`, next.Values()[name], actorID, planPromptTemplatePrefix+name, planPromptTemplateGroup)
		if err != nil {
			return planprompt.Templates{}, err
		}
		if count, err := result.RowsAffected(); err != nil || count > 1 {
			return planprompt.Templates{}, fmt.Errorf("unexpected template update result")
		}
	}
	if err := tx.Commit(); err != nil {
		return planprompt.Templates{}, err
	}
	return next, nil
}
