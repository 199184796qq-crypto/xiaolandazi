package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanProductAttributeNotFound = errors.New("live agent plan product attribute not found")
var ErrLiveAgentPlanProductAttributeVersionConflict = errors.New("live agent plan product attribute version conflict")

const liveAgentPlanProductAttributeSelect = `
	SELECT id, tenant_id, plan_id, product_link_id, attribute_code, display_name, value_text, unit,
	       display_type, display_priority, source_quote, source_type, source_ref, status, version_no,
	       created_by_user_id, updated_by_user_id, created_at, updated_at
	FROM live_agent_plan_product_attributes
`

func scanLiveAgentPlanProductAttribute(row interface{ Scan(dest ...any) error }) (model.LiveAgentPlanProductAttribute, error) {
	var item model.LiveAgentPlanProductAttribute
	var createdBy, updatedBy sql.NullInt64
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.ProductLinkID, &item.Code, &item.Label,
		&item.Value, &item.Unit, &item.DisplayType, &item.DisplayPriority, &item.SourceQuote,
		&item.SourceType, &item.SourceRef, &item.Status, &item.VersionNo, &createdBy, &updatedBy,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	return item, nil
}

func (s *Store) listLiveAgentPlanProductAttributes(ctx context.Context, tenantID, planID int64) (map[int64][]model.LiveAgentPlanProductAttribute, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanProductAttributeSelect+`
		WHERE tenant_id=? AND plan_id=? AND status='active'
		ORDER BY product_link_id ASC, display_priority ASC, id ASC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64][]model.LiveAgentPlanProductAttribute)
	for rows.Next() {
		item, err := scanLiveAgentPlanProductAttribute(rows)
		if err != nil {
			return nil, err
		}
		result[item.ProductLinkID] = append(result[item.ProductLinkID], item)
	}
	return result, rows.Err()
}

func (s *Store) attachLiveAgentPlanProductAttributes(ctx context.Context, tenantID, planID int64, links []model.LiveAgentPlanProductLink) error {
	grouped, err := s.listLiveAgentPlanProductAttributes(ctx, tenantID, planID)
	if err != nil {
		return err
	}
	for index := range links {
		links[index].Attributes = grouped[links[index].ID]
		if links[index].Attributes == nil {
			links[index].Attributes = []model.LiveAgentPlanProductAttribute{}
		}
	}
	return nil
}

func normalizeProductAttributeCandidate(item model.LiveAgentPlanProductAttributeCandidate, fallbackPriority int) model.LiveAgentPlanProductAttributeCandidate {
	item.Code = strings.ToLower(strings.TrimSpace(item.Code))
	item.Label = strings.TrimSpace(item.Label)
	item.Value = strings.TrimSpace(item.Value)
	item.Unit = strings.TrimSpace(item.Unit)
	item.DisplayType = strings.ToLower(strings.TrimSpace(item.DisplayType))
	item.SourceQuote = strings.TrimSpace(item.SourceQuote)
	if item.DisplayType == "" {
		item.DisplayType = "text"
	}
	if item.DisplayPriority == 0 {
		item.DisplayPriority = fallbackPriority
	}
	return item
}

func writeLiveAgentPlanProductAttributeRevision(
	ctx context.Context, tx *sql.Tx, item model.LiveAgentPlanProductAttribute, action string, actorUserID int64,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_product_attribute_revisions (
			tenant_id, plan_id, product_link_id, attribute_id, version_no, action, attribute_code,
			display_name, value_text, unit, display_type, display_priority, status, source_quote,
			source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.TenantID, item.PlanID, item.ProductLinkID, item.ID, item.VersionNo, action, item.Code,
		item.Label, item.Value, item.Unit, item.DisplayType, item.DisplayPriority, item.Status,
		item.SourceQuote, item.SourceType, item.SourceRef, actorUserID)
	return err
}

// adoptLiveAgentPlanProductAttributes replaces the inferred personalized layer
// for an adopted card. Rows are soft-disabled first so removed inference cannot
// survive as a generation ghost.
func adoptLiveAgentPlanProductAttributes(
	ctx context.Context, tx *sql.Tx, tenantID, planID, productLinkID, actorUserID int64,
	items []model.LiveAgentPlanProductAttributeCandidate, sourceRef string,
) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plan_product_attributes
		SET status='disabled', source_type='analysis_replaced', updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND plan_id=? AND product_link_id=? AND status='active'
	`, actorUserID, tenantID, planID, productLinkID); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(items))
	for index, raw := range items {
		item := normalizeProductAttributeCandidate(raw, (index+1)*10)
		if item.Code == "" || item.Label == "" || item.Value == "" {
			continue
		}
		if _, ok := seen[item.Code]; ok {
			continue
		}
		seen[item.Code] = struct{}{}
		var existingID, version int64
		err := tx.QueryRowContext(ctx, `
			SELECT id, version_no FROM live_agent_plan_product_attributes
			WHERE tenant_id=? AND plan_id=? AND product_link_id=? AND attribute_code=? FOR UPDATE
		`, tenantID, planID, productLinkID, item.Code).Scan(&existingID, &version)
		if errors.Is(err, sql.ErrNoRows) {
			insertResult, insertErr := tx.ExecContext(ctx, `
				INSERT INTO live_agent_plan_product_attributes (
					tenant_id, plan_id, product_link_id, attribute_code, display_name, value_text, unit,
					display_type, display_priority, source_quote, source_type, source_ref, status,
					version_no, created_by_user_id, updated_by_user_id
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'analysis_adoption', ?, 'active', 1, ?, ?)
			`, tenantID, planID, productLinkID, item.Code, item.Label, item.Value, item.Unit,
				item.DisplayType, item.DisplayPriority, item.SourceQuote, strings.TrimSpace(sourceRef), actorUserID, actorUserID)
			if insertErr != nil {
				return insertErr
			}
			attributeID, insertErr := insertResult.LastInsertId()
			if insertErr != nil {
				return insertErr
			}
			revision := model.LiveAgentPlanProductAttribute{ID: attributeID, TenantID: tenantID, PlanID: planID, ProductLinkID: productLinkID, Code: item.Code, Label: item.Label, Value: item.Value, Unit: item.Unit, DisplayType: item.DisplayType, DisplayPriority: item.DisplayPriority, SourceQuote: item.SourceQuote, SourceType: "analysis_adoption", SourceRef: strings.TrimSpace(sourceRef), Status: "active", VersionNo: 1}
			if err := writeLiveAgentPlanProductAttributeRevision(ctx, tx, revision, "adopt", actorUserID); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		nextVersion := version + 1
		_, err = tx.ExecContext(ctx, `
			UPDATE live_agent_plan_product_attributes
			SET display_name=?, value_text=?, unit=?, display_type=?, display_priority=?, source_quote=?,
			    source_type='analysis_adoption', source_ref=?, status='active', version_no=?,
			    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND tenant_id=? AND plan_id=? AND product_link_id=?
		`, item.Label, item.Value, item.Unit, item.DisplayType, item.DisplayPriority, item.SourceQuote,
			strings.TrimSpace(sourceRef), nextVersion, actorUserID, existingID, tenantID, planID, productLinkID)
		if err != nil {
			return err
		}
		revision := model.LiveAgentPlanProductAttribute{ID: existingID, TenantID: tenantID, PlanID: planID, ProductLinkID: productLinkID, Code: item.Code, Label: item.Label, Value: item.Value, Unit: item.Unit, DisplayType: item.DisplayType, DisplayPriority: item.DisplayPriority, SourceQuote: item.SourceQuote, SourceType: "analysis_adoption", SourceRef: strings.TrimSpace(sourceRef), Status: "active", VersionNo: nextVersion}
		if err := writeLiveAgentPlanProductAttributeRevision(ctx, tx, revision, "readopt", actorUserID); err != nil {
			return err
		}
	}
	return nil
}

