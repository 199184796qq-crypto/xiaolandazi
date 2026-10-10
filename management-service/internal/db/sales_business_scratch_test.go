package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

var salesTestPrefixedTables = flag.Bool("sales-test-prefixed-tables", false, "Explicit opt-in: use random-prefixed isolated test tables inside the permitted schema. All SQL is allowlisted and table identifiers rewritten; never accesses business tables")
var salesScratchTables = []string{
	"mgmt_anchor_model_settings",
	"crm_customer_agent_relations", "crm_referral_relations",
	"mkt_campaign_controls", "mkt_campaign_item_options", "mkt_claim_locks", "mkt_claim_reservations", "fin_commerce_rule_heads", "fin_commerce_rule_versions", "fin_order_reward_snapshots", "mkt_campaigns", "mkt_campaign_items", "mkt_campaign_price_rules", "mkt_campaign_inventory", "mkt_campaign_order_snapshots", "mkt_campaign_placements", "mkt_campaign_scopes", "catalog_time_card_products", "catalog_time_card_versions", "catalog_device_versions", "biz_audit_events", "catalog_membership_card_discounts",
	"fin_wechat_cash_refunds",
	"fin_wechat_cash_refund_items",
	"fin_wechat_session_payers",
	"fin_wechat_oauth_states",
	"quota_buckets",
	"quota_ledger",
	"biz_memberships",
	"org_resource_ledger",
	"fin_operating_entries",
	"inv_batch_registry",
	"catalog_device_products",
	"device_hardware_profiles",
	"device_claim_codes",
	"device_name_sequences",
	"device_provisioning_events",
	"device_business_events",
	"device_addressing_preferences",
	"device_voice_addressing",
	"media_assets",
	"voice_profiles",
	"core_rooms",
	"live_device_room_bindings",
	"live_device_runtime_state",
	"live_runtime_sessions",
	"live_runtime_events",
	"live_runtime_policy_snapshots",
	"live_runtime_policy_revisions",
	"mgmt_room_deletions",
	"live_content_refresh_jobs",
	"live_content_refresh_events",
	"live_content_policies",
	"live_room_content_access",
	"live_agent_plan_room_bindings",
	"live_agent_room_plan_selections",
	"live_agent_room_plan_publications",
	"live_agent_plan_versions",
	"live_agent_plans",
	"live_agent_plan_facts",
	"live_agent_plan_product_links",
	"live_agent_plan_benefits",
	"live_agent_plan_scripts",
	"live_agent_plan_script_references",
	"live_agent_plan_terms",
	"live_policy_scopes",
	"live_agent_plan_product_attributes", "live_selling_point_wheels",
	"live_plan_content_revisions", "live_plan_content_observations", "live_plan_content_changes", "live_plan_content_sets", "live_plan_content_jobs", "live_plan_content_pool_records", "live_plan_content_tasks", "live_plan_content_task_products", "live_plan_benefit_scopes", "live_plan_benefit_scope_products",
	"inv_warehouses",
	"inv_stock_documents",
	"inv_stock_document_items",
	"inv_device_ledger",
	"inv_rma_events",
	"inv_rma_costs",
	"inv_shipment_items",
	"inv_logistics_events",
	"inv_rma_links",
	"inv_scrap_disposals",
	"crm_registration_referrals",
	"catalog_membership_plans",
	"catalog_membership_plan_versions",
	"inc_programs",
	"inc_program_versions",
	"inc_rules",
	"inc_earnings",
	"inc_settlement_items",
	"inc_settlement_batches",
	"fin_beneficiary_wallets",
	"fin_beneficiary_wallet_ledger",
	"fin_withdrawal_requests",
	"work_inbox_revisions",
	"inv_rmas",
	"inv_devices",
	"inv_shipments",
	"mgmt_system_settings",
	"staff_approval_tasks",
	"fin_customer_receipts",
	"fin_customer_confirmations",
	"fin_customer_receipt_events",
	"crm_support_tickets",
	"crm_support_ticket_events",
	"fin_recharge_orders",
	"fin_refund_orders",
	"fin_wallet_ledger",
	"fin_payment_transactions",
	"biz_orders",
	"biz_order_items",
	"biz_time_card_assets",
	"mkt_campaign_usage",
	"mgmt_tenants",
	"mgmt_users",
	"staff_employees",
	"mgmt_sessions",
	"org_resource_accounts",
	"iam_invite_codes",
	"crm_customer_profiles",
	"crm_sales_teams",
	"crm_sales_staff",
	"crm_customer_sales_assignments",
	"crm_sales_followups",
	"crm_sales_leads",
	"crm_sales_lead_activities",
	"crm_customer_handoffs",
	"crm_customer_handoff_events",
	"crm_sales_handover_batches",
	"crm_sales_handover_items",
	"fin_wallet_accounts",
}
var salesSQLToken = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
var salesSQLRelation = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|REFERENCES)\s+([A-Za-z_][A-Za-z0-9_.]*)`)
var salesSQLFirstTable = regexp.MustCompile(`(?i)^(?:UPDATE\s+|ALTER\s+TABLE\s+|CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?)([A-Za-z_][A-Za-z0-9_.]*)`)

// This test-only connector rewrites the finite fixture table allowlist before
// every execution, including prepared statements and reconnects. No raw
// business-table query is handed to MySQL. Unknown objects fail closed.
func rewriteSalesScratchSQL(q, prefix string) (string, error) {
	allowed := map[string]bool{}
	for _, table := range salesScratchTables {
		allowed[table] = true
	}
	cmd := strings.ToUpper(strings.Join(strings.Fields(q), " "))
	valid := false
	if q == `SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?` {
		return q, nil
	}
	if regexp.MustCompile(`^CREATE TRIGGER xl_pool_source_[0-6]_(INSERT|UPDATE|DELETE) AFTER (INSERT|UPDATE|DELETE) ON (live_agent_plan_facts|live_agent_plan_product_links|live_agent_plan_product_attributes|live_agent_plan_benefits|live_plan_benefit_scopes|live_plan_benefit_scope_products|live_selling_point_wheels) FOR EACH ROW INSERT INTO live_plan_content_revisions\(tenant_id,plan_id,revision\) VALUES\((NEW|OLD)\.tenant_id,(NEW|OLD)\.plan_id,1\) ON DUPLICATE KEY UPDATE revision=revision\+1$`).MatchString(q) {
		valid = true
	}
	// Only the new payment columns/index may be added, and the target is still
	// rewritten to the random test prefix. No DROP/RENAME/general ALTER support.
	if strings.HasPrefix(cmd, "ALTER TABLE FIN_PAYMENT_TRANSACTIONS ADD COLUMN ") || strings.HasPrefix(cmd, "ALTER TABLE FIN_PAYMENT_TRANSACTIONS ADD UNIQUE KEY ") {
		valid = true
	}
	for _, start := range []string{"SELECT ", "INSERT ", "UPDATE ", "DELETE ", "CREATE TABLE "} {
		if strings.HasPrefix(cmd, start) {
			valid = true
		}
	}
	if !valid || strings.Contains(q, ";") {
		return "", errors.New("scratch SQL operation denied")
	}
	for _, re := range []*regexp.Regexp{salesSQLRelation, salesSQLFirstTable} {
		for _, m := range re.FindAllStringSubmatch(strings.TrimSpace(q), -1) {
			if !allowed[strings.ToLower(m[1])] {
				return "", fmt.Errorf("scratch SQL object not allowlisted: %s", m[1])
			}
		}
	}
	return salesSQLToken.ReplaceAllStringFunc(q, func(token string) string {
		if allowed[strings.ToLower(token)] || strings.HasPrefix(strings.ToLower(token), "fk_") || strings.HasPrefix(token, "xl_pool_source_") {
			return prefix + token
		}
		return token
	}), nil
}

type salesScratchConnector struct {
	driver.Connector
	prefix string
}

func (c *salesScratchConnector) Connect(ctx context.Context) (driver.Conn, error) {
	base, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &salesScratchConn{Conn: base, prefix: c.prefix}, nil
}

type salesScratchConn struct {
	driver.Conn
	prefix string
}

func (c *salesScratchConn) Prepare(q string) (driver.Stmt, error) {
	r, err := rewriteSalesScratchSQL(q, c.prefix)
	if err != nil {
		return nil, err
	}
	return c.Conn.Prepare(r)
}
func (c *salesScratchConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	r, err := rewriteSalesScratchSQL(q, c.prefix)
	if err != nil {
		return nil, err
	}
	if e, ok := c.Conn.(driver.ExecerContext); ok {
		return e.ExecContext(ctx, r, args)
	}
	return nil, driver.ErrSkip
}
func (c *salesScratchConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if q == `SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?` {
		args = append([]driver.NamedValue(nil), args...)
		if len(args) != 1 {
			return nil, errors.New("scratch trigger lookup denied")
		}
		name, ok := args[0].Value.(string)
		if !ok || !regexp.MustCompile(`^xl_pool_source_[0-6]_(INSERT|UPDATE|DELETE)$`).MatchString(name) {
			return nil, errors.New("scratch trigger name denied")
		}
		q = strings.Replace(q, "?", "'"+c.prefix+name+"'", 1)
		if e, ok := c.Conn.(driver.QueryerContext); ok {
			return e.QueryContext(ctx, q, nil)
		}
		return nil, errors.New("scratch trigger query requires direct driver")
	}
	r, err := rewriteSalesScratchSQL(q, c.prefix)
	if err != nil {
		return nil, err
	}
	if e, ok := c.Conn.(driver.QueryerContext); ok {
		return e.QueryContext(ctx, r, args)
	}
	return nil, driver.ErrSkip
}
func (c *salesScratchConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

func salesPrefixedDatabase(t *testing.T, cfg *mysql.Config) *sql.DB {
	t.Helper()
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	prefix := "sbt_" + hex.EncodeToString(raw) + "_"
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal("create scratch connector failed")
	}
	database := sql.OpenDB(&salesScratchConnector{Connector: connector, prefix: prefix})
	t.Cleanup(func() {
		database.Close()
		cleanup, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Errorf("scratch cleanup connection failed")
			return
		}
		defer cleanup.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		// Reverse dependency order, restricted to the exact generated prefix.
		// New content snapshot children are cleaned first; no business FK setting changes.
		poolChildren := []string{"live_plan_benefit_scope_products", "live_plan_benefit_scopes", "live_plan_content_task_products", "live_plan_content_tasks", "live_plan_content_pool_records"}
		for _, table := range poolChildren {
			if _, err := cleanup.ExecContext(ctx, "DROP TABLE IF EXISTS `"+prefix+table+"`"); err != nil {
				t.Errorf("scratch child cleanup failed: %v", err)
			}
		}
		for i := len(salesScratchTables) - 1; i >= 0; i-- {
			name := prefix + salesScratchTables[i]
			if _, err := cleanup.ExecContext(ctx, "DROP TABLE IF EXISTS `"+name+"`"); err != nil {
				t.Errorf("scratch cleanup %s failed: %v", name, err)
			}
		}
		t.Log("isolated random-prefixed test tables cleaned; no business tables used")
	})
	return database
}

func TestSalesScratchSQLGuard(t *testing.T) {
	prefix := "sbt_test_"
	for _, q := range []string{"SELECT * FROM unknown_table", "UPDATE mgmt_users SET status='active'; DELETE FROM mgmt_users", "SELECT * FROM other.mgmt_users", "DROP TABLE mgmt_users", "ALTER TABLE mgmt_users ADD COLUMN wrong INT", "ALTER TABLE fin_payment_transactions DROP COLUMN status", "ALTER TABLE fin_payment_transactions RENAME TO other_table"} {
		if _, err := rewriteSalesScratchSQL(q, prefix); err == nil {
			t.Fatalf("unsafe SQL accepted: %s", q)
		}
	}
	out, err := rewriteSalesScratchSQL("SELECT u.id FROM mgmt_users u JOIN crm_sales_staff ss ON u.id=ss.user_id", prefix)
	if err != nil || !strings.Contains(out, "sbt_test_mgmt_users") || !strings.Contains(out, "sbt_test_crm_sales_staff") {
		t.Fatal("table rewriting failed", out, err)
	}
}

func TestDeviceScratchSQLGuard(t *testing.T) {
	for _, schema := range []string{inventorySchema, deviceProvisioningSchema} {
		for _, q := range strings.Split(strings.ReplaceAll(schema, "\r\n", "\n"), "\n-- +statement\n") {
			out, err := rewriteSalesScratchSQL(strings.TrimSpace(q), "sbt_guard_")
			if err != nil {
				t.Fatal(err)
			}
			m := salesSQLFirstTable.FindStringSubmatch(strings.TrimSpace(out))
			if len(m) != 2 || !strings.HasPrefix(m[1], "sbt_guard_") {
				t.Fatal("unrewritten device fixture", out)
			}
		}
	}
	q := `SELECT s.id FROM live_runtime_sessions s WHERE s.room_id IN (SELECT room_id FROM live_device_room_bindings WHERE device_id=?)`
	out, err := rewriteSalesScratchSQL(q, "sbt_guard_")
	if err != nil || !strings.Contains(out, "FROM sbt_guard_live_runtime_sessions") || !strings.Contains(out, "FROM sbt_guard_live_device_room_bindings") {
		t.Fatal(out, err)
	}
}
