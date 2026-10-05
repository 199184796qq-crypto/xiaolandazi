package db

import (
	"context"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInventoryPaging(t *testing.T) {
	for _, tt := range []struct{ page, size, total, p, s int }{{0, 0, 0, 1, 20}, {100, 12, 25, 3, 12}, {1, 500, 1, 1, 100}, {2, 20, 45, 2, 20}} {
		p, s := inventoryPaging(tt.page, tt.size, tt.total)
		if p != tt.p || s != tt.s {
			t.Fatal(tt, p, s)
		}
	}
}

// Uses random-prefixed fixture tables only, never real inventory.
func TestInventoryStockMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range strings.Split(strings.ReplaceAll(inventorySchema, "\r\n", "\n"), "\n-- +statement\n") {
		exec(strings.TrimSpace(raw))
	}
	exec(strings.TrimSpace(strings.Split(strings.ReplaceAll(operatingFinanceSchema, "\r\n", "\n"), "\n-- +statement\n")[0]))
	exec(`CREATE TABLE catalog_device_products(id BIGINT UNSIGNED PRIMARY KEY,code VARCHAR(64),sku_code VARCHAR(96) UNIQUE,name VARCHAR(160),status VARCHAR(32))`)
	if err := s.MigrateDeviceProvisioning(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO inv_warehouses(id,code,name,status) VALUES(1,'HQ','总部主仓','active')`)
	for i := 1; i <= 25; i++ {
		exec(`INSERT INTO catalog_device_products(id,code,sku_code,name,status) VALUES(?,?,?,?, 'active')`, i, fmt.Sprintf("P%d", i), fmt.Sprintf("SKU%d", i), fmt.Sprintf("产品%d", i))
	}
	for i := 1; i <= 45; i++ {
		exec(`INSERT INTO inv_devices(sn,sku_code,batch_no,custody_warehouse_id,lifecycle_status,quality_status) VALUES(?,'SKU1','old',1,'IN_STOCK','qualified')`, fmt.Sprintf("SN%03d", i))
	}
	exec(`UPDATE inv_devices SET lifecycle_status='ACTIVE' WHERE id=1`)
	exec(`UPDATE inv_devices SET quality_status='defective' WHERE id=2`)
	exec(`INSERT INTO device_hardware_profiles(device_id,hardware_mac) VALUES(3,'02:00:00:00:00:03')`)
	products, total, page, size, err := s.ListInventoryStockProducts(ctx, "", 2, 12)
	if err != nil || total != 25 || page != 2 || size != 12 || len(products) != 12 {
		t.Fatal(products, total, page, size, err)
	}
	products, total, _, _, err = s.ListInventoryStockProducts(ctx, "SN003", 1, 12)
	if err != nil || total != 1 || len(products) != 1 || products[0].Total != 45 || products[0].Available != 43 || products[0].Delivered != 1 {
		t.Fatal(products, total, err)
	}
	products, total, _, _, err = s.ListInventoryStockProducts(ctx, "02:00:00:00:00:03", 1, 12)
	if err != nil || total != 1 || len(products) != 1 {
		t.Fatal(products, total, err)
	}
	devices, total, page, _, err := s.ListInventoryDevicesPage(ctx, "SKU1", "", "IN_STOCK", 3, 20)
	if err != nil || total != 44 || page != 3 || len(devices) != 4 {
		t.Fatal(devices, total, page, err)
	}
	devices, total, page, _, err = s.ListInventoryDevicesPage(ctx, "SKU1", "SN003", "all", 99, 20)
	if err != nil || total != 1 || page != 1 || len(devices) != 1 || devices[0].HardwareMAC != "02:00:00:00:00:03" {
		t.Fatal(devices, total, page, err)
	}
	if exists, err := s.InventoryBatchExists(ctx, "SKU1", "OLD"); err != nil || !exists {
		t.Fatal(exists, err)
	}
	// Concurrent purchase requests must create exactly one document and charge.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateBatchInbound(ctx, 101, model.BatchInboundInput{ProductID: 1, WarehouseID: 1, BatchPrefix: "20261002", BatchSuffix: "01", SNs: []string{fmt.Sprintf("NEW%d", i)}, ExpectedQuantity: 1, PurchaseAmountCents: 100, QualityStatus: "qualified"})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	success, duplicate := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrInventoryBatchExists) {
			duplicate++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || duplicate != 1 {
		t.Fatal(success, duplicate)
	}
	if exists, err := s.InventoryBatchExists(ctx, "SKU1", "20261002-01"); err != nil || !exists {
		t.Fatal(exists, err)
	}
	for _, table := range []string{"inv_stock_documents", "inv_batch_registry", "fin_operating_entries"} {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 1 {
			t.Fatal(table, n, err)
		}
	}
	// Same batch is allowed for a different product.
	if _, err := s.CreateBatchInbound(ctx, 101, model.BatchInboundInput{ProductID: 2, WarehouseID: 1, BatchPrefix: "20261002", BatchSuffix: "01", SNs: []string{"OTHER"}}); err != nil {
		t.Fatal(err)
	}
	// Failed SN creation rolls back the reserved batch, permitting a correction.
	input := model.BatchInboundInput{ProductID: 1, WarehouseID: 1, BatchPrefix: "20261002", BatchSuffix: "03", SNs: []string{"OTHER"}}
	if _, err := s.CreateBatchInbound(ctx, 101, input); err == nil {
		t.Fatal("duplicate SN accepted")
	}
	if exists, err := s.InventoryBatchExists(ctx, "SKU1", "20261002-03"); err != nil || exists {
		t.Fatal(exists, err)
	}
	input.SNs = []string{"CORRECTED"}
	if _, err := s.CreateBatchInbound(ctx, 101, input); err != nil {
		t.Fatal(err)
	}
	// Both bulk and single-device inbound allow activation without another form.
	bulk, err := s.CreateBatchInbound(ctx, 101, model.BatchInboundInput{ProductID: 1, WarehouseID: 1, BatchPrefix: "20261002", BatchSuffix: "AUTO", SNs: []string{"AUTO-1", "AUTO-2"}, HardwareMACs: []string{"02:00:00:00:00:a1", "02:00:00:00:00:a2"}, QualityStatus: "qualified"})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range bulk.DeviceIDs {
		d, err := s.GetDevice(ctx, id)
		if err != nil || !d.ClaimEnabled || d.LifecycleStatus != "IN_STOCK" {
			t.Fatal(d, err)
		}
		p, err := s.ProvisionDevice(ctx, bulkMAC(i), "test-secret")
		if err != nil || p.State != "claimable" || len(p.Code) != 6 {
			t.Fatal(p, err)
		}
	}
	bad, err := s.CreateBatchInbound(ctx, 101, model.BatchInboundInput{ProductID: 1, WarehouseID: 1, BatchPrefix: "20261002", BatchSuffix: "BAD", SNs: []string{"BAD-1"}, HardwareMACs: []string{"02:00:00:00:00:a3"}, QualityStatus: "defective"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.GetDevice(ctx, bad.DeviceIDs[0])
	if err != nil || d.ClaimEnabled {
		t.Fatal(d, err)
	}
	p, err := s.ProvisionDevice(ctx, "02:00:00:00:00:a3", "test-secret")
	if err != nil || p.State != "disabled" || p.Code != "" {
		t.Fatal(p, err)
	}
	if err := s.ConfigureDeviceHardware(ctx, 101, d.ID, d.HardwareMAC, true, "一键允许激活"); err == nil {
		t.Fatal("defective device activation accepted")
	}
	single, err := s.CreateDevice(ctx, 101, model.CreateDeviceInput{SN: "AUTO-SINGLE", SKUCode: "SKU1", WarehouseID: 1, HardwareMAC: "02:00:00:00:00:a4"})
	if err != nil || !single.ClaimEnabled || single.LifecycleStatus != "IN_STOCK" {
		t.Fatal(single, err)
	}
	if err := s.ConfigureDeviceHardware(ctx, 101, single.ID, "02:00:00:00:00:a5", true, "一键允许激活"); err == nil {
		t.Fatal("registered MAC changed")
	}
	// Legacy inventory can still be activated with one click; inventory stays in stock.
	if err := s.ConfigureDeviceHardware(ctx, 101, 3, "02:00:00:00:00:03", true, "库存页面一键允许激活"); err != nil {
		t.Fatal(err)
	}
	d, err = s.GetDevice(ctx, 3)
	if err != nil || !d.ClaimEnabled || d.LifecycleStatus != "IN_STOCK" {
		t.Fatal(d, err)
	}
	exec(`UPDATE inv_devices SET lifecycle_status='SCRAPPED' WHERE id=?`, single.ID)
	if err := s.ConfigureDeviceHardware(ctx, 101, single.ID, single.HardwareMAC, true, "一键允许激活"); err == nil {
		t.Fatal("scrapped device activation accepted")
	}
	noMAC, err := s.CreateDevice(ctx, 101, model.CreateDeviceInput{SN: "NO-MAC", SKUCode: "SKU1", WarehouseID: 1})
	if err != nil || noMAC.ClaimEnabled || noMAC.HardwareMAC != "" {
		t.Fatal(noMAC, err)
	}
	var defaultAudits int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_provisioning_events WHERE event_code='ACTIVATION_ALLOWED' AND detail='合格设备入库，默认允许激活'`).Scan(&defaultAudits); err != nil || defaultAudits != 3 {
		t.Fatal(defaultAudits, err)
	}
}

func bulkMAC(i int) string { return fmt.Sprintf("02:00:00:00:00:a%d", i+1) }
