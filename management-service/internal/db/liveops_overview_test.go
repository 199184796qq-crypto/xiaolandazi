package db

import (
	"context"
	"testing"
)

func TestLiveOpsOverviewMySQL(t *testing.T) {
	store := salesIsolatedMySQL(t)
	ctx := context.Background()
	for _, statement := range []string{
		"ALTER TABLE crm_customer_profiles ADD COLUMN cooperation_status VARCHAR(32) NOT NULL DEFAULT 'cooperating'",
		"CREATE TABLE core_rooms (id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL)",
		"CREATE TABLE mgmt_room_deletions (room_id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL)",
		"CREATE TABLE live_runtime_sessions (tenant_id BIGINT, room_id BIGINT, execution_realm VARCHAR(32), status VARCHAR(32), ended_at DATETIME NULL)",
		"INSERT INTO mgmt_tenants (id) VALUES (70),(71),(72),(73)",
		"INSERT INTO mgmt_users (username, tenant_id, role) VALUES ('customer-a',70,'customer'),('customer-b',70,'customer'),('customer-c',71,'customer'),('customer-d',72,'customer'),('staff',73,'platform_admin')",
		"INSERT INTO crm_customer_profiles (tenant_id, cooperation_status) VALUES (71,'non_cooperating'),(72,'cooperating')",
		"INSERT INTO core_rooms VALUES (1,70),(2,71),(3,72),(4,70),(5,73),(6,74),(7,72)",
		"INSERT INTO mgmt_room_deletions VALUES (4,70)",
		"INSERT INTO live_runtime_sessions VALUES (70,1,'prod','running',NULL),(70,1,'prod','running',NULL),(71,2,'prod','paused',NULL),(72,3,'test','running',NULL),(70,4,'prod','running',NULL),(73,5,'prod','running',NULL),(72,7,'prod','running',CURRENT_TIMESTAMP)",
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := store.GetLiveOpsOverview(ctx, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if stats.CooperatingUsers != 3 || stats.TotalRooms != 4 ||
		len(stats.RunningRooms) != 1 || stats.RunningRooms[0] != (LiveOpsRoomKey{TenantID: 70, RoomID: 1}) {
		t.Fatalf("unexpected live operations overview: %+v", stats)
	}
}
