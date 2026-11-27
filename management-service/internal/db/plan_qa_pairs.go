package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

var ErrQAPairsConflict = errors.New("问答或对象已变化，请刷新后重新核对")
var ErrQAOperationPending = errors.New("该保存请求正在处理或结果待确认，未重复调用向量；请稍后刷新")
var ErrQAOperationUnknown = errors.New("上次向量结果待确认，旧问答保留；如重新发起保存可能再次计费")

type qaQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type QALink struct {
	ID          int64             `json:"id"`
	LinkKey     string            `json:"link_key"`
	ProductName string            `json:"product_name"`
	FieldLabels map[string]string `json:"field_labels,omitempty"`
}
type QAOperation struct {
	State          string            `json:"state"`
	Writes         []qapair.Mutation `json:"writes"`
	Remove         []string          `json:"remove"`
	Kept           int               `json:"kept"`
	Revision       string            `json:"revision"`
	ObjectRevision string            `json:"object_revision"`
	Result         qapair.Catalog    `json:"result"`
}

func qaID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

func qaObjects(ctx context.Context, q qaQueryer, tenant, plan int64) (model.MaterialObjectCatalog, []QALink, string, error) {
	return readQAObjects(ctx, q, tenant, plan, false)
}

func readQAObjects(ctx context.Context, q qaQueryer, tenant, plan int64, lock bool) (model.MaterialObjectCatalog, []QALink, string, error) {
	objects, err := readMaterialObjects(ctx, q, tenant, plan, lock)
	links := []QALink{}
	if err != nil {
		return objects, nil, "", err
	}
	query := `SELECT id,link_key,product_name,COALESCE(field_labels_json,JSON_OBJECT()) FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? ORDER BY id`
	if lock {
		query += " FOR UPDATE"
	}
	rows, err := q.QueryContext(ctx, query, tenant, plan)
	if err != nil {
		return objects, nil, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var l QALink
		var labels []byte
		if err = rows.Scan(&l.ID, &l.LinkKey, &l.ProductName, &labels); err != nil {
			return objects, nil, "", err
		}
		if len(labels) > 0 {
			_ = json.Unmarshal(labels, &l.FieldLabels)
		}
		links = append(links, l)
	}
	if err = rows.Err(); err != nil {
		return objects, nil, "", err
	}
	return objects, links, qapair.Hash([]any{objects, links}), nil
}
func (s *Store) PlanQAObjects(ctx context.Context, tenant, plan int64) (model.MaterialObjectCatalog, []QALink, string, error) {
	return qaObjects(ctx, s.db, tenant, plan)
}

