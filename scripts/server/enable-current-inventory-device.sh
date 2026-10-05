#!/usr/bin/env bash
set -euo pipefail
[[ "$(id -u)" == 0 ]] || exit 2
[[ "$(readlink -f /opt/xiaolan/current)" == /opt/xiaolan/releases/20261002-1348-default-activation ]] || exit 2
# The deployment backup captures the pre-change row and all audit records.
[[ -s /opt/xiaolan/deployments/20261002-1348-default-activation-backup/database-before.sql.gz ]] || exit 2
set -a
source /etc/xiaolan/management.env
set +a
MYSQL_PWD="$DB_PASSWORD" mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch <<'SQL'
START TRANSACTION;
SELECT id FROM inv_devices WHERE id=7 FOR UPDATE;
SET @activation_target=(SELECT d.id FROM inv_devices d JOIN device_hardware_profiles p ON p.device_id=d.id WHERE d.id=7 AND d.sn='xidz0001' AND d.sku_code='xldzplus' AND d.quality_status='qualified' AND d.lifecycle_status='IN_STOCK' AND d.current_customer_id IS NULL AND p.claimed_tenant_id IS NULL AND p.hardware_mac='1c:29:04:31:0e:b8');
UPDATE device_hardware_profiles SET claim_enabled=TRUE WHERE device_id=@activation_target AND claim_enabled=FALSE;
SET @activation_changed=ROW_COUNT();
INSERT INTO device_provisioning_events(device_id,event_code,detail) SELECT @activation_target,'ACTIVATION_ALLOWED','入库默认允许激活业务升级，为当前合格在库设备补齐激活许可' WHERE @activation_changed=1;
UPDATE work_inbox_revisions SET revision=revision+1 WHERE topic='inventory' AND @activation_changed=1;
COMMIT;
SELECT d.id,d.sn,d.lifecycle_status,d.quality_status,p.hardware_mac,p.claim_enabled FROM inv_devices d JOIN device_hardware_profiles p ON p.device_id=d.id WHERE d.id=7;
SQL
curl -fsS --max-time 8 -H 'Content-Type: application/json' -H "X-Xiaozhi-Internal-Token: $XIAOZHI_INTERNAL_TOKEN" -d '{"hardware_mac":"1c:29:04:31:0e:b8"}' http://127.0.0.1:8080/internal/v1/xiaozhi/provision | python3 -c 'import json,sys; p=json.load(sys.stdin); assert p.get("state") in ("claimable","claimed","bound"), p.get("state"); assert p.get("state")!="claimable" or (len(p.get("binding_code",""))==6 or len(p.get("code",""))==6); print("device_state="+p["state"]+"; activation_verified=true")'
