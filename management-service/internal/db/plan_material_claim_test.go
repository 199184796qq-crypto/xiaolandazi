package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/semantic"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

type materialConn struct {
	items                                []model.PlanMaterialClaim
	writes                               [][]driver.NamedValue
	committed, rolled, began, failInsert bool
}
type materialConnector struct{ c *materialConn }

func (c materialConnector) Connect(context.Context) (driver.Conn, error) { return c.c, nil }
func (materialConnector) Driver() driver.Driver                          { return materialDriver{} }

type materialDriver struct{}

func (materialDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (*materialConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*materialConn) Close() error                { return nil }
func (c *materialConn) Begin() (driver.Tx, error) { c.began = true; return materialTx{c}, nil }

type materialTx struct{ c *materialConn }

func (t materialTx) Commit() error   { t.c.committed = true; return nil }
func (t materialTx) Rollback() error { t.c.rolled = true; return nil }

type materialRows struct {
	values  [][]driver.Value
	columns []string
}

func (r *materialRows) Columns() []string { return r.columns }
func (*materialRows) Close() error        { return nil }
func (r *materialRows) Next(dst []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dst, r.values[0])
	r.values = r.values[1:]
	return nil
}
func (c *materialConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) < 2 || args[0].Value != int64(14) || args[1].Value != int64(4) {
		return &materialRows{columns: []string{"id"}}, nil
	}
	if strings.Contains(q, "FOR UPDATE") {
		return &materialRows{columns: []string{"id"}, values: [][]driver.Value{{int64(4)}}}, nil
	}
	if !strings.Contains(q, "details_json") || strings.Contains(q, "fact_value") {
		return nil, errors.New("wrong claim read schema")
	}
	out := &materialRows{columns: make([]string, 22)}
	for _, it := range c.items {
		details, _ := json.Marshal(it.MaterialClaimDetails)
		out.values = append(out.values, []driver.Value{it.ID, int64(14), int64(4), it.ClaimKey, it.Topic, it.Attribute, it.Value, it.Content, it.Polarity, it.FactType, []byte("[]"), it.SourceQuote, it.SourceSentence, it.ScopeKind, it.StoreName, it.ProductLinkID, "qwen", "active", int64(8), time.Now(), time.Now(), details})
	}
	return out, nil
}
func (c *materialConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(q, "live_agent_plan_facts ") || strings.Contains(q, "fact_value") {
		return nil, errors.New("formal fact mutation")
	}
	if strings.Count(q, "?") != len(args) {
		return nil, errors.New("placeholder mismatch")
	}
	if c.failInsert && len(c.writes) == 1 {
		return nil, errors.New("simulated second write failure")
	}
	c.writes = append(c.writes, args)
	return driver.RowsAffected(1), nil
}
func materialStore(t *testing.T) (*Store, *materialConn) {
	t.Helper()
	c := &materialConn{}
	db := sql.OpenDB(materialConnector{c})
	t.Cleanup(func() { db.Close() })
	return &Store{db: db}, c
}
func materialInput() model.PlanMaterialClaimInput {
	return model.PlanMaterialClaimInput{ClaimKey: "invoice", Topic: "发票", Content: "支持", Value: "支持", Polarity: "positive", FactType: "positive", ScopeKind: "store", StoreName: "南充总店", Conditions: json.RawMessage(`[]`), MaterialClaimDetails: model.MaterialClaimDetails{Destination: "qa", Question: "能开发票吗", Answer: "支持", Exceptions: json.RawMessage(`[{"raw":"部分商品除外"}]`)}}
}
func TestMaterialTransactionRetainsDetailsAndStore(t *testing.T) {
	s, c := materialStore(t)
	it := materialInput()
	n, err := s.ReplacePlanMaterialClaims(context.Background(), 14, 4, 8, []model.PlanMaterialClaimInput{it}, "qwen", [][]float32{{1, 2}})
	if err != nil || n != 1 || !c.committed {
		t.Fatalf("%d %v %+v", n, err, c)
	}
	args := c.writes[0]
	if args[14].Value != "南充总店" || !strings.Contains(args[10].Value.(string), "能开发票吗") {
		t.Fatal("lost metadata or store")
	}
}
func TestMaterialAtomicFailureAndForeignReplacement(t *testing.T) {
	s, c := materialStore(t)
	c.failInsert = true
	a := materialInput()
	b := a
	b.Content = "另一条"
	n, err := s.ReplacePlanMaterialClaims(context.Background(), 14, 4, 8, []model.PlanMaterialClaimInput{a, b}, "qwen", [][]float32{{1, 2}, {2, 3}})
	if err == nil || n != 0 || c.committed || !c.rolled {
		t.Fatal("partial batch committed")
	}
	s, c = materialStore(t)
	a.ReplacesID = 700
	n, err = s.ReplacePlanMaterialClaims(context.Background(), 14, 4, 8, []model.PlanMaterialClaimInput{a}, "qwen", [][]float32{{1, 2}})
	if !errors.Is(err, ErrMaterialClaimConflict) || n != 0 || len(c.writes) != 0 || !c.rolled {
		t.Fatal("foreign/missing replacement accepted")
	}
}
func TestMaterialDuplicateConfirmationSkipsWrite(t *testing.T) {
	s, c := materialStore(t)
	it := materialInput()
	c.items = []model.PlanMaterialClaim{{ID: 7, MaterialClaimDetails: it.MaterialClaimDetails, ClaimKey: it.ClaimKey, Topic: it.Topic, Content: it.Content, Value: it.Value, Polarity: it.Polarity, ScopeKind: it.ScopeKind, StoreName: it.StoreName, Conditions: it.Conditions}}
	n, err := s.ReplacePlanMaterialClaims(context.Background(), 14, 4, 8, []model.PlanMaterialClaimInput{it}, "qwen", [][]float32{{1, 2}})
	if err != nil || n != 0 || len(c.writes) != 0 {
		t.Fatalf("duplicate: n=%d err=%v", n, err)
	}
}
func TestMaterialRejectsBadVectorsBeforeTransaction(t *testing.T) {
	for _, vectors := range [][][]float32{nil, {{}}, {{0, 0}}, {{float32(math.NaN())}}, {{float32(math.Inf(1))}}} {
		s, c := materialStore(t)
		_, err := s.ReplacePlanMaterialClaims(context.Background(), 14, 4, 8, []model.PlanMaterialClaimInput{materialInput()}, "qwen", vectors)
		if err == nil || c.began {
			t.Fatal("bad embedding reached transaction")
		}
	}
}
func TestMaterialListPreservesLegacyAndNamedRows(t *testing.T) {
	s, c := materialStore(t)
	it := materialInput()
	c.items = []model.PlanMaterialClaim{{ID: 7, MaterialClaimDetails: it.MaterialClaimDetails, StoreName: "南充总店"}}
	rows, err := s.ListPlanMaterialClaims(context.Background(), 14, 4)
	if err != nil || len(rows) != 1 || rows[0].Question != "能开发票吗" || rows[0].StoreName != "南充总店" {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestMaterialClaimsMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("material validation requires a fully isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") {
			exec(statement)
		}
	}
	exec(`INSERT INTO live_agent_plans(id,tenant_id) VALUES(4,14),(5,15)`)
	a := materialInput()
	a.SourceQuote = "南充总店支持开发票，部分商品除外"
	b := a
	b.ScopeKind = "voucher"
	b.StoreName = ""
	b.ScopeName = "双人餐"
	b.Question = "双人餐能开发票吗"
	vectors := [][]float32{{1, 2}, {3, 4}}
	modelName := "fixture"
	if os.Getenv("MATERIAL_TEST_REAL_QWEN") == "1" {
		e := semantic.NewFromEnv()
		if !e.Enabled() {
			t.Fatal("configured Qwen embedder is disabled")
		}
		var err error
		vectors, err = e.Embed(ctx, []string{"南充总店 开发票 支持 部分商品除外", "双人餐 开发票 支持 部分商品除外"})
		if err != nil {
			t.Fatal("configured Qwen request failed")
		}
		modelName = e.Model()
		if err := ValidateMaterialVectors(vectors, 2); err != nil {
			t.Fatal(err)
		}
		t.Logf("real_vector_model=%s texts=2 dimensions=%d requests=%d input_tokens=%d", modelName, len(vectors[0]), e.Stats().Requests, e.Stats().InputTokens)
	}
	if file := os.Getenv("MATERIAL_TEST_VECTOR_FILE"); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			Model   string      `json:"model"`
			Vectors [][]float32 `json:"vectors"`
		}
		if json.Unmarshal(raw, &data) != nil {
			t.Fatal("invalid actual vector fixture")
		}
		vectors = data.Vectors
		modelName = data.Model
		if err := ValidateMaterialVectors(vectors, 2); err != nil {
			t.Fatal(err)
		}
		t.Logf("actual provider vectors used for SQL test: model=%s dimension=%d", modelName, len(vectors[0]))
	}

	n, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{a, b}, modelName, vectors)
	if err != nil || n != 2 {
		t.Fatalf("insert %d: %v", n, err)
	}
	rows, err := s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(rows) != 2 {
		t.Fatalf("read rows: %v", err)
	}
	if rows[0].ScopeName != "双人餐" || rows[1].StoreName != "南充总店" || rows[1].Question != a.Question || !strings.Contains(string(rows[1].Exceptions), "部分商品除外") {
		t.Fatal("scope/question/exception metadata changed")
	}
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT embedding_json FROM live_agent_plan_material_claims WHERE id=?`, rows[0].ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored []float32
	if json.Unmarshal(raw, &stored) != nil || len(stored) != len(vectors[0]) || stored[0] != vectors[1][0] {
		t.Fatal("vector not persisted exactly")
	}
	n, err = s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{a}, modelName, vectors[:1])
	if err != nil || n != 0 {
		t.Fatal("duplicate persisted")
	}
	a.Content = "可开发票"
	a.ReplacesID = rows[1].ID
	bad := b
	bad.Content = "foreign"
	bad.ReplacesID = 99999
	if _, err = s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{a, bad}, modelName, vectors); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("missing replacement accepted")
	}
	rowsAfter, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(rowsAfter) != 2 || rowsAfter[1].Content != rows[1].Content {
		t.Fatal("failed batch partially committed")
	}
	if err = s.UpdatePlanMaterialClaim(ctx, 15, 5, rows[0].ID, a, modelName, raw); !errors.Is(err, ErrMaterialClaimNotFound) {
		t.Fatal("cross tenant update accepted")
	}
	if err = s.DeletePlanMaterialClaim(ctx, 15, 5, rows[0].ID); !errors.Is(err, ErrMaterialClaimNotFound) {
		t.Fatal("cross tenant delete accepted")
	}
	n, err = s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{a}, modelName, vectors[:1])
	if err != nil || n != 1 {
		t.Fatalf("replace %d %v", n, err)
	}
	t.Log("isolated_sql: metadata/vector roundtrip, duplicate, atomic rollback, explicit replacement and tenant isolation passed; temporary schema cleanup registered")
}

func TestMaterialQwenConfigured(t *testing.T) {
	if os.Getenv("MATERIAL_TEST_REAL_QWEN") != "1" {
		t.Skip("explicit opt-in only")
	}
	e := semantic.NewFromEnv()
	if !e.Enabled() {
		t.Fatal("configured Qwen embedder disabled")
	}
	vectors, err := e.Embed(context.Background(), []string{"南充总店 开发票 支持 部分商品除外", "双人餐 开发票 支持 部分商品除外"})
	if err != nil {
		t.Fatal("configured Qwen request failed")
	}
	if err := ValidateMaterialVectors(vectors, 2); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"model": e.Model(), "vectors": vectors})
	if err := os.WriteFile(os.Getenv("MATERIAL_TEST_VECTOR_FILE"), raw, 0600); err != nil {
		t.Fatal("write fixture vectors failed")
	}
	t.Logf("real_vector_model=%s texts=2 dimensions=%d requests=%d input_tokens=%d", e.Model(), len(vectors[0]), e.Stats().Requests, e.Stats().InputTokens)
}
