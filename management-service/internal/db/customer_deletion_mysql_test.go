package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestCustomerDeletionArchiveAndPurgeMySQL(t *testing.T) {
	store := salesIsolatedMySQL(t)
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int, query string, args ...any) {
		t.Helper()
		var got int
		if err := store.db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: got %d want %d (%v)", query, got, want, err)
		}
	}
	for _, statement := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE IF NOT EXISTS quota_buckets (") {
			exec(statement)
		}
	}
	if err := store.MigrateLiveRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`CREATE TABLE core_rooms(id BIGINT PRIMARY KEY,tenant_id BIGINT)`)
	exec(`ALTER TABLE mgmt_users ADD COLUMN avatar_url VARCHAR(1024) NOT NULL DEFAULT ''`)
	exec(`ALTER TABLE crm_customer_profiles ADD COLUMN cooperation_status VARCHAR(32) NOT NULL DEFAULT 'cooperating'`)
	for _, migrate := range []func() error{
		func() error { return store.MigrateRoomDeletions(ctx) },
		func() error { return store.MigrateContentPools(ctx) },
		func() error { return store.MigrateContentCorrections(ctx) },
		func() error { return store.MigrateAngleDirectionPools(ctx) },
		func() error { return store.MigrateLectureTasks(ctx) },
		func() error { return store.MigrateSpeechAnalysis(ctx) },
		func() error { return store.MigrateLiveContentRefresh(ctx) },
		func() error { return store.MigrateCustomMainlineUploads(ctx) },
		func() error { return store.MigrateCustomerDeletions(ctx) },
	} {
		if err := migrate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"live_agent_plan_prompts", "live_fact_risk_reviews_dedup_backup"} {
		exec("CREATE TABLE IF NOT EXISTS " + table + "(tenant_id BIGINT, plan_id BIGINT)")
	}
	exec(`INSERT INTO mgmt_tenants(id,code,name,status) VALUES (14,'target','删除样例','active'),(140,'other','其他客户','active'),(41,'shared','共享客户','active')`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,display_name,phone,role,status,password_hash) VALUES
		(8100,14,'sample','删除样例','test-8100','customer','active','fixture-password-hash'),
		(8101,140,'other','其他客户','test-8101','customer','active','other-hash'),
		(8102,41,'shared','共享客户','test-8102','customer','active','shared-hash'),
		(8103,41,'peer','同一租户','test-8103','customer','active','peer-hash')`)
	exec(`INSERT INTO mgmt_sessions(user_id) VALUES(8100),(8101)`)
	exec(`INSERT INTO crm_customer_profiles(tenant_id) VALUES(14),(140)`)
	exec(`INSERT INTO biz_orders(tenant_id,order_no,order_type,status,paid_amount_cents) VALUES(14,'preserve','membership','paid',1200)`)
	exec(`INSERT INTO fin_wallet_accounts(tenant_id,account_type,currency,balance_cents) VALUES(14,'cash','CNY',500)`)
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'方案1',''),(6,14,'方案2',''),(5,140,'其他方案','')`)
	exec(`INSERT INTO live_plan_support_requests(tenant_id,plan_id,staff_user_id,requested_by_user_id,status)
		VALUES(14,6,900,8100,'accepted')`)
	exec(`INSERT INTO voice_profiles(tenant_id,name,provider,voice_id,config_json,clone_status) VALUES
		(14,'客户音色','qwen','qwen-audio-3.0-tts-plus-fixture',JSON_OBJECT('source','customer_voice_clone','target_model','qwen-audio-3.0-tts-plus'),'deleted'),
		(14,'共享音色','qwen','shared-voice',JSON_OBJECT('source','customer_voice_clone'),'ready'),
		(140,'共享音色','qwen','shared-voice',JSON_OBJECT(),'ready')`)
	exec(`INSERT INTO voice_model_bindings(tenant_id,profile_id,sample_asset_id,provider,model,voice_id,config_json) VALUES
		(14,1,1,'cosyvoice','cosyvoice-v3.5-plus','cosyvoice-v3.5-plus-recloned',JSON_OBJECT('source','customer_voice_clone'))`)
	exec(`INSERT INTO live_agent_plan_versions(tenant_id,plan_id,room_id,version_no,voice_identity_json,variants_json,generation_context_json) VALUES
		(14,6,0,1,JSON_OBJECT('source','clone','provider','qwen','model','qwen-audio-3.1-tts-flash','voice_id','qwen-audio-3.1-tts-flash-historical'),JSON_ARRAY(),JSON_OBJECT())`)
	exec(`INSERT INTO media_assets(tenant_id,asset_type,original_name,storage_driver,object_key,metadata_json) VALUES
		(14,'audio','sample.wav','local','tenants/14/audio/sample.wav',JSON_OBJECT()),(140,'audio','neighbor.wav','local','tenants/140/audio/neighbor.wav',JSON_OBJECT())`)

	if _, err := store.QueueCustomerDeletion(ctx, 8100, 900, "wrong"); !errors.Is(err, ErrCustomerConfirmation) {
		t.Fatalf("wrong confirmation: %v", err)
	}
	if _, err := store.QueueCustomerDeletion(ctx, 8102, 900, "shared"); !errors.Is(err, ErrSharedCustomerTenant) {
		t.Fatalf("shared customer: %v", err)
	}
	count(0, `SELECT COUNT(*) FROM mgmt_customer_deletions`)
	count(2, `SELECT COUNT(*) FROM mgmt_sessions WHERE user_id IN (8100,8101)`)
	// Simulate failure after archive insertion and password/session changes.
	exec(`CREATE TRIGGER reject_archive BEFORE UPDATE ON mgmt_tenants
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='archive fixture failure'`)
	if _, err := store.QueueCustomerDeletion(ctx, 8100, 900, "sample"); err == nil {
		t.Fatal("archive should fail")
	}
	count(0, `SELECT COUNT(*) FROM mgmt_customer_deletions`)
	count(1, `SELECT COUNT(*) FROM mgmt_users WHERE id=8100 AND status='active' AND password_hash='fixture-password-hash'`)
	count(2, `SELECT COUNT(*) FROM mgmt_sessions WHERE user_id IN (8100,8101)`)
	exec(`DROP TRIGGER reject_archive`)
	job, err := store.QueueCustomerDeletion(ctx, 8100, 900, "sample")
	if err != nil || job.Status != "pending" {
		t.Fatal(job, err)
	}
	if _, err = store.QueueCustomerDeletion(ctx, 8100, 999, "sample"); err != nil {
		t.Fatal("second submit", err)
	}
	count(1, `SELECT COUNT(*) FROM mgmt_customer_deletions WHERE actor_user_id=900 AND profile_json->>'$.username'='sample'`)
	count(0, `SELECT COUNT(*) FROM mgmt_customer_deletions WHERE profile_json LIKE '%fixture-password-hash%'`)
	count(1, `SELECT COUNT(*) FROM mgmt_users WHERE id=8100 AND status='archived' AND password_hash=''`)
	count(1, `SELECT COUNT(*) FROM mgmt_sessions WHERE user_id=8101`)
	count(0, `SELECT COUNT(*) FROM mgmt_sessions WHERE user_id=8100`)
	if err := store.VerifyCustomerDeletionOwner(ctx, job); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimCustomerDeletion(ctx)
	if err != nil || claimed.UserID != 8100 {
		t.Fatal(claimed, err)
	}
	if _, err := store.ClaimCustomerDeletion(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("job claimed twice", err)
	}
	if err := store.RetryCustomerDeletion(ctx, job); err != nil {
		t.Fatal(err)
	}
	if current, err := store.GetCustomerDeletion(ctx, 8100); err != nil || !current.Retrying {
		t.Fatal(current, err)
	}
	voices, err := store.CustomerVoiceClones(ctx, 14)
	if err != nil || len(voices) != 3 {
		t.Fatal(voices, err)
	}
	for _, voice := range voices {
		if !strings.HasPrefix(voice.VoiceID, voice.Model+"-") {
			t.Fatal(voice)
		}
	}
	plans, _, err := store.CustomerDeletionIDs(ctx, 14)
	if err != nil || len(plans) != 2 {
		t.Fatal(plans, err)
	}
	if err := store.CompleteCustomerDeletion(ctx, job, plans); err != nil {
		t.Fatal(err)
	}
	count(2, `SELECT COUNT(*) FROM mgmt_users WHERE id IN (8100,8101)`)
	count(1, `SELECT COUNT(*) FROM mgmt_customer_deletions WHERE status='completed'`)
	count(1200, `SELECT paid_amount_cents FROM biz_orders WHERE tenant_id=14 AND status='paid'`)
	count(500, `SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=14`)
	count(1, `SELECT COUNT(*) FROM live_agent_plans WHERE tenant_id=140`)
	count(1, `SELECT COUNT(*) FROM voice_profiles WHERE tenant_id=140`)
	count(1, `SELECT COUNT(*) FROM media_assets WHERE tenant_id=140`)
	count(1, `SELECT COUNT(*) FROM live_agent_plans WHERE tenant_id=14 AND id=6 AND status='archived'`)
	count(1, `SELECT COUNT(*) FROM live_plan_support_requests WHERE tenant_id=14 AND plan_id=6 AND status='revoked'`)
	count(0, `SELECT COUNT(*) FROM live_agent_plans WHERE tenant_id=14 AND id=4`)
	for _, table := range []string{"voice_profiles", "voice_model_bindings", "media_assets", "live_agent_plan_versions"} {
		count(0, "SELECT COUNT(*) FROM "+table+" WHERE tenant_id=14")
	}
}
