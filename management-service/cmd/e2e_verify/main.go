package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"

	"livecompanion/management/internal/config"
)

type check struct {
	name  string
	query string
}

func main() {
	cfg := config.Load()
	mysqlCfg := mysql.Config{
		User:      cfg.DBUser,
		Passwd:    cfg.DBPassword,
		Net:       "tcp",
		Addr:      net.JoinHostPort(cfg.DBHost, cfg.DBPort),
		DBName:    cfg.DBName,
		ParseTime: true,
		Loc:       time.UTC,
		Params: map[string]string{
			"charset": "utf8mb4",
		},
	}
	db, err := sql.Open("mysql", mysqlCfg.FormatDSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("database unavailable: %v", err)
	}

	requiredTables := []string{
		"catalog_device_products",
		"catalog_device_versions",
		"biz_orders",
		"biz_order_items",
		"biz_order_shipping",
		"biz_order_devices",
		"inv_devices",
		"inv_shipments",
		"inv_shipment_items",
		"inv_logistics_events",
	}
	for _, table := range requiredTables {
		var exists int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM information_schema.tables
			WHERE table_schema=? AND table_name=?
		`, cfg.DBName, table).Scan(&exists); err != nil {
			log.Fatalf("check table %s: %v", table, err)
		}
		if exists != 1 {
			log.Fatalf("required table missing: %s", table)
		}
		fmt.Printf("[OK] table %s\n", table)
	}

	checks := []check{
		{
			name: "paid device orders have reserved device rows",
			query: `
				SELECT COUNT(*)
				FROM biz_orders o
				WHERE o.order_type='device'
				  AND o.status IN ('paid','fulfilled')
				  AND NOT EXISTS (
				    SELECT 1 FROM biz_order_devices od WHERE od.order_id=o.id
				  )
			`,
		},
		{
			name: "device order rows are attached to shipments",
			query: `
				SELECT COUNT(*)
				FROM biz_order_devices od
				INNER JOIN biz_orders o ON o.id=od.order_id
				WHERE o.order_type='device'
				  AND o.status IN ('paid','fulfilled')
				  AND od.shipment_id IS NULL
			`,
		},
		{
			name: "shipment items match device order links",
			query: `
				SELECT COUNT(*)
				FROM biz_order_devices od
				LEFT JOIN inv_shipment_items si
				  ON si.shipment_id=od.shipment_id
				 AND si.device_id=od.device_id
				WHERE od.shipment_id IS NOT NULL
				  AND si.id IS NULL
			`,
		},
		{
			name: "delivered order shipments have delivered order devices",
			query: `
				SELECT COUNT(*)
				FROM inv_shipments sh
				INNER JOIN biz_order_devices od ON od.shipment_id=sh.id
				WHERE sh.business_type='order'
				  AND sh.status='delivered'
				  AND od.status <> 'delivered'
			`,
		},
		{
			name: "fulfilled device orders preserve customer-bind trace",
			query: `
				SELECT COUNT(*)
				FROM biz_orders o
				INNER JOIN biz_order_devices od ON od.order_id=o.id
				WHERE o.order_type='device'
				  AND o.status='fulfilled'
				  AND NOT EXISTS (
				    SELECT 1
				    FROM inv_device_ledger l
				    WHERE l.device_id=od.device_id
				      AND l.action='customer_bind'
				      AND l.to_customer_id=o.tenant_id
				  )
			`,
		},
		{
			name: "fulfilled device orders have shipping snapshots",
			query: `
				SELECT COUNT(*)
				FROM biz_orders o
				LEFT JOIN biz_order_shipping s ON s.order_id=o.id
				WHERE o.order_type='device'
				  AND o.status IN ('paid','fulfilled')
				  AND s.order_id IS NULL
			`,
		},
		{
			name: "order shipments reference existing orders",
			query: `
				SELECT COUNT(*)
				FROM inv_shipments sh
				LEFT JOIN biz_orders o
				  ON sh.business_type='order'
				 AND sh.business_id=o.id
				WHERE sh.business_type='order'
				  AND o.id IS NULL
			`,
		},
	}

	failed := false
	for _, item := range checks {
		var count int64
		if err := db.QueryRowContext(ctx, item.query).Scan(&count); err != nil {
			log.Fatalf("%s: %v", item.name, err)
		}
		if count != 0 {
			failed = true
			fmt.Printf("[FAIL] %s: %d inconsistent row(s)\n", item.name, count)
			continue
		}
		fmt.Printf("[OK] %s\n", item.name)
	}

	var (
		deviceOrders int64
		fulfilled    int64
		shipments    int64
		delivered    int64
	)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM biz_orders WHERE order_type='device'`).Scan(&deviceOrders)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM biz_orders WHERE order_type='device' AND status='fulfilled'`).Scan(&fulfilled)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inv_shipments WHERE business_type='order'`).Scan(&shipments)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inv_shipments WHERE business_type='order' AND status='delivered'`).Scan(&delivered)

	fmt.Printf(
		"[SUMMARY] device_orders=%d fulfilled=%d order_shipments=%d delivered=%d\n",
		deviceOrders,
		fulfilled,
		shipments,
		delivered,
	)

	if failed {
		os.Exit(2)
	}
}
