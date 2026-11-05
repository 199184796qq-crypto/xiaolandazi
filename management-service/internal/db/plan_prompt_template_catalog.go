package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"livecompanion/management/internal/planprompt"
)

const planPromptCatalogGroup = "plan_prompt_template_catalog"
const planPromptCatalogPrefix = "plan_prompt_catalog_"

var ErrPlanPromptCatalogRevision = errors.New("模板目录已被其他人修改，请重新读取后再保存")

func (s *Store) GetPlanPromptTemplateCatalog(ctx context.Context) (planprompt.TemplateCatalog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT setting_key,value_text FROM mgmt_system_settings WHERE setting_group=? ORDER BY setting_key`, planPromptCatalogGroup)
	if err != nil {
		return planprompt.TemplateCatalog{}, err
	}
	defer rows.Close()
	return readPlanPromptCatalog(rows, false)
}
func readPlanPromptCatalog(rows *sql.Rows, complete bool) (planprompt.TemplateCatalog, error) {
	c := planprompt.EmptyTemplateCatalog()
	count := 0
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return c, err
		}
		for _, page := range planprompt.TemplateKeys {
			if key != planPromptCatalogPrefix+page {
				continue
			}
			var items []planprompt.PromptTemplate
			if err := json.Unmarshal([]byte(raw), &items); err != nil {
				return c, fmt.Errorf("模板目录记录损坏: %w", err)
			}
			c.Pages[page] = items
			count++
			break
		}
	}
	if err := rows.Err(); err != nil {
		return c, err
	}
	if complete && count != len(planprompt.TemplateKeys) {
		return c, errors.New("模板目录记录不完整")
	}
	return c, c.Validate()
}
func (s *Store) SavePlanPromptTemplateCatalog(ctx context.Context, next planprompt.TemplateCatalog, revision string, actorID int64) (planprompt.TemplateCatalog, error) {
	if err := next.Validate(); err != nil {
		return next, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return next, err
	}
	defer tx.Rollback()
	// Deterministic exclusive locks serialize initial creation and subsequent CAS.
	for i, key := range planprompt.TemplateKeys {
		_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_system_settings(setting_key,setting_group,label,value_text,input_type,sort_order,updated_by_user_id) VALUES(?,?,?,'[]','textarea',?,?) ON DUPLICATE KEY UPDATE setting_key=VALUES(setting_key)`, planPromptCatalogPrefix+key, planPromptCatalogGroup, "方案模板目录 "+key, i, actorID)
		if err != nil {
			return next, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT setting_key,value_text FROM mgmt_system_settings WHERE setting_group=? ORDER BY setting_key FOR UPDATE`, planPromptCatalogGroup)
	if err != nil {
		return next, err
	}
	current, err := readPlanPromptCatalog(rows, true)
	closeErr := rows.Close()
	if err != nil {
		return next, err
	}
	if closeErr != nil {
		return next, closeErr
	}
	if revision == "" || current.Revision() != revision {
		return next, ErrPlanPromptCatalogRevision
	}
	for _, key := range planprompt.TemplateKeys {
		data, err := json.Marshal(next.Pages[key])
		if err != nil {
			return next, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=? WHERE setting_key=? AND setting_group=?`, string(data), actorID, planPromptCatalogPrefix+key, planPromptCatalogGroup)
		if err != nil {
			return next, err
		}
		if count, err := result.RowsAffected(); err != nil || count > 1 {
			return next, errors.New("unexpected catalog update result")
		}
	}
	if err := tx.Commit(); err != nil {
		return next, err
	}
	return next, nil
}
