package model

import "time"

type Warehouse struct {
	ID             int64     `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	OrganizationID *int64    `json:"organization_id,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type WarehouseInput struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Device struct {
	HardwareMAC        string    `json:"hardware_mac,omitempty"`
	ClaimEnabled       bool      `json:"claim_enabled"`
	ID                 int64     `json:"id"`
	SN                 string    `json:"sn"`
	SKUCode            string    `json:"sku_code"`
	BatchNo            string    `json:"batch_no"`
	OwnerOrgID         *int64    `json:"owner_org_id,omitempty"`
	CustodyWarehouseID *int64    `json:"custody_warehouse_id,omitempty"`
	CustodyWarehouse   string    `json:"custody_warehouse,omitempty"`
	CurrentCustomerID  *int64    `json:"current_customer_id,omitempty"`
	LifecycleStatus    string    `json:"lifecycle_status"`
	QualityStatus      string    `json:"quality_status"`
	CreatedByUserID    *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID    *int64    `json:"updated_by_user_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type DeviceLedgerEntry struct {
	ID              int64     `json:"id"`
	DeviceID        int64     `json:"device_id"`
	DocumentID      int64     `json:"document_id"`
	DocumentNo      string    `json:"document_no"`
	Action          string    `json:"action"`
	FromStatus      string    `json:"from_status"`
	ToStatus        string    `json:"to_status"`
	FromWarehouseID *int64    `json:"from_warehouse_id,omitempty"`
	ToWarehouseID   *int64    `json:"to_warehouse_id,omitempty"`
	FromOwnerOrgID  *int64    `json:"from_owner_org_id,omitempty"`
	ToOwnerOrgID    *int64    `json:"to_owner_org_id,omitempty"`
	FromCustomerID  *int64    `json:"from_customer_id,omitempty"`
	ToCustomerID    *int64    `json:"to_customer_id,omitempty"`
	OperatorUserID  *int64    `json:"operator_user_id,omitempty"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
}

type StockDocument struct {
	ID                  int64      `json:"id"`
	DocumentNo          string     `json:"document_no"`
	DocumentType        string     `json:"document_type"`
	Status              string     `json:"status"`
	FromWarehouseID     *int64     `json:"from_warehouse_id,omitempty"`
	ToWarehouseID       *int64     `json:"to_warehouse_id,omitempty"`
	CounterpartyOrgID   *int64     `json:"counterparty_org_id,omitempty"`
	ReferenceNo         string     `json:"reference_no"`
	ProductID           *int64     `json:"product_id,omitempty"`
	ProductName         string     `json:"product_name"`
	ExpectedQuantity    int        `json:"expected_quantity"`
	ActualQuantity      int        `json:"actual_quantity"`
	BusinessAmountCents uint64     `json:"business_amount_cents"`
	CounterpartyName    string     `json:"counterparty_name"`
	PaymentMethod       string     `json:"payment_method"`
	Reason              string     `json:"reason"`
	OperatorUserID      *int64     `json:"operator_user_id,omitempty"`
	ApprovedByUserID    *int64     `json:"approved_by_user_id,omitempty"`
	EffectiveAt         *time.Time `json:"effective_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	ItemCount           int        `json:"item_count"`
}

type RMARecord struct {
	ID                       int64      `json:"id"`
	RMANo                    string     `json:"rma_no"`
	DeviceID                 int64      `json:"device_id"`
	DeviceSN                 string     `json:"device_sn"`
	ServiceType              string     `json:"service_type"`
	Status                   string     `json:"status"`
	SourceType               string     `json:"source_type"`
	SourceUserID             *int64     `json:"source_user_id,omitempty"`
	SourceTenantID           *int64     `json:"source_tenant_id,omitempty"`
	SourceAgentOrgID         *int64     `json:"source_agent_org_id,omitempty"`
	CustomerName             string     `json:"customer_name"`
	ContactPhone             string     `json:"contact_phone"`
	Issue                    string     `json:"issue"`
	Resolution               string     `json:"resolution"`
	ReplacementDeviceID      *int64     `json:"replacement_device_id,omitempty"`
	SourceOrderID            *int64     `json:"source_order_id,omitempty"`
	SourceOrderNo            string     `json:"source_order_no"`
	SourceShipmentID         *int64     `json:"source_shipment_id,omitempty"`
	SourceShipmentNo         string     `json:"source_shipment_no"`
	ReturnShipmentID         *int64     `json:"return_shipment_id,omitempty"`
	ReturnShipmentNo         string     `json:"return_shipment_no"`
	OutboundShipmentID       *int64     `json:"outbound_shipment_id,omitempty"`
	OutboundShipmentNo       string     `json:"outbound_shipment_no"`
	RepairOutboundShipmentID *int64     `json:"repair_outbound_shipment_id,omitempty"`
	RepairOutboundShipmentNo string     `json:"repair_outbound_shipment_no"`
	RepairReturnShipmentID   *int64     `json:"repair_return_shipment_id,omitempty"`
	RepairReturnShipmentNo   string     `json:"repair_return_shipment_no"`
	RefundID                 *int64     `json:"refund_id,omitempty"`
	RefundNo                 string     `json:"refund_no"`
	OperatorUserID           *int64     `json:"operator_user_id,omitempty"`
	AcceptedByUserID         *int64     `json:"accepted_by_user_id,omitempty"`
	AcceptedAt               *time.Time `json:"accepted_at,omitempty"`
	CompletedAt              *time.Time `json:"completed_at,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type InventorySummary struct {
	WarehouseID     int64  `json:"warehouse_id"`
	WarehouseName   string `json:"warehouse_name"`
	LifecycleStatus string `json:"lifecycle_status"`
	Quantity        int64  `json:"quantity"`
}

