package db

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestSharedMaterialSentenceMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires isolated schema")
	}
	store := salesIsolatedMySQL(t)
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)")
	exec("CREATE TABLE live_agent_plan_product_links(id BIGINT PRIMARY KEY,tenant_id BIGINT,plan_id BIGINT,link_key VARCHAR(64),product_name TEXT,field_labels_json JSON)")
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		statement = strings.TrimSpace(statement)
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") ||
			strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") {
			exec(statement)
		}
	}
	exec(materialSourcesDDL)
	exec(agentFactEditReceiptsDDL)
	exec("INSERT INTO live_agent_plans VALUES(4,14),(5,15)")
	before := "品名葡萄，南充7店，支持到店自选。"
	original := before + "\n" + before
	hash := func(text string) string {
		return fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	}
	reference := func(doc string, start, end int) *model.MaterialSourceReference {
		return &model.MaterialSourceReference{ID: hash(doc), Ranges: []model.MaterialSourceRange{{Start: start, End: end}}}
	}
	originRef := reference(original, 0, len(before))
	makeClaim := func(key, value string, ref *model.MaterialSourceReference) model.PlanMaterialClaimInput {
		return model.PlanMaterialClaimInput{MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", SourceRef: ref},
			ClaimKey: key, Attribute: key, Topic: key, Value: value, Content: value, SourceSentence: before, SourceQuote: value,
			Polarity: "positive", FactType: "positive", ScopeKind: "plan"}
	}

	seed := []model.PlanMaterialClaimInput{
		makeClaim("商品名称", "葡萄", originRef), makeClaim("门店数量", "南充7店", originRef),
		makeClaim("使用方式", "到店自选", originRef),
		makeClaim("其他资料的门店", "南充7店", reference(original, len(before)+1, len(original))),
	}
	for _, scope := range [][2]int64{{14, 4}, {15, 5}} {
		if err := store.SaveMaterialSource(ctx, scope[0], scope[1], originRef.ID, original); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReplacePlanMaterialClaims(ctx, scope[0], scope[1], 8, seed, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	list := func(tenant, plan int64) map[string]model.PlanMaterialClaim {
		t.Helper()
		rows, err := store.ListPlanMaterialClaims(ctx, tenant, plan)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]model.PlanMaterialClaim{}
		for _, row := range rows {
			out[row.Attribute] = row
		}
		return out
	}
	first := list(14, 4)
	target := first["商品名称"]
	input := MaterialClaimInput(target)
	input.Value, input.Content, input.SourceQuote = "青提", "青提", "青提"
	input.SourceSentence = "品名青提，南充5店，支持到店自选。"
	input.InitialSourceRef = originRef
	input.SourceRef = reference(input.SourceSentence, 0, len(input.SourceSentence))
	commit := func() error {
		return store.UpdatePlanMaterialClaimAtRevision(ctx, 14, 4, target.ID, input, "", nil, MaterialClaimRevision(target), true)
	}
	exec(`CREATE TRIGGER reject_shared_update BEFORE UPDATE ON live_agent_plan_material_claims
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='shared rollback'`)
	if err := commit(); err == nil || !strings.Contains(err.Error(), "shared rollback") {
		t.Fatalf("expected transaction failure, got %v", err)
	}
	if _, err := store.MaterialSource(ctx, 14, 4, input.SourceRef.ID); !errors.Is(err, ErrMaterialClaimNotFound) {
		t.Fatal("source survived rollback", err)
	}
	if got := list(14, 4)["使用方式"]; got.SourceSentence != before {
		t.Fatal(got)
	}
	exec("DROP TRIGGER reject_shared_update")
	if err := commit(); err != nil {
		t.Fatal(err)
	}
	if err := commit(); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatalf("stale manual edit got %v", err)
	}
	after := list(14, 4)
	for _, key := range []string{"商品名称", "门店数量", "使用方式"} {
		item := after[key]
		if item.SourceSentence != input.SourceSentence || !SameMaterialSource(item.SourceRef, input.SourceRef) ||
			item.SharedSourceKey == "" || !SameMaterialSource(item.InitialSourceRef, originRef) {
			t.Fatal(key, item)
		}
	}
	if got := after["门店数量"]; got.Value != "南充7店" || !got.SharedSourceNeedsReview || !got.NeedsConfirmation {
		t.Fatal(got)
	}
	if got := after["使用方式"]; got.Value != "到店自选" || got.SharedSourceNeedsReview || got.SourceQuote != got.Value {
		t.Fatal(got)
	}
	if after["其他资料的门店"].SourceSentence != before || list(15, 5)["商品名称"].SourceSentence != before {
		t.Fatal("separate range or tenant was updated")
	}
	records, err := store.PlanMaterialRecallSnapshot(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Claim.Attribute == "门店数量" {
			t.Fatal("unreviewed value leaked into fact answer")
		}
	}
	claims, _, _, err := store.PlanRewriteFactsSnapshot(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	_, _, objectRevision, err := store.PlanQAObjects(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	update := MaterialClaimInput(after["门店数量"])
	update.SourceSentence = "品名青提，南充6店，支持到店自选。"
	update.Content, update.Value, update.SourceQuote = "南充6店", "南充6店", "南充6店"
	update.SourceRef = reference(update.SourceSentence, 0, len(update.SourceSentence))
	scope := AgentFactEditScope{TenantID: 14, PlanID: 4, ActorID: 8, ConversationID: strings.Repeat("a", 32)}
	asserted := json.RawMessage(`{"ok":true}`)
	if err := store.CommitAgentFactEdit(ctx, scope, strings.Repeat("b", 32), asserted, after["门店数量"].ID, update, "", nil,
		MaterialLibraryRevision(claims), objectRevision, true); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"商品名称", "门店数量", "使用方式"} {
		item := list(14, 4)[key]
		if !SameMaterialSource(item.SourceRef, update.SourceRef) || item.SourceSentence != update.SourceSentence || item.SharedSourceNeedsReview {
			t.Fatal(key, item)
		}
	}
}

func TestProjectSharedMaterialSentenceHistory(t *testing.T) {
	old := "同城配送，支持退货。"
	corrected := "本地自取，支持退货。"
	conflicting := "快递发货，支持退货。"
	ref := func(text string) *model.MaterialSourceReference {
		return &model.MaterialSourceReference{ID: fmt.Sprintf("%x", sha256.Sum256([]byte(text))),
			Ranges: []model.MaterialSourceRange{{Start: 0, End: len(text)}}}
	}
	origin := ref(old)
	sibling := model.PlanMaterialClaim{MaterialClaimDetails: model.MaterialClaimDetails{SourceRef: origin},
		SourceSentence: old, SourceQuote: "支持退货", Value: "支持退货", Content: "支持退货"}
	revision := func(text string) model.PlanMaterialClaim {
		item := sibling
		item.SourceSentence, item.SourceRef, item.InitialSourceRef = text, ref(text), origin
		return item
	}
	rows := []model.PlanMaterialClaim{sibling, revision(corrected)}
	projectSharedMaterialSentences(rows)
	if rows[0].SourceSentence != corrected || rows[0].NeedsConfirmation || !SameMaterialSource(rows[0].SourceRef, rows[1].SourceRef) {
		t.Fatal(rows[0])
	}
	rows = []model.PlanMaterialClaim{sibling, revision(corrected), revision(conflicting)}
	projectSharedMaterialSentences(rows)
	for _, item := range rows {
		if !item.SharedSourceNeedsReview || item.SourceIssue != sharedSourceConflict {
			t.Fatal(item)
		}
	}
}
