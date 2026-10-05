#!/usr/bin/env bash
set -euo pipefail
set -a
source /etc/xiaolan/management.env
set +a
MYSQL_PWD="$DB_PASSWORD" mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch <<'SQL'
SELECT id,code,sku_code,name,status FROM catalog_device_products;
SELECT id,sn,sku_code,batch_no,lifecycle_status,current_customer_id FROM inv_devices;
SELECT id,document_no,document_type,product_id,reference_no,business_amount_cents FROM inv_stock_documents;
SELECT id,rma_no,device_id,replacement_device_id,status FROM inv_rmas;
SELECT id,order_no,order_type,status FROM biz_orders WHERE order_type='device';
SELECT table_name,column_name FROM information_schema.columns WHERE table_schema=DATABASE() AND (column_name IN ('device_id','replacement_device_id','product_id','device_product_id','rma_id','shipment_id','document_id','order_id','reference_id','reference_type','source_id','source_type')) ORDER BY table_name,ordinal_position;
SELECT table_name,column_name FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name IN ('biz_order_devices','biz_order_items','fin_operating_entries','fin_refund_orders','fin_wallet_ledger','fin_payment_transactions','inc_earnings','inv_shipments','catalog_device_versions','live_runtime_events','live_device_room_bindings','work_inbox_revisions') ORDER BY table_name,ordinal_position;
SELECT id,order_id,device_id,shipment_id FROM biz_order_devices;
SELECT id,order_id,product_type,product_id FROM biz_order_items WHERE order_id IN (1,2,3);
SELECT id,shipment_no,shipment_type FROM inv_shipments;
SELECT id,shipment_id,device_id FROM inv_shipment_items;
SELECT id,source_key,amount_cents FROM fin_operating_entries;
SELECT id,source_type,source_id,status FROM fin_refund_orders;
SELECT table_name,column_name,referenced_table_name,referenced_column_name FROM information_schema.key_column_usage WHERE table_schema=DATABASE() AND referenced_table_name IS NOT NULL;
SELECT 'wallet',COUNT(*) FROM fin_wallet_ledger WHERE order_no IN (SELECT order_no FROM biz_orders WHERE id IN(1,2,3));
SELECT 'payment',id,order_id,status,paid_amount_cents FROM fin_payment_transactions WHERE order_id IN(1,2,3);
SELECT 'earnings',id,source_order_id,status,amount_cents FROM inc_earnings WHERE source_order_id IN(1,2,3);
SELECT 'receipts',id,order_id,status FROM fin_customer_receipts WHERE order_id IN(1,2,3);
SELECT table_name,column_name FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name IN('fin_customer_receipt_events','fin_customer_confirmations','fin_beneficiary_wallet_ledger','inc_settlement_items','crm_sales_handover_items','staff_approval_tasks') ORDER BY table_name,ordinal_position;
SELECT 'rma_costs',COUNT(*) FROM inv_rma_costs WHERE rma_id IN(1,2,3,4);
SELECT 'rma_links',rma_id,source_order_id,source_shipment_id,return_shipment_id,outbound_shipment_id FROM inv_rma_links;
SQL
