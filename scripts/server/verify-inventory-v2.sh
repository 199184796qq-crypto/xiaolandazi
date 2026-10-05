#!/usr/bin/env bash
set -euo pipefail
set -a
source /etc/xiaolan/management.env
set +a
[[ "$(readlink -f /opt/xiaolan/current)" == /opt/xiaolan/releases/20261002-1301-inventory-v2 ]]
systemctl is-active xiaolan-management xiaolan-core xiaolan-xiaozhi
MYSQL_PWD="$DB_PASSWORD" mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch <<'SQL'
SELECT id,name,sku_code FROM catalog_device_products;
SELECT id,sn,sku_code,batch_no,lifecycle_status FROM inv_devices;
SELECT sku_code,batch_no FROM inv_batch_registry;
SELECT COUNT(*) AS remaining_e2e_products FROM catalog_device_products WHERE sku_code LIKE 'E2E%';
SELECT COUNT(*) AS remaining_e2e_devices FROM inv_devices WHERE sn LIKE 'E2E%';
SELECT COUNT(*) AS remaining_test_device_orders FROM biz_orders WHERE order_no IN ('ORD-DEV-1790061783726853401','ORD-DEV-1790061882936235224','ORD-DEV-1790061922623009921');
SELECT COUNT(*) AS preserved_real_inbound FROM inv_stock_documents WHERE id=40 AND document_no='IN-1790085519635248700';
SQL
for path in /api/v1/inventory/stock-products /api/v1/inventory/stock-devices /api/v1/inventory/batches/check; do
  code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:8080$path")"
  [[ "$code" == 401 ]] || { echo "Unexpected auth response for $path: $code"; exit 1; }
  echo "Protected inventory route: $path"
done
curl -fsS https://www.xiaolandaizi.cn/resources/inventory | grep -q index-CNkr_ePL.js
curl -fsS --output /dev/null https://www.xiaolandaizi.cn/assets/index-CNkr_ePL.js
curl -fsS --output /dev/null https://www.xiaolandaizi.cn/assets/index-BDcLtUcP.css
echo 'New inventory assets and service health verified'