type CreateDeviceInput struct {
	HardwareMAC   string `json:"hardware_mac,omitempty"`
	SN            string `json:"sn"`
	SKUCode       string `json:"sku_code"`
	BatchNo       string `json:"batch_no"`
	OwnerOrgID    *int64 `json:"owner_org_id,omitempty"`
	WarehouseID   int64  `json:"warehouse_id"`
	QualityStatus string `json:"quality_status"`
	Reason        string `json:"reason"`
}

type InventoryDeviceProduct struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	SKUCode string `json:"sku_code"`
	Name    string `json:"name"`
	Status  string `json:"status"`
}

type InventoryDeviceSKUType struct {
	SKUCode          string `json:"sku_code"`
	TotalQuantity    int64  `json:"total_quantity"`
	InStockQuantity  int64  `json:"in_stock_quantity"`
	WarehouseCount   int64  `json:"warehouse_count"`
	SampleSN         string `json:"sample_sn"`
	SampleBatchNo    string `json:"sample_batch_no"`
	BoundProductID   int64  `json:"bound_product_id"`
	BoundProductName string `json:"bound_product_name"`
}

type BatchInboundInput struct {
	BatchPrefix         string   `json:"batch_prefix,omitempty"`
	BatchSuffix         string   `json:"batch_suffix,omitempty"`
	HardwareMACs        []string `json:"hardware_macs,omitempty"`
	ProductID           int64    `json:"product_id"`
	BatchNo             string   `json:"batch_no"`
	PurchaseNo          string   `json:"purchase_no"`
	WarehouseID         int64    `json:"warehouse_id"`
	ExpectedQuantity    int      `json:"expected_quantity"`
	PurchaseAmountCents uint64   `json:"purchase_amount_cents"`
	SupplierName        string   `json:"supplier_name"`
	PaymentMethod       string   `json:"payment_method"`
	SNs                 []string `json:"sns"`
	QualityStatus       string   `json:"quality_status"`
	Reason              string   `json:"reason"`
}

