package model

import "time"

type CommercialDeviceVersion struct {
	ID                          int64      `json:"id"`
	ProductID                   int64      `json:"product_id"`
	VersionNo                   uint32     `json:"version_no"`
	LifecycleStatus             string     `json:"lifecycle_status"`
	Currency                    string     `json:"currency"`
	ListPriceCents              uint64     `json:"list_price_cents"`
	SalePriceCents              uint64     `json:"sale_price_cents"`
	ParticipatesReferral        bool       `json:"participates_referral"`
	ParticipatesSalesCommission bool       `json:"participates_sales_commission"`
	ParticipatesAgentSettlement bool       `json:"participates_agent_settlement"`
	EffectiveFrom               *time.Time `json:"effective_from,omitempty"`
	EffectiveTo                 *time.Time `json:"effective_to,omitempty"`
	PublishedAt                 *time.Time `json:"published_at,omitempty"`
	CreatedAt                   time.Time  `json:"created_at"`
}

type CommercialDeviceProduct struct {
	ID             int64                    `json:"id"`
	Code           string                   `json:"code"`
	SKUCode        string                   `json:"sku_code"`
	Name           string                   `json:"name"`
	Description    string                   `json:"description"`
	Status         string                   `json:"status"`
	SortOrder      int                      `json:"sort_order"`
	AvailableStock int                      `json:"available_stock"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
	ActiveVersion  *CommercialDeviceVersion `json:"active_version,omitempty"`
	DraftVersion   *CommercialDeviceVersion `json:"draft_version,omitempty"`
	LatestVersion  *CommercialDeviceVersion `json:"latest_version,omitempty"`
}

type CommercialDeviceInput struct {
	Code                        string `json:"code"`
	SKUCode                     string `json:"sku_code"`
	Name                        string `json:"name"`
	Description                 string `json:"description"`
	SortOrder                   int    `json:"sort_order"`
	ListPriceCents              uint64 `json:"list_price_cents"`
	SalePriceCents              uint64 `json:"sale_price_cents"`
	ParticipatesReferral        bool   `json:"participates_referral"`
	ParticipatesSalesCommission bool   `json:"participates_sales_commission"`
	ParticipatesAgentSettlement bool   `json:"participates_agent_settlement"`
}

type CustomerDeviceOffer struct {
	ID                 int64  `json:"id"`
	Code               string `json:"code"`
	SKUCode            string `json:"sku_code"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	OriginalPriceCents uint64 `json:"original_price_cents"`
	SalePriceCents     uint64 `json:"sale_price_cents"`
	DiscountBPS        uint32 `json:"discount_bps"`
	VersionNo          uint32 `json:"version_no"`
	AvailableStock     int    `json:"available_stock"`
}

type CustomerOrderShipping struct {
	RecipientName  string `json:"recipient_name"`
	RecipientPhone string `json:"recipient_phone"`
	Province       string `json:"province"`
	City           string `json:"city"`
	District       string `json:"district"`
	Address        string `json:"address"`
	FullAddress    string `json:"full_address"`
}

type CustomerOrderDevice struct {
	ID          int64      `json:"id"`
	OrderID     int64      `json:"order_id"`
	OrderItemID int64      `json:"order_item_id"`
	DeviceID    int64      `json:"device_id"`
	SN          string     `json:"sn"`
	SKUCode     string     `json:"sku_code"`
	Status      string     `json:"status"`
	ShipmentID  *int64     `json:"shipment_id,omitempty"`
	ReservedAt  time.Time  `json:"reserved_at"`
	ShippedAt   *time.Time `json:"shipped_at,omitempty"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	ReturnedAt  *time.Time `json:"returned_at,omitempty"`
}
