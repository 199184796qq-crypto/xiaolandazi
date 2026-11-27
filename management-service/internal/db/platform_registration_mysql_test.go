package db

import (
	"strings"
	"testing"
)

func TestPlatformRegistrationOrganizationMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE mgmt_tenants SET name='伴播搭子平台' WHERE code='platform'`)
	exec(`INSERT INTO mgmt_tenants (id,parent_id,org_type,level,code,name,status) VALUES
		(10,1,'customer',1,'old-direct','Existing direct customer','active'),
		(11,1,'agent',1,'agent-fixture','代理甲','active'),
		(12,11,'customer',2,'old-agent','Existing agent customer','active')`)
	exec(`INSERT INTO crm_customer_profiles (tenant_id,source_type) VALUES (10,'direct'),(12,'agent')`)
	exec(`INSERT INTO mgmt_users (id,tenant_id,username,display_name,phone,role,status)
		VALUES (901,11,'agent_a','Test agent','test-901','agent_admin','active')`)
	for range 2 {
		if err := s.MigrateOrganizations(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// The migration changes the name on the existing organization ID.
	id, err := s.PlatformOrganizationID(ctx)
	if err != nil || id != 1 {
		t.Fatalf("platform organization id=%d err=%v", id, err)
	}
	check := func(tenant, wantParent int64, wantName, wantSource string) {
		t.Helper()
		var parentID int64
		var parentName, source string
		err := s.db.QueryRowContext(ctx, `SELECT parent.id,parent.name,profile.source_type
			FROM mgmt_tenants customer JOIN mgmt_tenants parent ON parent.id=customer.parent_id
			JOIN crm_customer_profiles profile ON profile.tenant_id=customer.id WHERE customer.id=?`, tenant).Scan(&parentID, &parentName, &source)
		if err != nil || parentID != wantParent || parentName != wantName || source != wantSource {
			t.Fatalf("tenant %d got parent=%d %q source=%q: %v", tenant, parentID, parentName, source, err)
		}
	}
	check(10, 1, "系统直册", "direct")
	check(12, 11, "代理甲", "agent")
	direct, err := s.RegisterCustomer(ctx, "new_direct", "New direct", "test-direct", "hash")
	if err != nil {
		t.Fatal(err)
	}
	check(*direct.TenantID, 1, "系统直册", "direct")

	for _, raw := range strings.Split(commercialSchema, "\n-- +statement\n") {
		q := strings.TrimSpace(raw)
		for _, table := range []string{"crm_agent_orgs", "crm_agent_members", "crm_customer_agent_relations"} {
			if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS "+table+" (") {
				exec(q)
			}
		}
	}
	// The shared sales fixture starts with a minimal invite table.
	exec(`ALTER TABLE iam_invite_codes ADD COLUMN max_uses BIGINT UNSIGNED NOT NULL DEFAULT 0,
		ADD COLUMN used_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
		ADD COLUMN expires_at DATETIME(3) NULL,
		ADD COLUMN created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		ADD COLUMN updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)`)
	if err := s.MigrateInvitations(ctx); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		inviter, parent               int64
		username, phone, name, source string
	}{
		{900, 1, "new_platform", "test-platform", "系统直册", "platform_invite"},
		{101, 1, "new_sales", "test-sales", "系统直册", "sales_invite"},
		{901, 11, "new_agent", "test-agent", "代理甲", "agent_invite"},
	} {
		var code string
		if err := s.db.QueryRowContext(ctx, `SELECT code FROM iam_invite_codes WHERE owner_user_id=?`, item.inviter).Scan(&code); err != nil {
			t.Fatal(err)
		}
		customer, err := s.RegisterCustomerByInvite(ctx, item.username, "New customer", item.phone, "", "", "", "hash", code)
		if err != nil {
			t.Fatal(err)
		}
		check(*customer.TenantID, item.parent, item.name, item.source)
	}
}