type BatchInboundResult struct {
	DocumentID          int64    `json:"document_id"`
	DocumentNo          string   `json:"document_no"`
	ProductID           int64    `json:"product_id"`
	ProductName         string   `json:"product_name"`
	SKUCode             string   `json:"sku_code"`
	BatchNo             string   `json:"batch_no"`
	PurchaseNo          string   `json:"purchase_no"`
	ExpectedQuantity    int      `json:"expected_quantity"`
	ActualQuantity      int      `json:"actual_quantity"`
	PurchaseAmountCents uint64   `json:"purchase_amount_cents"`
	SupplierName        string   `json:"supplier_name"`
	PaymentMethod       string   `json:"payment_method"`
	DeviceIDs           []int64  `json:"device_ids"`
	SNs                 []string `json:"sns"`
}

type DeviceTransitionInput struct {
	ToStatus      string `json:"to_status"`
	ToWarehouseID *int64 `json:"to_warehouse_id,omitempty"`
	ToOwnerOrgID  *int64 `json:"to_owner_org_id,omitempty"`
	ToCustomerID  *int64 `json:"to_customer_id,omitempty"`
	Reason        string `json:"reason"`
	ReferenceNo   string `json:"reference_no"`
}

type CreateRMAInput struct {
	DeviceID         int64  `json:"device_id"`
	ServiceType      string `json:"service_type"`
	SourceType       string `json:"source_type"`
	SourceUserID     *int64 `json:"source_user_id,omitempty"`
	SourceTenantID   *int64 `json:"source_tenant_id,omitempty"`
	SourceAgentOrgID *int64 `json:"source_agent_org_id,omitempty"`
	CustomerName     string `json:"customer_name"`
	ContactPhone     string `json:"contact_phone"`
	Issue            string `json:"issue"`
}

type CompleteRMAInput struct {
	Resolution          string `json:"resolution"`
	ToStatus            string `json:"to_status"`
	ToWarehouseID       *int64 `json:"to_warehouse_id,omitempty"`
	ReplacementDeviceID *int64 `json:"replacement_device_id,omitempty"`
}

type RMAEvent struct {
	ID              int64     `json:"id"`
	RMAID           int64     `json:"rma_id"`
	EventCode       string    `json:"event_code"`
	Status          string    `json:"status"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	CustomerVisible bool      `json:"customer_visible"`
	OperatorUserID  *int64    `json:"operator_user_id,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type RMACost struct {
	ID               int64     `json:"id"`
	CostNo           string    `json:"cost_no"`
	RMAID            int64     `json:"rma_id"`
	CostType         string    `json:"cost_type"`
	AmountCents      uint64    `json:"amount_cents"`
	CounterpartyName string    `json:"counterparty_name"`
	PaymentMethod    string    `json:"payment_method"`
	Note             string    `json:"note"`
	OperatorUserID   *int64    `json:"operator_user_id,omitempty"`
	OccurredAt       time.Time `json:"occurred_at"`
	CreatedAt        time.Time `json:"created_at"`
}

type CreateRMACostInput struct {
	CostType         string `json:"cost_type"`
	AmountCents      uint64 `json:"amount_cents"`
	CounterpartyName string `json:"counterparty_name"`
	PaymentMethod    string `json:"payment_method"`
	Note             string `json:"note"`
}

type ShipmentItem struct {
	ID         int64     `json:"id"`
	ShipmentID int64     `json:"shipment_id"`
	DeviceID   int64     `json:"device_id"`
	SN         string    `json:"sn"`
	SKUCode    string    `json:"sku_code"`
	CreatedAt  time.Time `json:"created_at"`
}

type LogisticsEvent struct {
	ID             int64     `json:"id"`
	ShipmentID     int64     `json:"shipment_id"`
	EventCode      string    `json:"event_code"`
	Status         string    `json:"status"`
	Location       string    `json:"location"`
	Description    string    `json:"description"`
	OperatorUserID *int64    `json:"operator_user_id,omitempty"`
	OccurredAt     time.Time `json:"occurred_at"`
	CreatedAt      time.Time `json:"created_at"`
}

