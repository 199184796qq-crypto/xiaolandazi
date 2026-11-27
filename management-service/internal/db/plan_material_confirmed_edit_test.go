package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestMaterialConfirmedEditMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	exec := func(query string) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	exec(`CREATE TABLE live_agent_plan_product_links(id BIGINT PRIMARY KEY,tenant_id BIGINT,plan_id BIGINT,link_key VARCHAR(64),product_name TEXT,field_labels_json JSON)`)
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		statement = strings.TrimSpace(statement)
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") ||
			strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") {
			exec(statement)
		}
	}
	exec(materialSourcesDDL)
	exec(agentFactEditReceiptsDDL)
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15)`)
	exec(`INSERT INTO live_agent_plan_product_links VALUES(10,14,4,'1','original',NULL),(20,15,5,'1','unrelated',NULL)`)
	reference := func(text string) *model.MaterialSourceReference {
		sum := sha256.Sum256([]byte(text))
		return &model.MaterialSourceReference{ID: hex.EncodeToString(sum[:]), Ranges: []model.MaterialSourceRange{{Start: 0, End: len(text)}}}
	}
	before := materialInput()
	before.ScopeKind, before.StoreName, before.ProductLinkID = "product", "", 10
	before.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 10}}
	before.Value, before.Content, before.SourceSentence, before.SourceQuote = "葡萄", "葡萄", "葡萄500克", "葡萄"
	before.SourceRef = reference(before.SourceSentence)
	if err := s.SaveMaterialSource(ctx, 14, 4, before.SourceRef.ID, before.SourceSentence); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 7, []model.PlanMaterialClaimInput{before}, "", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	id, library := rows[0].ID, MaterialLibraryRevision(rows)
	_, _, objects, err := s.PlanQAObjects(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	after := before
	after.Value, after.Content, after.SourceSentence, after.SourceQuote = "青提", "青提", "青提500克", "青提"
	after.InitialSourceRef, after.SourceRef = before.SourceRef, reference(after.SourceSentence)

	commit := func(expectObjects string) error {
		return s.CommitEditedPlanMaterialClaim(ctx, 14, 4, id, after, "", nil, library, expectObjects)
	}
	exec(`UPDATE live_agent_plan_product_links SET product_name='changed' WHERE id=10`)
	if err := commit(objects); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("object change was not detected", err)
	}
	exec(`UPDATE live_agent_plan_product_links SET product_name='original' WHERE id=10`)
	exec(`CREATE TRIGGER reject_confirmed_edit BEFORE UPDATE ON live_agent_plan_material_claims FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='exercise rollback'`)
	if err := commit(objects); err == nil || !strings.Contains(err.Error(), "exercise rollback") {
		t.Fatal("expected injected persistence error", err)
	}
	if _, err := s.MaterialSource(ctx, 14, 4, after.SourceRef.ID); !errors.Is(err, ErrMaterialClaimNotFound) {
		t.Fatal("corrected source was saved despite rollback", err)
	}
	exec(`DROP TRIGGER reject_confirmed_edit`)
	result := make(chan error, 2)
	go func() { result <- commit(objects) }()
	go func() { result <- commit(objects) }()
	first, second := <-result, <-result
	if !((first == nil && errors.Is(second, ErrMaterialClaimConflict)) ||
		(second == nil && errors.Is(first, ErrMaterialClaimConflict))) {
		t.Fatal("expected one concurrent confirmation to win", first, second)
	}
	rows, err = s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Value != after.Value ||
		rows[0].SourceSentence != after.SourceSentence || rows[0].InitialSourceRef == nil ||
		rows[0].InitialSourceRef.ID != before.SourceRef.ID {
		t.Fatal(rows, err)
	}
	for _, item := range []model.PlanMaterialClaimInput{before, after} {
		if got, err := s.MaterialSource(ctx, 14, 4, item.SourceRef.ID); err != nil || got != item.SourceSentence {
			t.Fatal("source lookup differs", got, err)
		}
		if _, err := s.MaterialSource(ctx, 15, 5, item.SourceRef.ID); !errors.Is(err, ErrMaterialClaimNotFound) {
			t.Fatal("source leaked across plans", err)
		}
	}
	library = MaterialLibraryRevision(rows)
	scope := AgentFactEditScope{TenantID: 14, PlanID: 4, ActorID: 7, ConversationID: strings.Repeat("a", 32)}
	preview := strings.Repeat("b", 32)
	changed := MaterialClaimInput(rows[0])
	changed.Attribute = "商品名称"
	changed.Value, changed.Content, changed.SourceSentence, changed.SourceQuote = "阳光青提", "阳光青提", "阳光青提500克", "阳光青提"
	changed.SourceRef = reference(changed.SourceSentence)
	payload := json.RawMessage(`{"new_attribute":"商品名称","new_value":"阳光青提"}`)
	confirm := func() error {
		return s.CommitAgentFactEdit(ctx, scope, preview, payload, id, changed, "", nil, library, objects, true)
	}
	exec(`CREATE TRIGGER reject_fact_edit_receipt BEFORE INSERT ON live_agent_plan_fact_edit_receipts FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='exercise receipt rollback'`)
	if err := confirm(); err == nil || !strings.Contains(err.Error(), "exercise receipt rollback") {
		t.Fatal("expected atomic failure", err)
	}
	if got, err := s.AgentFactEditReceipt(ctx, scope, preview); got != nil || err != nil {
		t.Fatal("result exists after failure", string(got), err)
	}
	if _, err := s.MaterialSource(ctx, 14, 4, changed.SourceRef.ID); !errors.Is(err, ErrMaterialClaimNotFound) {
		t.Fatal("source escaped a receipt rollback", err)
	}
	if unchanged, err := s.ListPlanMaterialClaims(ctx, 14, 4); err != nil || len(unchanged) != 1 ||
		unchanged[0].Value != after.Value || unchanged[0].Attribute != after.Attribute {
		t.Fatal("fact escaped a receipt rollback", unchanged, err)
	}
	exec(`DROP TRIGGER reject_fact_edit_receipt`)
	go func() { result <- confirm() }()
	go func() { result <- confirm() }()
	first, second = <-result, <-result
	if !((first == nil && errors.Is(second, ErrMaterialClaimConflict)) ||
		(second == nil && errors.Is(first, ErrMaterialClaimConflict))) {
		t.Fatal("expected a single write", first, second)
	}
	receipts, err := s.AgentFactEditReceipts(ctx, scope)
	if err != nil || len(receipts) != 1 || !strings.Contains(string(receipts[0]), "阳光青提") {
		t.Fatal("missing durable result", receipts, err)
	}
	if got, err := s.AgentFactEditReceipt(ctx, scope, preview); err != nil || string(got) != string(receipts[0]) {
		t.Fatal("retry could not read original result", string(got), err)
	}
	for _, foreign := range []AgentFactEditScope{
		{TenantID: 15, PlanID: 4, ActorID: 7, ConversationID: scope.ConversationID},
		{TenantID: 14, PlanID: 5, ActorID: 7, ConversationID: scope.ConversationID},
		{TenantID: 14, PlanID: 4, ActorID: 8, ConversationID: scope.ConversationID},
		{TenantID: 14, PlanID: 4, ActorID: 7, ConversationID: strings.Repeat("c", 32)},
	} {
		if got, err := s.AgentFactEditReceipt(ctx, foreign, preview); err != nil || got != nil {
			t.Fatal("result leaked across scopes", foreign, string(got), err)
		}
	}
	if saved, err := s.ListPlanMaterialClaims(ctx, 14, 4); err != nil || len(saved) != 1 ||
		saved[0].Value != changed.Value || saved[0].Attribute != changed.Attribute ||
		saved[0].SourceSentence != changed.SourceSentence || saved[0].InitialSourceRef.ID != before.SourceRef.ID {
		t.Fatal("saved fact differs from receipt", saved, err)
	}

}