func listQA(ctx context.Context, q qaQueryer, tenant, plan int64) ([]qapair.Record, error) {
	rows, err := q.QueryContext(ctx, `SELECT r.qa_id,r.revision,r.content_json,v.variant_id,v.embedding_model,v.space_version,v.embedding_json FROM live_agent_plan_qa_pairs p JOIN live_agent_plan_qa_revisions r ON r.tenant_id=p.tenant_id AND r.plan_id=p.plan_id AND r.qa_id=p.qa_id AND r.revision=p.active_revision LEFT JOIN live_agent_plan_qa_question_vectors v ON v.tenant_id=r.tenant_id AND v.plan_id=r.plan_id AND v.qa_id=r.qa_id AND v.revision=r.revision WHERE p.tenant_id=? AND p.plan_id=? AND p.deleted=0 ORDER BY p.qa_id,v.variant_id`, tenant, plan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []qapair.Record{}
	for rows.Next() {
		var id string
		var revision int64
		var body, vector []byte
		var variant sql.NullInt64
		var embeddingModel, space sql.NullString
		if err = rows.Scan(&id, &revision, &body, &variant, &embeddingModel, &space, &vector); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Entry.ID != id {
			var entry qapair.Entry
			if err = json.Unmarshal(body, &entry); err != nil {
				return nil, err
			}
			entry.ID = id
			entry.Revision = revision
			out = append(out, qapair.Record{Entry: entry})
		}
		r := &out[len(out)-1]
		if variant.Valid {
			if variant.Int64 != int64(len(r.Vectors)) || len(r.Vectors) > 0 && (embeddingModel.String != r.Model || space.String != r.Space) {
				return nil, errors.New("问答向量索引不完整")
			}
			var v []float32
			if err = json.Unmarshal(vector, &v); err != nil {
				return nil, err
			}
			r.Model = embeddingModel.String
			r.Space = space.String
			r.Vectors = append(r.Vectors, v)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, r := range out {
		if len(r.Vectors) > 0 || r.Entry.Enabled && len(r.Entry.Targets) > 0 {
			if err = ValidateMaterialVectors(r.Vectors, len(qapair.Questions(r.Entry))); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func (s *Store) PlanQAPairs(ctx context.Context, tenant, plan int64) ([]qapair.Record, error) {
	return listQA(ctx, s.db, tenant, plan)
}
func QACatalog(records []qapair.Record, objectRevision string) qapair.Catalog {
	items := []qapair.Entry{}
	for _, r := range records {
		items = append(items, r.Entry)
	}
	return qapair.Catalog{Revision: qapair.Hash(items), ObjectRevision: objectRevision, Items: items}
}

func lockQAPlan(ctx context.Context, tx *sql.Tx, tenant, plan int64) error {
	var id int64
	return tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, plan).Scan(&id)
}

// Lookup precedes optimistic preflight: a successful repeated request must be
// recoverable even after its own commit has changed the catalog revision.
func (s *Store) QAOperation(ctx context.Context, tenant, plan int64, id, hash string) (QAOperation, bool, error) {
	var op QAOperation
	var oldHash, state string
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_hash,state,data_json FROM live_agent_plan_qa_save_operations WHERE tenant_id=? AND plan_id=? AND operation_id=?`, tenant, plan, id).Scan(&oldHash, &state, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return op, false, nil
	}
	if err != nil {
		return op, false, err
	}
	if oldHash != hash {
		return op, true, ErrQAPairsConflict
	}
	if err = json.Unmarshal(raw, &op); err != nil {
		return op, true, err
	}
	op.State = state
	return op, true, nil
}

func (s *Store) BeginQAOperation(ctx context.Context, tenant, plan, actor int64, id, hash string, op QAOperation) (QAOperation, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return op, false, err
	}
	defer tx.Rollback()
	if err = lockQAPlan(ctx, tx, tenant, plan); err != nil {
		return op, false, err
	}
	var previousHash, state string
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,state,data_json FROM live_agent_plan_qa_save_operations WHERE tenant_id=? AND plan_id=? AND operation_id=?`, tenant, plan, id).Scan(&previousHash, &state, &previous)
	if err == nil {
		if previousHash != hash {
			return op, false, ErrQAPairsConflict
		}
		if err = json.Unmarshal(previous, &op); err != nil {
			return op, false, err
		}
		op.State = state
		return op, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return op, false, err
	}
	records, err := listQA(ctx, tx, tenant, plan)
	if err != nil {
		return op, false, err
	}
	_, _, objects, err := qaObjects(ctx, tx, tenant, plan)
	if err != nil {
		return op, false, err
	}
	if QACatalog(records, objects).Revision != op.Revision || objects != op.ObjectRevision {
		return op, false, ErrQAPairsConflict
	}
	for i := range op.Writes {
		m := &op.Writes[i]
		if m.Entry.ID == "" {
			m.Entry.ID, err = qaID()
			if err != nil {
				return op, false, err
			}
			m.Entry.Revision = 1
		} else {
			m.Entry.Revision++
		}
		m.Entry.ClientID = ""
	}
	op.State = "processing"
	data, _ := json.Marshal(op)
	_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_qa_save_operations(tenant_id,plan_id,operation_id,payload_hash,state,data_json,created_by_user_id) VALUES(?,?,?,?,'processing',?,?)`, tenant, plan, id, hash, string(data), actor)
	if err != nil {
		return op, false, err
	}
	if err = tx.Commit(); err != nil {
		return op, false, err
	}
	return op, true, nil
}

func (s *Store) QAOperationReady(ctx context.Context, tenant, plan int64, id string, op QAOperation) error {
	for _, m := range op.Writes {
		if len(m.Entry.Targets) > 0 && m.Entry.Enabled {
			if m.Space != qapair.SpaceVersion || m.Model == "" {
				return errors.New("向量配置不可用")
			}
			if err := ValidateMaterialVectors(m.Vectors, len(qapair.Questions(m.Entry))); err != nil {
				return err
			}
		}
	}
	op.State = "ready"
	raw, err := json.Marshal(op)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE live_agent_plan_qa_save_operations SET state='ready',data_json=? WHERE tenant_id=? AND plan_id=? AND operation_id=? AND state='processing'`, string(raw), tenant, plan, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrQAOperationPending
	}
	return err
}
func (s *Store) QAOperationUncertain(ctx context.Context, tenant, plan int64, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_agent_plan_qa_save_operations SET state='unknown' WHERE tenant_id=? AND plan_id=? AND operation_id=? AND state='processing'`, tenant, plan, id)
	return err
}

func (s *Store) CommitQAOperation(ctx context.Context, tenant, plan, actor int64, id string) (qapair.Catalog, error) {
	var empty qapair.Catalog
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	if err = lockQAPlan(ctx, tx, tenant, plan); err != nil {
		return empty, err
	}
	var op QAOperation
	var state string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT state,data_json FROM live_agent_plan_qa_save_operations WHERE tenant_id=? AND plan_id=? AND operation_id=? FOR UPDATE`, tenant, plan, id).Scan(&state, &raw)
	if err != nil {
		return empty, err
	}
	if err = json.Unmarshal(raw, &op); err != nil {
		return empty, err
	}
	if state == "done" {
		return op.Result, nil
	}
	if state != "ready" {
		return empty, ErrQAOperationPending
	}
	records, err := listQA(ctx, tx, tenant, plan)
	if err != nil {
		return empty, err
	}
	_, _, objects, err := qaObjects(ctx, tx, tenant, plan)
	if err != nil {
		return empty, err
	}
	if QACatalog(records, objects).Revision != op.Revision || objects != op.ObjectRevision {
		return empty, ErrQAPairsConflict
	}
	for _, qa := range op.Remove {
		result, e := tx.ExecContext(ctx, `UPDATE live_agent_plan_qa_pairs SET deleted=1 WHERE tenant_id=? AND plan_id=? AND qa_id=? AND deleted=0`, tenant, plan, qa)
		if e != nil {
			return empty, e
		}
		n, e := result.RowsAffected()
		if e != nil || n != 1 {
			return empty, ErrQAPairsConflict
		}
	}
	for _, m := range op.Writes {
		e := m.Entry
		data, er := json.Marshal(e)
		if er != nil {
			return empty, er
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_qa_revisions(tenant_id,plan_id,qa_id,revision,content_json,operation_id,created_by_user_id) VALUES(?,?,?,?,?,?,?)`, tenant, plan, e.ID, e.Revision, string(data), id, actor)
		if err != nil {
			return empty, err
		}
		for i, v := range m.Vectors {
			vector, _ := json.Marshal(v)
			texts := qapair.Questions(e)
			if i >= len(texts) {
				return empty, errors.New("问答向量对应关系不完整")
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_qa_question_vectors(tenant_id,plan_id,qa_id,revision,variant_id,question_hash,embedding_model,space_version,dimensions,embedding_json) VALUES(?,?,?,?,?,?,?,?,?,?)`, tenant, plan, e.ID, e.Revision, i, qapair.Hash(texts[i]), m.Model, m.Space, len(v), string(vector))
			if err != nil {
				return empty, err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_qa_pairs(tenant_id,plan_id,qa_id,active_revision,deleted) VALUES(?,?,?,?,0) ON DUPLICATE KEY UPDATE active_revision=VALUES(active_revision),deleted=0`, tenant, plan, e.ID, e.Revision)
		if err != nil {
			return empty, err
		}
	}
	records, err = listQA(ctx, tx, tenant, plan)
	if err != nil {
		return empty, err
	}
	op.State = "done"
	op.Result = QACatalog(records, objects)
	data, _ := json.Marshal(op)
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_qa_save_operations SET state='done',data_json=? WHERE tenant_id=? AND plan_id=? AND operation_id=?`, string(data), tenant, plan, id)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return op.Result, nil
}
