package db

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestProductFieldLabelsMySQLPersistCASCopyAndAudit(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		f := strings.Fields(q)
		if len(f) > 5 && (f[5] == "live_agent_plans" || f[5] == "live_agent_plan_facts" || f[5] == "live_selling_point_wheels" || strings.HasPrefix(f[5], "live_agent_plan_product_") || strings.HasPrefix(f[5], "live_agent_plan_benefit")) {
			if _, err := s.db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.MigrateContentPools(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'字段测试',''),(5,14,'另一方案','')`); err != nil {
		t.Fatal(err)
	}
	created, err := s.AdoptLiveAgentPlanProductLink(ctx, 14, 4, 8, model.LiveAgentPlanProductLinkCandidate{LinkKey: "1号链接", ProductName: "十人餐", Spec: "10人餐", DailyPrice: "337", Quantity: "22道荤素菜", Audience: "10人"}, "")
	if err != nil || created.Saved == nil {
		t.Fatal(created, err)
	}
	p := *created.Saved
	input := model.UpdateLiveAgentPlanProductLinkInput{ExpectedVersionNo: p.VersionNo, LinkKey: p.LinkKey, ProductName: p.ProductName, Spec: p.Spec, DailyPrice: p.DailyPrice, Quantity: p.Quantity, Audience: p.Audience, FieldLabels: map[string]string{"quantity": "套餐内容", "audience": "用餐人数"}}
	updated, err := s.UpdateLiveAgentPlanProductLink(ctx, 14, 4, p.ID, 8, input)
	if err != nil || updated.VersionNo != 2 || updated.FieldLabels["quantity"] != "套餐内容" || updated.Quantity != p.Quantity {
		t.Fatal(updated, err)
	}
	if _, err = s.UpdateLiveAgentPlanProductLink(ctx, 14, 4, p.ID, 8, input); !errors.Is(err, ErrLiveAgentPlanProductLinkVersionConflict) {
		t.Fatal("stale save accepted", err)
	}
	if _, err = s.UpdateLiveAgentPlanProductLink(ctx, 15, 4, p.ID, 8, input); !errors.Is(err, ErrLiveAgentPlanProductLinkNotFound) {
		t.Fatal("cross tenant write accepted", err)
	}
	if _, err = s.UpdateLiveAgentPlanProductLink(ctx, 14, 5, p.ID, 8, input); !errors.Is(err, ErrLiveAgentPlanProductLinkNotFound) {
		t.Fatal("cross plan write accepted", err)
	}
	input.ExpectedVersionNo = updated.VersionNo
	input.FieldLabels = nil
	input.Spec = "10人套餐"
	updated, err = s.UpdateLiveAgentPlanProductLink(ctx, 14, 4, p.ID, 8, input)
	if err != nil || updated.FieldLabels["audience"] != "用餐人数" {
		t.Fatal("legacy client update cleared names", err)
	}
	list, err := s.ListLiveAgentPlanProductLinks(ctx, 14, 4)
	if err != nil || len(list) != 1 || list[0].FieldLabels["quantity"] != "套餐内容" {
		t.Fatal("reload lost names", err)
	}
	copy, err := s.CopyLiveAgentPlanProductLink(ctx, 14, 4, p.ID, 8, updated.VersionNo)
	if err != nil || copy.ID == p.ID || copy.FieldLabels["quantity"] != "套餐内容" {
		t.Fatal("copy lost names", err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE live_agent_plan_product_links SET status='disabled' WHERE id=?`, copy.ID); err != nil {
		t.Fatal(err)
	}
	readopt, err := s.AdoptLiveAgentPlanProductLink(ctx, 14, 4, 8, model.LiveAgentPlanProductLinkCandidate{LinkKey: copy.LinkKey, ProductName: copy.ProductName, Spec: copy.Spec, DailyPrice: copy.DailyPrice, Quantity: copy.Quantity, Audience: copy.Audience}, "")
	if err != nil || readopt.Saved == nil || readopt.Saved.FieldLabels["quantity"] != "套餐内容" {
		t.Fatal("readoption lost names", err)
	}
	if err = s.DeleteLiveAgentPlanProductLink(ctx, 14, 4, p.ID, 8); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT field_labels_json FROM live_agent_plan_product_link_revisions WHERE product_link_id=? AND version_no>=2 ORDER BY version_no`, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var labels map[string]string
		if json.Unmarshal([]byte(raw), &labels) != nil || labels["quantity"] != "套餐内容" {
			t.Fatal("audit lost field names")
		}
		count++
	}
	if count != 3 || rows.Err() != nil {
		t.Fatal("missing update/delete audit revisions", count, rows.Err())
	}
}
