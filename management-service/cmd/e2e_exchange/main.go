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
	mysqlCfg := mysql.NewConfig()
	mysqlCfg.User = cfg.DBUser
	mysqlCfg.Passwd = cfg.DBPassword
	mysqlCfg.Net = "tcp"
	mysqlCfg.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	mysqlCfg.DBName = cfg.DBName
	mysqlCfg.ParseTime = true
	mysqlCfg.Loc = time.UTC
	mysqlCfg.Params = map[string]string{
		"charset": "utf8mb4",
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
		"inv_rmas",
		"inv_rma_links",
		"inv_shipments",
		"inv_shipment_items",
		"biz_orders",
		"biz_order_shipping",
		"biz_order_devices",
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
			name: "exchange RMAs have trace links",
			query: `
				SELECT COUNT(*)
				FROM inv_rmas r
				LEFT JOIN inv_rma_links l ON l.rma_id=r.id
				WHERE r.service_type='exchange'
				  AND l.id IS NULL
			`,
		},
		{
			name: "customer-side exchange RMAs preserve source order",
			query: `
				SELECT COUNT(*)
				FROM inv_rmas r
				INNER JOIN inv_rma_links l ON l.rma_id=r.id
				WHERE r.service_type='exchange'
				  AND l.source_device_status IN ('SOLD','CUSTOMER_BOUND','ACTIVE')
				  AND l.source_order_id IS NULL
			`,
		},
		{
			name: "customer-side exchange RMAs have return shipment",
			query: `
				SELECT COUNT(*)
				FROM inv_rmas r
				INNER JOIN inv_rma_links l ON l.rma_id=r.id
				WHERE r.service_type='exchange'
				  AND l.source_device_status IN ('SOLD','CUSTOMER_BOUND','ACTIVE')
				  AND l.return_shipment_id IS NULL
			`,
		},
		{
			name: "return shipment points back to same RMA",
			query: `
				SELECT COUNT(*)
				FROM inv_rma_links l
				INNER JOIN inv_rmas r ON r.id=l.rma_id
				LEFT JOIN inv_shipments sh ON sh.id=l.return_shipment_id
				WHERE r.service_type='exchange'
				  AND l.return_shipment_id IS NOT NULL
				  AND (
				    sh.id IS NULL
				    OR sh.business_type <> 'rma'
				    OR sh.business_id <> r.id
				    OR sh.shipment_type NOT IN ('return','rma_return')
				  )
			`,
		},
		{
			name: "exchange replacement device differs from source device",
			query: `
				SELECT COUNT(*)
				FROM inv_rmas r
				WHERE r.service_type='exchange'
				  AND r.replacement_device_id IS NOT NULL
				  AND r.replacement_device_id=r.device_id
			`,
		},
		{
			name: "exchange outbound shipment carries replacement device",
			query: `
				SELECT COUNT(*)
				FROM inv_rmas r
				INNER JOIN inv_rma_links l ON l.rma_id=r.id
				LEFT JOIN inv_shipment_items si
				  ON si.shipment_id=l.outbound_shipment_id
				 AND si.device_id=r.replacement_device_id
				WHERE r.service_type='exchange'
				  AND l.outbound_shipment_id IS NOT NULL
				  AND (
				    r.replacement_device_id IS NULL
				    OR si.id IS NULL
				  )
			`,
		},
		{
			name: "completed exchange has delivered outbound shipment",
			query: `
				SELECT COUNT(*)
				FROM inv_rmas r
				INNER JOIN inv_rma_links l ON l.rma_id=r.id
				LEFT JOIN inv_shipments sh ON sh.id=l.outbound_shipment_id
				WHERE r.service_type='exchange'
				  AND r.status='COMPLETED'
				  AND (
				    l.outbound_shipment_id IS NULL
				    OR sh.id IS NULL
				    OR sh.status <> 'delivered'
				  )
			`,
		},
		{
			name: "exchange source order snapshots match existing orders",
			query: `
				SELECT COUNT(*)
				FROM inv_rma_links l
				INNER JOIN inv_rmas r ON r.id=l.rma_id
				LEFT JOIN biz_orders o ON o.id=l.source_order_id
				WHERE r.service_type='exchange'
				  AND l.source_order_id IS NOT NULL
				  AND (
				    o.id IS NULL
				    OR o.order_no <> l.source_order_no
				  )
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
		total      int64
		processing int64
		completed  int64
	)
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM inv_rmas WHERE service_type='exchange'
	`).Scan(&total)
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM inv_rmas
		WHERE service_type='exchange' AND status <> 'COMPLETED'
	`).Scan(&processing)
	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM inv_rmas
		WHERE service_type='exchange' AND status='COMPLETED'
	`).Scan(&completed)

	fmt.Printf(
		"[SUMMARY] exchange_rmas=%d processing=%d completed=%d\n",
		total,
		processing,
		completed,
	)

	if failed {
		os.Exit(2)
	}
}