func disableLiveAgentPlanProductAttributesForLink(ctx context.Context, tx *sql.Tx, tenantID, planID, productLinkID, actorUserID int64) error {
	rows, err := tx.QueryContext(ctx, liveAgentPlanProductAttributeSelect+`
		WHERE tenant_id=? AND plan_id=? AND product_link_id=? AND status='active' FOR UPDATE
	`, tenantID, planID, productLinkID)
	if err != nil {
		return err
	}
	items := make([]model.LiveAgentPlanProductAttribute, 0)
	for rows.Next() {
		item, scanErr := scanLiveAgentPlanProductAttribute(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		items = append(items, item)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		item.Status = "disabled"
		item.SourceType = "system_agent_delete"
		item.SourceRef = ""
		item.VersionNo++
		if _, err = tx.ExecContext(ctx, `
			UPDATE live_agent_plan_product_attributes
			SET status='disabled', source_type='system_agent_delete', source_ref='', version_no=?,
			    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND status='active'
		`, item.VersionNo, actorUserID, item.ID); err != nil {
			return err
		}
		if err = writeLiveAgentPlanProductAttributeRevision(ctx, tx, item, "delete_with_product", actorUserID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateLiveAgentPlanProductAttribute(
	ctx context.Context, tenantID, planID, productLinkID, actorUserID int64,
	input model.CreateLiveAgentPlanProductAttributeInput,
) (model.LiveAgentPlanProductAttribute, error) {
	item := normalizeProductAttributeCandidate(model.LiveAgentPlanProductAttributeCandidate{
		Code: input.Code, Label: input.Label, Value: input.Value, Unit: input.Unit,
		DisplayType: input.DisplayType, DisplayPriority: input.DisplayPriority, SourceQuote: input.SourceQuote,
	}, 10)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	defer tx.Rollback()
	var linkExists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plan_product_links WHERE id=? AND tenant_id=? AND plan_id=? AND status='active' FOR UPDATE`, productLinkID, tenantID, planID).Scan(&linkExists); errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanProductAttribute{}, ErrLiveAgentPlanProductLinkNotFound
	} else if err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	var existing model.LiveAgentPlanProductAttribute
	existing, err = scanLiveAgentPlanProductAttribute(tx.QueryRowContext(ctx, liveAgentPlanProductAttributeSelect+` WHERE tenant_id=? AND plan_id=? AND product_link_id=? AND attribute_code=? FOR UPDATE`, tenantID, planID, productLinkID, item.Code))
	if err == nil && existing.Status == "active" {
		return model.LiveAgentPlanProductAttribute{}, fmt.Errorf("same product attribute code already exists")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	var saved model.LiveAgentPlanProductAttribute
	if err == nil {
		saved = existing
		saved.Label, saved.Value, saved.Unit = item.Label, item.Value, item.Unit
		saved.DisplayType, saved.DisplayPriority = item.DisplayType, item.DisplayPriority
		saved.SourceQuote, saved.SourceType, saved.SourceRef = item.SourceQuote, "system_agent_edit", ""
		saved.Status, saved.VersionNo = "active", existing.VersionNo+1
		_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_product_attributes SET display_name=?, value_text=?, unit=?, display_type=?, display_priority=?, source_quote=?, source_type='system_agent_edit', source_ref='', status='active', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=?`, saved.Label, saved.Value, saved.Unit, saved.DisplayType, saved.DisplayPriority, saved.SourceQuote, saved.VersionNo, actorUserID, saved.ID)
	} else {
		var insertResult sql.Result
		insertResult, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_product_attributes (tenant_id, plan_id, product_link_id, attribute_code, display_name, value_text, unit, display_type, display_priority, source_quote, source_type, source_ref, status, version_no, created_by_user_id, updated_by_user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'system_agent_edit', '', 'active', 1, ?, ?)`, tenantID, planID, productLinkID, item.Code, item.Label, item.Value, item.Unit, item.DisplayType, item.DisplayPriority, item.SourceQuote, actorUserID, actorUserID)
		if err == nil {
			var id int64
			id, err = insertResult.LastInsertId()
			saved = model.LiveAgentPlanProductAttribute{ID: id, TenantID: tenantID, PlanID: planID, ProductLinkID: productLinkID, Code: item.Code, Label: item.Label, Value: item.Value, Unit: item.Unit, DisplayType: item.DisplayType, DisplayPriority: item.DisplayPriority, SourceQuote: item.SourceQuote, SourceType: "system_agent_edit", Status: "active", VersionNo: 1}
		}
	}
	if err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	if err = writeLiveAgentPlanProductAttributeRevision(ctx, tx, saved, "create", actorUserID); err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	return scanLiveAgentPlanProductAttribute(s.db.QueryRowContext(ctx, liveAgentPlanProductAttributeSelect+` WHERE id=?`, saved.ID))
}

func (s *Store) UpdateLiveAgentPlanProductAttribute(
	ctx context.Context, tenantID, planID, productLinkID, attributeID, actorUserID int64,
	input model.UpdateLiveAgentPlanProductAttributeInput,
) (model.LiveAgentPlanProductAttribute, error) {
	item := normalizeProductAttributeCandidate(model.LiveAgentPlanProductAttributeCandidate{Code: input.Code, Label: input.Label, Value: input.Value, Unit: input.Unit, DisplayType: input.DisplayType, DisplayPriority: input.DisplayPriority}, 10)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	defer tx.Rollback()
	current, err := scanLiveAgentPlanProductAttribute(tx.QueryRowContext(ctx, liveAgentPlanProductAttributeSelect+` WHERE id=? AND tenant_id=? AND plan_id=? AND product_link_id=? AND status='active' FOR UPDATE`, attributeID, tenantID, planID, productLinkID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanProductAttribute{}, ErrLiveAgentPlanProductAttributeNotFound
	}
	if err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	if input.ExpectedVersionNo > 0 && input.ExpectedVersionNo != current.VersionNo {
		return model.LiveAgentPlanProductAttribute{}, ErrLiveAgentPlanProductAttributeVersionConflict
	}
	if item.Code != current.Code {
		var conflictID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plan_product_attributes WHERE product_link_id=? AND attribute_code=? AND id<>? LIMIT 1`, productLinkID, item.Code, attributeID).Scan(&conflictID)
		if err == nil {
			return model.LiveAgentPlanProductAttribute{}, fmt.Errorf("same product attribute code already exists")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.LiveAgentPlanProductAttribute{}, err
		}
	}
	current.Code, current.Label, current.Value, current.Unit = item.Code, item.Label, item.Value, item.Unit
	current.DisplayType, current.DisplayPriority = item.DisplayType, item.DisplayPriority
	current.SourceType, current.SourceRef, current.VersionNo = "system_agent_edit", "", current.VersionNo+1
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_product_attributes SET attribute_code=?, display_name=?, value_text=?, unit=?, display_type=?, display_priority=?, source_type='system_agent_edit', source_ref='', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=?`, current.Code, current.Label, current.Value, current.Unit, current.DisplayType, current.DisplayPriority, current.VersionNo, actorUserID, current.ID)
	if err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	if err = writeLiveAgentPlanProductAttributeRevision(ctx, tx, current, "update", actorUserID); err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.LiveAgentPlanProductAttribute{}, err
	}
	return scanLiveAgentPlanProductAttribute(s.db.QueryRowContext(ctx, liveAgentPlanProductAttributeSelect+` WHERE id=?`, attributeID))
}

func (s *Store) DeleteLiveAgentPlanProductAttribute(ctx context.Context, tenantID, planID, productLinkID, attributeID, actorUserID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanLiveAgentPlanProductAttribute(tx.QueryRowContext(ctx, liveAgentPlanProductAttributeSelect+` WHERE id=? AND tenant_id=? AND plan_id=? AND product_link_id=? AND status='active' FOR UPDATE`, attributeID, tenantID, planID, productLinkID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveAgentPlanProductAttributeNotFound
	}
	if err != nil {
		return err
	}
	current.Status, current.SourceType, current.SourceRef, current.VersionNo = "disabled", "system_agent_delete", "", current.VersionNo+1
	if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_product_attributes SET status='disabled', source_type='system_agent_delete', source_ref='', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=?`, current.VersionNo, actorUserID, current.ID); err != nil {
		return err
	}
	if err = writeLiveAgentPlanProductAttributeRevision(ctx, tx, current, "delete", actorUserID); err != nil {
		return err
	}
	return tx.Commit()
}
