package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/lecturetask"
	"strings"
	"unicode/utf8"
)

func saveLectureRootRevision(ctx context.Context, tx *sql.Tx, tenant, plan, id, actor int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_fact_revisions(tenant_id,plan_id,fact_id,version_no,action,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,actor_user_id,scope_json)
 SELECT tenant_id,plan_id,id,version_no,'update',category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,?,scope_json FROM live_agent_plan_facts WHERE tenant_id=? AND plan_id=? AND id=?`, actor, tenant, plan, id)
	return err
}

// Bind a user-selected legacy fact as the level-one root. Remaining evidence
// stays under the selected subject, preserving content and original provenance.
func (s *Store) AttachLectureSubject(ctx context.Context, tenant, plan, actor, taskID, revision, rootID int64, subject string, detailIDs ...int64) error {
	subject = strings.TrimSpace(subject)
	if tenant <= 0 || plan <= 0 || actor <= 0 || taskID <= 0 || revision <= 0 || rootID <= 0 || subject == "" || utf8.RuneCountInString(subject) > 100 {
		return fmt.Errorf("资料对象或关联事实无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan).Scan(&exists); err != nil {
		return err
	}
	var angles, rawFacts string
	if err = tx.QueryRowContext(ctx, `SELECT angles_json,facts_json FROM live_plan_lecture_tasks WHERE tenant_id=? AND plan_id=? AND id=? AND revision=? AND status='active' FOR UPDATE`, tenant, plan, taskID, revision).Scan(&angles, &rawFacts); err != nil {
		return err
	}
	var data map[string]any
	var facts []lecturetask.Fact
	if err = json.Unmarshal([]byte(angles), &data); err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("讲解任务内容无效")
	}
	if err = json.Unmarshal([]byte(rawFacts), &facts); err != nil {
		return err
	}
	rootIndex := -1
	for i, f := range facts {
		if f.Storage == "" && f.ID == rootID {
			rootIndex = i
			break
		}
	}
	if rootIndex < 0 {
		return fmt.Errorf("所选一级事实不属于这份资料")
	}
	saved, err := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+` WHERE tenant_id=? AND plan_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan, rootID))
	if err != nil {
		return err
	}
	root := facts[rootIndex]
	if saved.VersionNo != root.Version || saved.Value != root.Text {
		return ErrLectureChanged
	}
	if saved.SourceType != "story_subject" {
		if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_facts SET source_type='story_subject',version_no=version_no+1,updated_by_user_id=? WHERE tenant_id=? AND plan_id=? AND id=?`, actor, tenant, plan, rootID); err != nil {
			return err
		}
		root.Version++
		if err = saveLectureRootRevision(ctx, tx, tenant, plan, rootID, actor); err != nil {
			return err
		}
	}
	data["subject"], data["root_fact"] = subject, root
	facts = append(facts[:rootIndex], facts[rootIndex+1:]...)
	seen := map[int64]bool{}
	for _, f := range facts {
		seen[f.ID] = true
	}
	for _, id := range detailIDs {
		if id == rootID {
			return fmt.Errorf("一级事实不能同时作为二级资料")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		v, e := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+` WHERE tenant_id=? AND plan_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan, id))
		if e != nil {
			return e
		}
		if v.SourceType != "story_lecture" {
			if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_facts SET source_type='story_lecture',version_no=version_no+1,updated_by_user_id=? WHERE tenant_id=? AND plan_id=? AND id=?`, actor, tenant, plan, id); err != nil {
				return err
			}
			v.VersionNo++
			if err = saveLectureRootRevision(ctx, tx, tenant, plan, id, actor); err != nil {
				return err
			}
		}
		facts = append(facts, lecturetask.Fact{ID: v.ID, Version: v.VersionNo, Category: v.Category, Name: v.Key, Text: v.Value, Quote: v.SourceQuote})
	}
	a, _ := json.Marshal(data)
	f, _ := json.Marshal(facts)
	if _, err = tx.ExecContext(ctx, `UPDATE live_plan_lecture_tasks SET angles_json=?,facts_json=?,revision=revision+1 WHERE tenant_id=? AND plan_id=? AND id=? AND revision=?`, string(a), string(f), tenant, plan, taskID, revision); err != nil {
		return err
	}
	return tx.Commit()
}
