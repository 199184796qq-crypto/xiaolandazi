// One-shot, explicitly scoped maintenance tool. Defaults to read-only dry run.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"log"
	"os"
	"time"
)

const devices = "(1,2,3,4,5)"
const products = "(1,2,3)"
const orders = "(1,2,3)"
const documents = "(SELECT document_id FROM inv_stock_document_items WHERE device_id IN (1,2,3,4,5) UNION SELECT document_id FROM inv_device_ledger WHERE device_id IN (1,2,3,4,5))"

type removal struct {
	Table string `json:"table"`
	Where string `json:"-"`
	Count int64  `json:"count"`
}

func run() error {
	apply := flag.Bool("apply", false, "Commit exactly the previously inspected three E2E products and their five devices")
	flag.Parse()
	cfg := mysql.NewConfig()
	cfg.User = os.Getenv("DB_USER")
	cfg.Passwd = os.Getenv("DB_PASSWORD")
	cfg.Net = "tcp"
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "3306"
	}
	cfg.Addr = os.Getenv("DB_HOST") + ":" + port
	cfg.DBName = os.Getenv("DB_NAME")
	cfg.ParseTime = true
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = 20 * time.Second
	cfg.WriteTimeout = 20 * time.Second
	if cfg.DBName != "livecompanion" || cfg.Passwd == "" {
		return fmt.Errorf("expected production database environment")
	}
	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	count := func(q string) (int64, error) { var n int64; err := tx.QueryRowContext(ctx, q).Scan(&n); return n, err }
	require := func(q string, want int64) error {
		n, err := count(q)
		if err != nil {
			return err
		}
		if n != want {
			return fmt.Errorf("scope guard failed: expected %d, got %d (%s)", want, n, q)
		}
		return nil
	}
	guards := []struct {
		q string
		n int64
	}{
		{`SELECT COUNT(*) FROM catalog_device_products WHERE (id=1 AND sku_code='E2E-SKU-1790061783662135947') OR (id=2 AND sku_code='E2EX-SKU-1790061882856795678') OR (id=3 AND sku_code='E2EX-SKU-1790061922526641898')`, 3},
		{`SELECT COUNT(*) FROM inv_devices WHERE (id=1 AND sn='E2E-SN-A-1790061783662135947' AND sku_code='E2E-SKU-1790061783662135947') OR (id=2 AND sn='E2EX-OLD-1790061882856795678' AND sku_code='E2EX-SKU-1790061882856795678') OR (id=3 AND sn='E2EX-NEW-1790061882856795678' AND sku_code='E2EX-SKU-1790061882856795678') OR (id=4 AND sn='E2EX-OLD-1790061922526641898' AND sku_code='E2EX-SKU-1790061922526641898') OR (id=5 AND sn='E2EX-NEW-1790061922526641898' AND sku_code='E2EX-SKU-1790061922526641898')`, 5},
		{`SELECT COUNT(*) FROM catalog_device_products WHERE id=4 AND sku_code='xldzplus'`, 1},
		{`SELECT COUNT(*) FROM inv_devices WHERE id=6 AND sn='xldz0001' AND sku_code='xldzplus'`, 1},
		{`SELECT COUNT(*) FROM inv_stock_documents WHERE id=40 AND document_no='IN-1790085519635248700'`, 1},
		{`SELECT COUNT(*) FROM inv_stock_documents WHERE id IN ` + documents, 40},
		{`SELECT COUNT(*) FROM inv_stock_document_items WHERE document_id IN ` + documents + ` AND device_id NOT IN ` + devices, 0},
		{`SELECT COUNT(*) FROM inv_device_ledger WHERE document_id IN ` + documents + ` AND device_id NOT IN ` + devices, 0},
		{`SELECT COUNT(*) FROM inv_rmas WHERE id IN (1,2,3,4) AND device_id IN ` + devices, 4},
		{`SELECT COUNT(*) FROM inv_rmas WHERE id IN (1,2,3,4) AND replacement_device_id IS NOT NULL AND replacement_device_id NOT IN ` + devices, 0},
		{`SELECT COUNT(*) FROM inv_shipment_items WHERE shipment_id IN (1,2,3,4,5,6,7,8) AND device_id NOT IN ` + devices, 0},
		{`SELECT COUNT(DISTINCT shipment_id) FROM inv_shipment_items WHERE device_id IN ` + devices, 8},
		{`SELECT COUNT(*) FROM biz_orders WHERE id IN ` + orders + ` AND order_type='device'`, 3},
		{`SELECT COUNT(*) FROM biz_order_items WHERE order_id IN ` + orders + ` AND (product_type<>'device' OR product_id NOT IN ` + products + `)`, 0},
		{`SELECT COUNT(*) FROM biz_order_devices WHERE order_id IN ` + orders + ` AND device_id NOT IN ` + devices, 0},
		{`SELECT COUNT(*) FROM biz_order_devices WHERE device_id IN ` + devices + ` AND order_id NOT IN ` + orders, 0},
		{`SELECT COUNT(*) FROM biz_order_items WHERE product_type='device' AND product_id IN ` + products + ` AND order_id NOT IN ` + orders, 0},
		// Never silently alter customer wallet balances or commission settlements.
		{`SELECT COUNT(*) FROM fin_wallet_ledger WHERE order_no IN (SELECT order_no FROM biz_orders WHERE id IN ` + orders + `) OR (business_type='order' AND business_id IN ` + orders + `)`, 0},
		{`SELECT COUNT(*) FROM inc_earnings WHERE source_order_id IN ` + orders, 0},
		{`SELECT COUNT(*) FROM fin_refund_orders WHERE source_type='order' AND source_id IN ` + orders + ` AND status<>'rejected'`, 0},
	}
	for _, g := range guards {
		if err := require(g.q, g.n); err != nil {
			return err
		}
	}
	plan := []removal{
		{Table: "fin_customer_receipt_events", Where: "receipt_id IN (SELECT id FROM fin_customer_receipts WHERE order_id IN " + orders + ")"},
		{Table: "fin_customer_confirmations", Where: "receipt_id IN (SELECT id FROM fin_customer_receipts WHERE order_id IN " + orders + ")"},
		{Table: "fin_customer_receipts", Where: "order_id IN " + orders},
		{Table: "fin_payment_transactions", Where: "order_id IN " + orders},
		{Table: "fin_refund_orders", Where: "source_type='order' AND source_id IN " + orders},
		{Table: "mkt_campaign_order_snapshots", Where: "order_id IN " + orders},
		{Table: "mkt_campaign_usage", Where: "order_id IN " + orders},
		{Table: "biz_order_shipping", Where: "order_id IN " + orders},
		{Table: "biz_order_devices", Where: "device_id IN " + devices},
		{Table: "biz_order_items", Where: "order_id IN " + orders},
		{Table: "biz_orders", Where: "id IN " + orders},
		{Table: "inv_logistics_events", Where: "shipment_id IN (1,2,3,4,5,6,7,8)"},
		{Table: "inv_shipment_items", Where: "device_id IN " + devices},
		{Table: "inv_rma_events", Where: "rma_id IN (1,2,3,4)"},
		{Table: "inv_rma_costs", Where: "rma_id IN (1,2,3,4)"},
		{Table: "inv_rma_links", Where: "rma_id IN (1,2,3,4)"},
		{Table: "inv_rmas", Where: "id IN (1,2,3,4)"},
		{Table: "inv_shipments", Where: "id IN (1,2,3,4,5,6,7,8)"},
		{Table: "inv_scrap_disposals", Where: "device_id IN " + devices},
		{Table: "device_claim_codes", Where: "device_id IN " + devices},
		{Table: "device_provisioning_events", Where: "device_id IN " + devices},
		{Table: "device_hardware_profiles", Where: "device_id IN " + devices},
		{Table: "live_runtime_events", Where: "device_id IN " + devices},
		{Table: "live_runtime_sessions", Where: "device_id IN " + devices},
		{Table: "live_device_room_bindings", Where: "device_id IN " + devices},
		{Table: "live_device_runtime_state", Where: "device_id IN " + devices},
		{Table: "fin_operating_entries", Where: "business_type='inventory_inbound' AND business_id IN " + documents},
		// Documents must be removed before their scope-defining child rows.
		{Table: "inv_stock_documents", Where: "id IN " + documents},
		{Table: "inv_stock_document_items", Where: "device_id IN " + devices},
		{Table: "inv_device_ledger", Where: "device_id IN " + devices},
		{Table: "inv_batch_registry", Where: "sku_code IN ('E2E-SKU-1790061783662135947','E2EX-SKU-1790061882856795678','E2EX-SKU-1790061922526641898')"},
		{Table: "inv_devices", Where: "id IN " + devices},
		{Table: "catalog_device_versions", Where: "product_id IN " + products},
		{Table: "catalog_device_products", Where: "id IN " + products},
	}
	for i := range plan {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`, plan[i].Table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 && plan[i].Table == "inv_batch_registry" {
			continue
		}
		n, err := count("SELECT COUNT(*) FROM " + plan[i].Table + " WHERE " + plan[i].Where)
		if err != nil {
			return err
		}
		plan[i].Count = n
	}
	if !*apply {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"mode": "dry_run", "removals": plan, "preserved": "product #4 / SN xldz0001 / document #40"})
	}
	for _, p := range plan {
		if p.Count == 0 {
			continue
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM "+p.Table+" WHERE "+p.Where)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != p.Count {
			return fmt.Errorf("concurrent changes in %s: expected %d deleted %d; rolled back", p.Table, p.Count, n)
		}
	}
	for _, g := range guards[2:5] {
		if err := require(g.q, g.n); err != nil {
			return err
		}
	}
	if err := require("SELECT COUNT(*) FROM inv_devices WHERE id IN "+devices, 0); err != nil {
		return err
	}
	if err := require("SELECT COUNT(*) FROM catalog_device_products WHERE id IN "+products, 0); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE work_inbox_revisions SET revision=revision+1 WHERE topic IN ('inventory','orders','finance')"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"mode": "committed", "removals": plan, "preserved": "product #4 / SN xldz0001 / document #40"})
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
