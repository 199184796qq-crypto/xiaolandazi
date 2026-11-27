package db

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-sql-driver/mysql"
)

type PlanAnswerAttributeVector struct {
	Hash   string
	Model  string
	Vector []float32
}

func (s *Store) PlanAnswerAttributeVectors(ctx context.Context, tenant, plan int64) (map[string]PlanAnswerAttributeVector, error) {
	out := make(map[string]PlanAnswerAttributeVector)
	rows, err := s.db.QueryContext(ctx, `SELECT attribute_name, concept_text_hash, embedding_model, dimensions, embedding_json
		FROM live_agent_plan_attribute_vectors WHERE tenant_id=? AND plan_id=?`, tenant, plan)
	if err != nil {
		var sqlErr *mysql.MySQLError
		if errors.As(err, &sqlErr) && sqlErr.Number == 1146 {
			return out, nil
		}
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var row PlanAnswerAttributeVector
		var name string
		var size int
		var data []byte
		if err := rows.Scan(&name, &row.Hash, &row.Model, &size, &data); err != nil {
			return nil, err
		}
		if json.Unmarshal(data, &row.Vector) == nil && size > 0 && len(row.Vector) == size {
			out[name] = row
		}
	}
	return out, rows.Err()
}
