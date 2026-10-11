package db

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/lecturetask"
)

// Preserve the original quote and old task in the analysis archive. A single
// version-checked transaction updates only the selected object/detail.
func (s *Store) UpdateLectureDetail(ctx context.Context, tenant, plan, actor, taskID, revision, detailID int64, storage, name, text, sourceRef string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan).Scan(&exists); err != nil {
		return err
	}
	var raw, angles string
	if err = tx.QueryRowContext(ctx, `SELECT facts_json,angles_json FROM live_plan_lecture_tasks WHERE tenant_id=? AND plan_id=? AND id=? AND revision=? AND status='active' FOR UPDATE`, tenant, plan, taskID, revision).Scan(&raw, &angles); err != nil {
		return ErrLectureChanged
	}
	var asset struct {
		Root *lecturetask.Fact `json:"root_fact"`
	}
	if err = json.Unmarshal([]byte(angles), &asset); err != nil {
		return err
	}
	if asset.Root != nil {
		current, e := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+` WHERE tenant_id=? AND plan_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan, asset.Root.ID))
		if e != nil || current.VersionNo != asset.Root.Version || current.Value != asset.Root.Text {
			return ErrLectureChanged
		}
	}
	var facts []lecturetask.Fact
	if err = json.Unmarshal([]byte(raw), &facts); err != nil {
		return err
	}
	index := -1
	for i, f := range facts {
		if f.ID == detailID && f.Storage == storage {
			index = i
			break
		}
	}
	if index < 0 {
		return errors.New("资料不属于当前对象，未保存")
	}
	f := &facts[index]
	if f.Storage != "lecture" {
		current, e := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+` WHERE tenant_id=? AND plan_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan, detailID))
		if e != nil || current.VersionNo != f.Version || current.Value != f.Text {
			return ErrLectureChanged
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_facts SET fact_key=?,fact_value=?,source_type='story_lecture',source_ref=?,version_no=version_no+1,updated_by_user_id=? WHERE tenant_id=? AND plan_id=? AND id=?`, name, text, sourceRef, actor, tenant, plan, detailID); err != nil {
			return err
		}
		if err = saveLectureRootRevision(ctx, tx, tenant, plan, detailID, actor); err != nil {
			return err
		}
	}
	f.Name, f.Text, f.Version = name, text, f.Version+1
	next, _ := json.Marshal(facts)
	if _, err = tx.ExecContext(ctx, `UPDATE live_plan_lecture_tasks SET facts_json=?,revision=revision+1 WHERE tenant_id=? AND plan_id=? AND id=? AND revision=?`, string(next), tenant, plan, taskID, revision); err != nil {
		return err
	}
	return tx.Commit()
}
