package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
)

var ErrDerivedSafetyNotFound = errors.New("derived safety snapshot not found")

func (s *Store) SaveLiveFactPackageSnapshot(ctx context.Context, tenant, plan int64, hash, text string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_fact_package_snapshots(tenant_id,plan_id,source_sha256,fact_package_md) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, tenant, plan, hash, text)
	return err
}

func (s *Store) SaveLiveDerivedSafety(ctx context.Context, actor int64, item model.LiveDerivedSafetyAsset) (model.LiveDerivedSafetyAsset, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_derived_safety_assets(tenant_id,plan_id,source_sha256,original_facts,original_wheel_json,safe_facts,safe_wheel_json,policy_hashes_json,changes_json,rewritten_count,notice,created_by_user_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, item.TenantID, item.PlanID, item.SourceSHA256, item.OriginalFacts, item.OriginalWheelJSON, item.SafeFacts, item.SafeWheelJSON, item.PolicyHashesJSON, item.ChangesJSON, item.RewrittenCount, item.Notice, actor)
	if err != nil {
		return item, err
	}
	return s.GetLiveDerivedSafety(ctx, item.TenantID, item.PlanID, item.SourceSHA256)
}
func (s *Store) GetLiveDerivedSafety(ctx context.Context, tenant, plan int64, hash string) (model.LiveDerivedSafetyAsset, error) {
	var a model.LiveDerivedSafetyAsset
	err := s.db.QueryRowContext(ctx, `SELECT id,tenant_id,plan_id,source_sha256,original_facts,original_wheel_json,safe_facts,safe_wheel_json,policy_hashes_json,changes_json,rewritten_count,notice,created_at FROM live_derived_safety_assets WHERE tenant_id=? AND plan_id=? AND source_sha256=?`, tenant, plan, hash).Scan(&a.ID, &a.TenantID, &a.PlanID, &a.SourceSHA256, &a.OriginalFacts, &a.OriginalWheelJSON, &a.SafeFacts, &a.SafeWheelJSON, &a.PolicyHashesJSON, &a.ChangesJSON, &a.RewrittenCount, &a.Notice, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrDerivedSafetyNotFound
	}
	return a, err
}

