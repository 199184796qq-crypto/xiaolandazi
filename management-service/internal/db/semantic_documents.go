package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/semantic"
)

func (s *Store) GetSemanticDocument(ctx context.Context, lookup semantic.Lookup) (semantic.StoredDocument, error) {
	var item semantic.StoredDocument
	var vectorBytes []byte
	var expiresAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, room_id, plan_id, content_type, source_id, source_version, status,
		       content_text, text_hash, embedding_model, embedding_dimensions, embedding_blob,
		       expires_at, created_at, updated_at
		FROM semantic_documents
		WHERE tenant_id=? AND room_id=? AND plan_id=? AND content_type=? AND source_id=? AND source_version=?
		  AND embedding_model=? AND text_hash=? AND status='active'
		  AND (expires_at IS NULL OR expires_at>CURRENT_TIMESTAMP(3))
		LIMIT 1
	`, lookup.TenantID, lookup.RoomID, lookup.PlanID, strings.TrimSpace(lookup.ContentType), strings.TrimSpace(lookup.SourceID),
		lookup.SourceVersion, strings.TrimSpace(lookup.Model), strings.TrimSpace(lookup.TextHash)).Scan(
		&item.ID, &item.TenantID, &item.RoomID, &item.PlanID, &item.ContentType, &item.SourceID, &item.SourceVersion,
		&item.Status, &item.Text, &item.TextHash, &item.Model, &item.Dimensions, &vectorBytes,
		&expiresAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return semantic.StoredDocument{}, semantic.ErrDocumentNotFound
	}
	if err != nil {
		return semantic.StoredDocument{}, err
	}
	if expiresAt.Valid {
		value := expiresAt.Time
		item.ExpiresAt = &value
	}
	item.Vector, err = semantic.DecodeVector(vectorBytes, item.Dimensions)
	if err != nil {
		return semantic.StoredDocument{}, err
	}
	return item, nil
}

func (s *Store) UpsertSemanticDocument(ctx context.Context, item semantic.StoredDocument) error {
	status := strings.TrimSpace(item.Status)
	if status == "" {
		status = "active"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO semantic_documents (
			tenant_id, room_id, plan_id, content_type, source_id, source_version, status,
			content_text, text_hash, embedding_model, embedding_dimensions, embedding_blob, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			room_id=VALUES(room_id),
			plan_id=VALUES(plan_id),
			status=VALUES(status),
			content_text=VALUES(content_text),
			text_hash=VALUES(text_hash),
			embedding_dimensions=VALUES(embedding_dimensions),
			embedding_blob=VALUES(embedding_blob),
			expires_at=VALUES(expires_at),
			updated_at=CURRENT_TIMESTAMP(3)
	`, item.TenantID, item.RoomID, item.PlanID, strings.TrimSpace(item.ContentType),
		strings.TrimSpace(item.SourceID), item.SourceVersion, status, strings.TrimSpace(item.Text),
		strings.TrimSpace(item.TextHash), strings.TrimSpace(item.Model), item.Dimensions,
		semantic.EncodeVector(item.Vector), item.ExpiresAt)
	return err
}

func (s *Store) ListSemanticDocuments(ctx context.Context, query semantic.Query) ([]semantic.StoredDocument, error) {
	if query.TenantID <= 0 || strings.TrimSpace(query.ContentType) == "" {
		return nil, nil
	}
	limit := query.CandidateLimit
	if limit <= 0 {
		limit = 500
	}
	if limit > 500 {
		limit = 500
	}

	builder := strings.Builder{}
	builder.WriteString(`
		SELECT id, tenant_id, room_id, plan_id, content_type, source_id, source_version, status,
		       content_text, text_hash, embedding_model, embedding_dimensions, embedding_blob,
		       expires_at, created_at, updated_at
		FROM semantic_documents
		WHERE tenant_id=? AND content_type=? AND status='active'
		  AND (expires_at IS NULL OR expires_at>CURRENT_TIMESTAMP(3))
	`)
	args := []any{query.TenantID, strings.TrimSpace(query.ContentType)}
	if model := strings.TrimSpace(query.Model); model != "" {
		builder.WriteString(" AND embedding_model=?")
		args = append(args, model)
	}
	if query.RoomID > 0 {
		builder.WriteString(" AND room_id=?")
		args = append(args, query.RoomID)
	}
	if query.PlanID > 0 {
		builder.WriteString(" AND plan_id=?")
		args = append(args, query.PlanID)
	}
	if !query.Since.IsZero() {
		builder.WriteString(" AND created_at>=?")
		args = append(args, query.Since.UTC())
	}
	builder.WriteString(" ORDER BY updated_at DESC, id DESC LIMIT ?")
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]semantic.StoredDocument, 0)
	for rows.Next() {
		var item semantic.StoredDocument
		var vectorBytes []byte
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.RoomID, &item.PlanID, &item.ContentType, &item.SourceID, &item.SourceVersion,
			&item.Status, &item.Text, &item.TextHash, &item.Model, &item.Dimensions, &vectorBytes,
			&expiresAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			item.ExpiresAt = &value
		}
		item.Vector, err = semantic.DecodeVector(vectorBytes, item.Dimensions)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// PruneSemanticDocuments deactivates vectors that no longer correspond to the
// source set for one exact room or plan. Scope values of zero are matched
// exactly, so a plan rebuild cannot touch a room's runtime vectors.
func (s *Store) PruneSemanticDocuments(ctx context.Context, scope semantic.SyncScope, keep []semantic.SourceRevision) (int64, error) {
	if scope.TenantID <= 0 || strings.TrimSpace(scope.ContentType) == "" {
		return 0, errors.New("semantic prune scope is incomplete")
	}
	if len(keep) > semantic.MaxSyncDocuments {
		return 0, fmt.Errorf("semantic prune keep set=%d exceeds %d", len(keep), semantic.MaxSyncDocuments)
	}
	var builder strings.Builder
	builder.WriteString(`UPDATE semantic_documents SET status='superseded', updated_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND room_id=? AND plan_id=? AND content_type=? AND status='active'`)
	args := []any{scope.TenantID, scope.RoomID, scope.PlanID, strings.TrimSpace(scope.ContentType)}
	if len(keep) > 0 {
		builder.WriteString(" AND NOT (")
		for index, item := range keep {
			if index > 0 {
				builder.WriteString(" OR ")
			}
			builder.WriteString("(source_id=? AND source_version=? AND embedding_model=?)")
			args = append(args, strings.TrimSpace(item.SourceID), item.SourceVersion, strings.TrimSpace(item.Model))
		}
		builder.WriteString(")")
	}
	result, err := s.db.ExecContext(ctx, builder.String(), args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) DeleteExpiredSemanticDocuments(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM semantic_documents
		WHERE expires_at IS NOT NULL AND expires_at <= ?
	`, now.UTC())
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return count, nil
}