type Shipment struct {
	ID                  int64            `json:"id"`
	ShipmentNo          string           `json:"shipment_no"`
	ShipmentType        string           `json:"shipment_type"`
	BusinessType        string           `json:"business_type"`
	BusinessID          *int64           `json:"business_id,omitempty"`
	BusinessNo          string           `json:"business_no"`
	FromWarehouseID     *int64           `json:"from_warehouse_id,omitempty"`
	ToWarehouseID       *int64           `json:"to_warehouse_id,omitempty"`
	RecipientCustomerID *int64           `json:"recipient_customer_id,omitempty"`
	RecipientOrgID      *int64           `json:"recipient_org_id,omitempty"`
	RecipientType       string           `json:"recipient_type"`
	DeliveryMethod      string           `json:"delivery_method"`
	LogisticsFeeCents   uint64           `json:"logistics_fee_cents"`
	RecipientName       string           `json:"recipient_name"`
	RecipientPhone      string           `json:"recipient_phone"`
	RecipientAddress    string           `json:"recipient_address"`
	CarrierCode         string           `json:"carrier_code"`
	CarrierName         string           `json:"carrier_name"`
	TrackingNo          string           `json:"tracking_no"`
	Status              string           `json:"status"`
	Note                string           `json:"note"`
	OperatorUserID      *int64           `json:"operator_user_id,omitempty"`
	ShippedAt           *time.Time       `json:"shipped_at,omitempty"`
	DeliveredAt         *time.Time       `json:"delivered_at,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
	Items               []ShipmentItem   `json:"items"`
	Events              []LogisticsEvent `json:"events"`
}

type CreateShipmentInput struct {
	ShipmentType        string  `json:"shipment_type"`
	BusinessType        string  `json:"business_type"`
	BusinessID          *int64  `json:"business_id,omitempty"`
	BusinessNo          string  `json:"business_no"`
	FromWarehouseID     *int64  `json:"from_warehouse_id,omitempty"`
	ToWarehouseID       *int64  `json:"to_warehouse_id,omitempty"`
	RecipientCustomerID *int64  `json:"recipient_customer_id,omitempty"`
	RecipientOrgID      *int64  `json:"recipient_org_id,omitempty"`
	RecipientType       string  `json:"recipient_type"`
	DeliveryMethod      string  `json:"delivery_method"`
	LogisticsFeeCents   *uint64 `json:"logistics_fee_cents,omitempty"`
	RecipientName       string  `json:"recipient_name"`
	RecipientPhone      string  `json:"recipient_phone"`
	RecipientAddress    string  `json:"recipient_address"`
	CarrierCode         string  `json:"carrier_code"`
	CarrierName         string  `json:"carrier_name"`
	TrackingNo          string  `json:"tracking_no"`
	DeviceIDs           []int64 `json:"device_ids"`
	Note                string  `json:"note"`
}

type ShipmentStatusInput struct {
	Status      string `json:"status"`
	Location    string `json:"location"`
	Description string `json:"description"`
}

type ScrapDisposalInput struct {
	AmountCents   uint64 `json:"amount_cents"`
	BuyerName     string `json:"buyer_name"`
	PaymentMethod string `json:"payment_method"`
	Note          string `json:"note"`
}

type ScrapDisposal struct {
	ID             int64     `json:"id"`
	DisposalNo     string    `json:"disposal_no"`
	DeviceID       int64     `json:"device_id"`
	DeviceSN       string    `json:"device_sn"`
	AmountCents    uint64    `json:"amount_cents"`
	BuyerName      string    `json:"buyer_name"`
	PaymentMethod  string    `json:"payment_method"`
	Note           string    `json:"note"`
	OperatorUserID *int64    `json:"operator_user_id,omitempty"`
	DisposedAt     time.Time `json:"disposed_at"`
	CreatedAt      time.Time `json:"created_at"`
}
