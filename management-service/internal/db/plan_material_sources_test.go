package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestMaterialSourceImportMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(query string) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	exec(materialSourcesDDL)
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15)`)
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		statement = strings.TrimSpace(statement)
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") || strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") {
			exec(statement)
		}
	}
	original := "设备甲，额定电压220V。"
	sourceID := strings.Repeat("a", 64)
	if err := s.SaveMaterialSource(ctx, 14, 4, sourceID, original); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMaterialSource(ctx, 14, 4, sourceID, "不能改写快照"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.MaterialSource(ctx, 14, 4, sourceID); err != nil || got != original {
		t.Fatal(got, err)
	}
	if _, err := s.MaterialSource(ctx, 15, 5, sourceID); !errors.Is(err, ErrMaterialClaimNotFound) {
		t.Fatal(err)
	}
	item := materialInput()
	item.Content, item.Value, item.SourceSentence = "额定电压220V", "220V", original
	item.SourceRef = &model.MaterialSourceReference{ID: sourceID, Ranges: []model.MaterialSourceRange{{Start: 0, End: len(original)}}}
	rev := MaterialLibraryRevision(nil)
	if n, err := s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, rev, 0, []model.PlanMaterialClaimInput{item}, nil, "", nil); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	rows, err := s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(rows) != 1 || rows[0].SourceRef == nil || rows[0].SourceRef.ID != sourceID {
		t.Fatal(rows, err)
	}
	var noVector bool
	if err := s.db.QueryRowContext(ctx, "SELECT embedding_json IS NULL FROM live_agent_plan_material_claims WHERE id=?", rows[0].ID).Scan(&noVector); err != nil || !noVector {
		t.Fatal(noVector, err)
	}
	if _, err := s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, rev, 0, []model.PlanMaterialClaimInput{item}, nil, "", nil); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal(err)
	}
}