func (s *Store) SyncLiveRiskReviews(ctx context.Context, tenant, plan int64, items []model.LiveRiskReview) (map[string]model.LiveRiskReview, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize refreshes even when the plan has no review rows yet.
	var lockedPlan int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE", tenant, plan).Scan(&lockedPlan); err != nil {
		return nil, err
	}
	type priorReview struct {
		status, evidence, semanticDecision, semanticPromptSHA, semanticReason, semanticSuggestion, semanticProvider, semanticModel string
		semanticAt                                                                                                                 sql.NullTime
		appealStatus, appealReason                                                                                                 string
		appealBy                                                                                                                   sql.NullInt64
		appealAt                                                                                                                   sql.NullTime
		by                                                                                                                         sql.NullInt64
		at                                                                                                                         sql.NullTime
	}
	prior := map[string]priorReview{}
	activePrior := map[string]bool{}
	rows, err := tx.QueryContext(ctx, "SELECT claim_sha256,status,COALESCE(evidence,''),semantic_decision,semantic_prompt_sha256,COALESCE(semantic_reason,''),COALESCE(semantic_suggestion,''),semantic_provider,semantic_model,semantic_reviewed_at,appeal_status,COALESCE(appeal_reason,''),appeal_by_user_id,appeal_at,confirmed_by_user_id,confirmed_at,active FROM live_fact_risk_reviews WHERE tenant_id=? AND plan_id=? FOR UPDATE", tenant, plan)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var hash string
		var p priorReview
		var isActive bool
		if err = rows.Scan(&hash, &p.status, &p.evidence, &p.semanticDecision, &p.semanticPromptSHA, &p.semanticReason, &p.semanticSuggestion, &p.semanticProvider, &p.semanticModel, &p.semanticAt, &p.appealStatus, &p.appealReason, &p.appealBy, &p.appealAt, &p.by, &p.at, &isActive); err != nil {
			rows.Close()
			return nil, err
		}
		prior[hash] = p
		if isActive {
			activePrior[hash] = true
		}
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Old releases removed temporarily absent claims after backing them up.
	// Restore only an exact tenant/plan/claim identity, never a text similarity.
	rows, err = tx.QueryContext(ctx, "SELECT claim_sha256,status,COALESCE(evidence,''),semantic_decision,semantic_prompt_sha256,COALESCE(semantic_reason,''),COALESCE(semantic_suggestion,''),semantic_provider,semantic_model,semantic_reviewed_at,appeal_status,COALESCE(appeal_reason,''),appeal_by_user_id,appeal_at,confirmed_by_user_id,confirmed_at FROM live_fact_risk_reviews_dedup_backup WHERE tenant_id=? AND plan_id=? AND status='confirmed' ORDER BY confirmed_at,id", tenant, plan)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var hash string
		var p priorReview
		if err = rows.Scan(&hash, &p.status, &p.evidence, &p.semanticDecision, &p.semanticPromptSHA, &p.semanticReason, &p.semanticSuggestion, &p.semanticProvider, &p.semanticModel, &p.semanticAt, &p.appealStatus, &p.appealReason, &p.appealBy, &p.appealAt, &p.by, &p.at); err != nil {
			rows.Close()
			return nil, err
		}
		current, exists := prior[hash]
		if !exists || (current.status != "confirmed" && current.semanticPromptSHA == p.semanticPromptSHA && current.semanticDecision != "block" && current.appealStatus != "pending") {
			prior[hash] = p
		}
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := map[string]model.LiveRiskReview{}
	for _, item := range items {
		if _, ok := result[item.ClaimSHA256]; ok {
			continue
		}
		p := prior[item.ClaimSHA256]
		if item.RecheckLegacyBoundaryBlock && p.semanticDecision == "block" && p.semanticReason == model.GeneratedWheelAbsoluteBoundaryReason {
			// Re-run the model; do not automatically approve or confirm the claim.
			p.semanticDecision, p.semanticReason, p.semanticSuggestion = "", "", ""
			p.semanticProvider, p.semanticModel = "", ""
			p.semanticAt = sql.NullTime{}
		}
		item.Status, item.Evidence = "pending", ""
		if item.ProgramDecision == "" {
			item.ProgramDecision = "matched"
		}
		if item.SemanticDecision == "" {
			item.SemanticDecision = "not_required"
		}
		if p.semanticDecision != "" && item.SemanticDecision == "pending" && p.semanticPromptSHA == item.SemanticPromptSHA {
			item.SemanticDecision, item.SemanticReason, item.SemanticSuggestion = p.semanticDecision, p.semanticReason, p.semanticSuggestion
			item.SemanticProvider, item.SemanticModel = p.semanticProvider, p.semanticModel
			if p.semanticAt.Valid {
				value := p.semanticAt.Time
				item.SemanticReviewedAt = &value
			}
		}
		item.AppealStatus, item.AppealReason = p.appealStatus, p.appealReason
		if p.appealBy.Valid {
			value := p.appealBy.Int64
			item.AppealBy = &value
		}
		if p.appealAt.Valid {
			value := p.appealAt.Time
			item.AppealAt = &value
		}
		confirmableP0 := item.Severity == "P0" && (item.SemanticDecision == "pass" || item.SemanticDecision == "evidence_required")
		if (item.Severity != "P0" || confirmableP0) && p.status == "confirmed" && strings.TrimSpace(p.evidence) != "" {
			item.Status, item.Evidence = p.status, p.evidence
			if p.by.Valid {
				value := p.by.Int64
				item.ConfirmedBy = &value
			}
			if p.at.Valid {
				value := p.at.Time
				item.ConfirmedAt = &value
			}
		} else {
			p.by = sql.NullInt64{}
			p.at = sql.NullTime{}
		}
		refs, err := json.Marshal(item.Sources)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO live_fact_risk_reviews(tenant_id,plan_id,claim_sha256,source_key,claim_text,rule_id,severity,rule_sha256,status,evidence,reason_text,program_decision,semantic_decision,semantic_prompt_sha256,semantic_reason,semantic_suggestion,semantic_provider,semantic_model,semantic_reviewed_at,appeal_status,appeal_reason,appeal_by_user_id,appeal_at,active,confirmed_by_user_id,confirmed_at,semantic_key,subject_text,source_refs_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,?,?) ON DUPLICATE KEY UPDATE source_key=VALUES(source_key),claim_text=VALUES(claim_text),status=VALUES(status),evidence=VALUES(evidence),reason_text=VALUES(reason_text),program_decision=VALUES(program_decision),semantic_decision=VALUES(semantic_decision),semantic_prompt_sha256=VALUES(semantic_prompt_sha256),semantic_reason=VALUES(semantic_reason),semantic_suggestion=VALUES(semantic_suggestion),semantic_provider=VALUES(semantic_provider),semantic_model=VALUES(semantic_model),semantic_reviewed_at=VALUES(semantic_reviewed_at),appeal_status=VALUES(appeal_status),appeal_reason=VALUES(appeal_reason),appeal_by_user_id=VALUES(appeal_by_user_id),appeal_at=VALUES(appeal_at),active=1,confirmed_by_user_id=VALUES(confirmed_by_user_id),confirmed_at=VALUES(confirmed_at),semantic_key=VALUES(semantic_key),subject_text=VALUES(subject_text),source_refs_json=VALUES(source_refs_json)`, tenant, plan, item.ClaimSHA256, item.SourceKey, item.Text, item.RuleID, item.Severity, item.RuleSHA256, item.Status, item.Evidence, item.Reason, item.ProgramDecision, item.SemanticDecision, item.SemanticPromptSHA, item.SemanticReason, item.SemanticSuggestion, item.SemanticProvider, item.SemanticModel, item.SemanticReviewedAt, item.AppealStatus, item.AppealReason, item.AppealBy, item.AppealAt, p.by, p.at, item.SemanticKey, item.Subject, string(refs))
		if err != nil {
			return nil, err
		}
		result[item.ClaimSHA256] = item
	}
	// Temporarily absent claims are hidden, not deleted. A later reappearance can
	// retain its confirmation even after sorting, editing or regenerating a pool.
	for hash := range activePrior {
		if _, ok := result[hash]; ok {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO live_fact_risk_reviews_dedup_backup SELECT * FROM live_fact_risk_reviews WHERE tenant_id=? AND plan_id=? AND claim_sha256=?`, tenant, plan, hash); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_fact_risk_reviews SET active=0 WHERE tenant_id=? AND plan_id=? AND claim_sha256=?`, tenant, plan, hash); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) ListLiveRiskReviews(ctx context.Context, tenant, plan int64) ([]model.LiveRiskReview, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT claim_sha256,source_key,claim_text,rule_id,severity,rule_sha256,status,COALESCE(evidence,''),COALESCE(reason_text,''),program_decision,semantic_decision,semantic_prompt_sha256,COALESCE(semantic_reason,''),COALESCE(semantic_suggestion,''),semantic_provider,semantic_model,semantic_reviewed_at,appeal_status,COALESCE(appeal_reason,''),appeal_by_user_id,appeal_at,confirmed_by_user_id,confirmed_at,semantic_key,COALESCE(subject_text,''),COALESCE(source_refs_json,'[]') FROM live_fact_risk_reviews WHERE tenant_id=? AND plan_id=? AND active=1 ORDER BY severity,source_key,rule_id`, tenant, plan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.LiveRiskReview{}
	for rows.Next() {
		var a model.LiveRiskReview
		var confirmedBy sql.NullInt64
		var confirmedAt sql.NullTime
		var semanticAt, appealAt sql.NullTime
		var appealBy sql.NullInt64
		var refs string
		if err = rows.Scan(&a.ClaimSHA256, &a.SourceKey, &a.Text, &a.RuleID, &a.Severity, &a.RuleSHA256, &a.Status, &a.Evidence, &a.Reason, &a.ProgramDecision, &a.SemanticDecision, &a.SemanticPromptSHA, &a.SemanticReason, &a.SemanticSuggestion, &a.SemanticProvider, &a.SemanticModel, &semanticAt, &a.AppealStatus, &a.AppealReason, &appealBy, &appealAt, &confirmedBy, &confirmedAt, &a.SemanticKey, &a.Subject, &refs); err != nil {
			return nil, err
		}
		if confirmedBy.Valid {
			value := confirmedBy.Int64
			a.ConfirmedBy = &value
		}
		if confirmedAt.Valid {
			value := confirmedAt.Time
			a.ConfirmedAt = &value
		}
		if semanticAt.Valid {
			value := semanticAt.Time
			a.SemanticReviewedAt = &value
		}
		if appealBy.Valid {
			value := appealBy.Int64
			a.AppealBy = &value
		}
		if appealAt.Valid {
			value := appealAt.Time
			a.AppealAt = &value
		}
		if err = json.Unmarshal([]byte(refs), &a.Sources); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func normalizeLiveRiskConfirmation(severity, semanticDecision, evidence string) (string, error) {
	evidence = strings.TrimSpace(evidence)
	switch severity {
	case "P0":
		switch semanticDecision {
		case "pass":
			if evidence == "" {
				evidence = "用户已确认该表达在当前完整语境中使用"
			}
			return evidence, nil
		case "evidence_required":
			if evidence == "" {
				evidence = "用户已知晓系统提示并确认该事实内容真实，同意按当前内容用于本方案"
			}
			return evidence, nil
		default:
			return "", errors.New("该项经程序和语义复核仍为P0，不能由用户直接放行")
		}
	case "P1":
		if evidence == "" {
			evidence = "用户已知晓系统提示并确认该事实内容真实，同意按当前内容用于本方案"
		}
		return evidence, nil
	case "P2":
		if evidence == "" {
			return "用户已确认该项P2表达可以使用", nil
		}
		return evidence, nil
	default:
		return "", errors.New("风险档位无效，不能确认")
	}
}

func (s *Store) ConfirmLiveRiskReview(ctx context.Context, tenant, plan, actor int64, hash, evidence string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var severity, semanticDecision string
	if err = tx.QueryRowContext(ctx, `SELECT severity,semantic_decision FROM live_fact_risk_reviews WHERE tenant_id=? AND plan_id=? AND claim_sha256=? AND active=1 FOR UPDATE`, tenant, plan, hash).Scan(&severity, &semanticDecision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("风险卡已变化，请刷新后重新确认")
		}
		return err
	}
	evidence, err = normalizeLiveRiskConfirmation(severity, semanticDecision, evidence)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE live_fact_risk_reviews SET status='confirmed',evidence=?,confirmed_by_user_id=?,confirmed_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND claim_sha256=? AND active=1 AND (severity IN ('P1','P2') OR (severity='P0' AND semantic_decision IN ('pass','evidence_required')))`, evidence, actor, tenant, plan, hash)
	if err != nil {
		return err
	}
	if n, countErr := result.RowsAffected(); countErr != nil || n != 1 {
		return errors.New("风险卡已变化，请刷新后重新确认")
	}
	return tx.Commit()
}

func (s *Store) SaveLiveRiskSemanticDecisions(ctx context.Context, tenant, plan int64, promptSHA, provider, modelName string, items []model.LiveRiskSemanticDecision) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		if item.Decision != "pass" && item.Decision != "block" && item.Decision != "evidence_required" {
			return errors.New("语义复核决定无效")
		}
		result, execErr := tx.ExecContext(ctx, `UPDATE live_fact_risk_reviews SET semantic_decision=?,semantic_reason=?,semantic_suggestion=?,semantic_provider=?,semantic_model=?,semantic_reviewed_at=CURRENT_TIMESTAMP(3),status='pending',evidence='',confirmed_by_user_id=NULL,confirmed_at=NULL WHERE tenant_id=? AND plan_id=? AND claim_sha256=? AND active=1 AND severity='P0' AND semantic_decision='pending' AND semantic_prompt_sha256=?`, item.Decision, strings.TrimSpace(item.Reason), strings.TrimSpace(item.Suggestion), strings.TrimSpace(provider), strings.TrimSpace(modelName), tenant, plan, item.ClaimSHA256, promptSHA)
		if execErr != nil {
			return execErr
		}
		if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
			return errors.New("风险卡已变化，请刷新后重新语义复核")
		}
	}
	return tx.Commit()
}

func (s *Store) SubmitLiveRiskAppeal(ctx context.Context, tenant, plan, actor int64, hash, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("请填写申请复核的具体原因")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var severity, semanticDecision, appealStatus string
	if err = tx.QueryRowContext(ctx, `SELECT severity,semantic_decision,appeal_status FROM live_fact_risk_reviews WHERE tenant_id=? AND plan_id=? AND claim_sha256=? AND active=1 FOR UPDATE`, tenant, plan, hash).Scan(&severity, &semanticDecision, &appealStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("风险卡已变化，请刷新后重试")
		}
		return err
	}
	if severity != "P0" || (semanticDecision != "block" && semanticDecision != "not_required") {
		return errors.New("只有程序与语义复核仍未通过的P0项目可以提交后台申请")
	}
	if appealStatus == "pending" {
		return errors.New("该项目已经提交后台复核，请勿重复申请")
	}
	result, err := tx.ExecContext(ctx, `UPDATE live_fact_risk_reviews SET appeal_status='pending',appeal_reason=?,appeal_by_user_id=?,appeal_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND claim_sha256=? AND active=1`, reason, actor, tenant, plan, hash)
	if err != nil {
		return err
	}
	if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
		return errors.New("风险卡已变化，请刷新后重试")
	}
	return tx.Commit()
}

// Only derived configuration changes; saved original speech/audio are untouched.
func (s *Store) SetVersionDerivedSafety(ctx context.Context, tenant, plan, room, version int64, expected map[string]any, asset model.LiveDerivedSafetyAsset) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT generation_context_json FROM live_agent_plan_versions WHERE tenant_id=? AND plan_id=? AND room_id=? AND id=? AND lifecycle_status='draft' FOR UPDATE`, tenant, plan, room, version).Scan(&raw); err != nil {
		return err
	}
	var current map[string]any
	if err = json.Unmarshal([]byte(raw), &current); err != nil {
		return err
	}
	prior, _ := json.Marshal(expected)
	now, _ := json.Marshal(current)
	if string(prior) != string(now) {
		return errors.New("版本配置已变化，请刷新后重新发布")
	}
	current["derived_safety"] = map[string]any{"asset_id": asset.ID, "source_sha256": asset.SourceSHA256, "rewritten_count": asset.RewrittenCount, "notice": asset.Notice}
	encoded, _ := json.Marshal(current)
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_versions SET generation_context_json=? WHERE tenant_id=? AND plan_id=? AND room_id=? AND id=?`, string(encoded), tenant, plan, room, version)
	if err != nil {
		return err
	}
	return tx.Commit()
}
