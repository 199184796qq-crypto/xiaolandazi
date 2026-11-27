package db

import (
	"context"
	"encoding/json"
	"strings"

	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/voiceasset"
)

type HumanFissionNameEdit struct {
	Part         int    `json:"part,omitempty"`
	Recipe       string `json:"recipe,omitempty"`
	Version      string `json:"version"`
	Name         string `json:"name"`
	ExpectedName string `json:"expected_name"`
}

func (s *Store) RenameHumanFission(ctx context.Context, tenant, plan, source int64, edit HumanFissionNameEdit) error {
	if !humanfission.ValidName(edit.Name) {
		return ErrFissionUpdated
	}
	return s.changeHumanFission(ctx, tenant, plan, source, func(job *HumanFissionJob) (string, error) {
		if job.Busy() {
			return "", ErrFissionBusy
		}
		if edit.Part > 0 && edit.Part <= len(job.State.Parts) && edit.Recipe == "" {
			part := &job.State.Parts[edit.Part-1]
			if part.Version() != edit.Version || part.DisplayName(edit.Part) != edit.ExpectedName {
				return "", ErrFissionUpdated
			}
			part.Name = edit.Name
			return "save", nil
		}
		index := job.State.RecipeIndex(edit.Recipe)
		if edit.Part != 0 || index < 0 || job.State.RecipeVersion() != edit.Version ||
			job.State.RecipeName(job.State.Recipes[index], index+1) != edit.ExpectedName {
			return "", ErrFissionUpdated
		}
		job.State.Recipes[index].Name = edit.Name
		return "save", nil
	})
}

func (s *Store) QueueHumanFissionNames(ctx context.Context, tenant, plan, source int64) error {
	return s.changeHumanFission(ctx, tenant, plan, source, func(job *HumanFissionJob) (string, error) {
		if job.Busy() {
			return "", ErrFissionBusy
		}
		if !job.State.NeedsNames() {
			return "", nil
		}
		job.State.Split, job.State.Merge, job.State.Exact = nil, nil, nil
		return "names", nil
	})
}

func (s *Store) QueueExactHumanFission(ctx context.Context, tenant, plan, source int64, target humanfission.ExactRequest) error {
	return s.changeHumanFission(ctx, tenant, plan, source, func(job *HumanFissionJob) (string, error) {
		if job.State.RecipeVersion() != target.Version || job.State.RecipeIndex(target.Recipe) < 0 {
			return "", ErrFissionUpdated
		}
		if job.Busy() {
			return "", ErrFissionBusy
		}
		job.State.Split, job.State.Merge, job.State.Exact = nil, nil, &target
		return "render_exact", nil
	})
}

func (s *Store) changeHumanFission(ctx context.Context, tenant, plan, source int64, change func(*HumanFissionJob) (string, error)) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	asset, err := fissionOriginal(ctx, tx, tenant, plan, source)
	if err != nil {
		return err
	}
	job, err := scanFission(tx.QueryRowContext(ctx, "SELECT "+humanFissionColumns+
		" FROM plan_human_fission WHERE source_id=? AND tenant_id=? AND plan_id=? FOR UPDATE", source, tenant, plan))
	if err != nil {
		return err
	}
	if job.State.Input != HumanFissionInput(asset) || job.State.ReadyParts() == 0 {
		return ErrFissionUpdated
	}
	action, err := change(&job)
	if err != nil || action == "" {
		return err
	}
	if action == "save" {
		raw, err := json.Marshal(job.State)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE plan_human_fission SET state_json=? WHERE source_id=?", raw, source); err != nil {
			return err
		}
		return tx.Commit()
	}
	return queueHumanFissionEdit(ctx, tx, job, action)
}

// Keep full audio manifests and word-level timestamps out of the live menu.
func (s *Store) ListHumanFissionChoices(ctx context.Context, tenant, plan int64) ([]HumanFissionJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.source_id,
		JSON_OBJECT('input',JSON_UNQUOTE(f.state_json->'$.input'), 'generation',COALESCE(JSON_UNQUOTE(f.state_json->'$.generation'),''),
		    'recipes',f.state_json->'$.recipes'),
		a.object_key,COALESCE(JSON_UNQUOTE(a.metadata_json->'$.script_transcript'),'')
		FROM plan_human_fission f JOIN media_assets a ON a.id=f.source_id AND a.tenant_id=f.tenant_id
		WHERE f.tenant_id=? AND f.plan_id=? AND a.status='active' AND a.asset_type=? AND a.object_key LIKE ?
		AND JSON_LENGTH(f.state_json,'$.recipes')>0 ORDER BY f.source_id`, tenant, plan, voiceasset.ScriptHumanAsset,
		ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var choices []HumanFissionJob
	for rows.Next() {
		job := HumanFissionJob{Tenant: tenant, Plan: plan}
		var raw []byte
		var key, transcript string
		if err = rows.Scan(&job.Source, &raw, &key, &transcript); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &job.State); err != nil {
			return nil, err
		}
		if strings.TrimSpace(transcript) != "" && job.State.Input == humanfission.Digest([]any{key, transcript}) {
			choices = append(choices, job)
		}
	}
	return choices, rows.Err()
}
