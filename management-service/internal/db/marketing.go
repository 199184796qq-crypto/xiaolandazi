package db

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type marketingCampaignPayload struct {
	DisplayLocations []string                      `json:"display_locations"`
	Items            []model.MarketingCampaignItem `json:"items"`
	Description      string                        `json:"description"`
	PricingRule      string                        `json:"pricing_rule"`
	TargetType       string                        `json:"target_type"`
	TargetID         int64                         `json:"target_id"`
	PricingMode      string                        `json:"pricing_mode"`
	PackageMonths    uint32                        `json:"package_months"`
	DiscountBPS      uint32                        `json:"discount_bps"`
	StartsAt         string                        `json:"starts_at"`
	EndsAt           string                        `json:"ends_at"`
}

const marketingBackofficePlacement = "backoffice"

func validMarketingPlacementCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func normalizeMarketingDisplayLocations(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if !validMarketingPlacementCode(value) {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) >= 16 {
			break
		}
	}
	if len(result) == 0 {
		return []string{marketingBackofficePlacement}
	}
	if len(result) > 1 {
		filtered := result[:0]
		for _, value := range result {
			if value != marketingBackofficePlacement {
				filtered = append(filtered, value)
			}
		}
		if len(filtered) > 0 {
			result = filtered
		}
	}
	return result
}

func marketingCampaignHasDisplayLocation(campaign model.MarketingCampaign, location string) bool {
	location = strings.ToLower(strings.TrimSpace(location))
	if location == "" {
		return true
	}
	for _, value := range campaign.DisplayLocations {
		if strings.EqualFold(strings.TrimSpace(value), location) {
			return true
		}
	}
	return false
}

func marketingCampaignCustomerVisible(campaign model.MarketingCampaign) bool {
	for _, value := range campaign.DisplayLocations {
		if strings.TrimSpace(value) != "" && !strings.EqualFold(value, marketingBackofficePlacement) {
			return true
		}
	}
	return false
}

func normalizeCustomerMarketingPlacement(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "membership":
		return "membership"
	default:
		return "shop"
	}
}

func normalizeMarketingDiscountBPS(value uint32) uint32 {
	if value >= 100 && value < 1000 {
		value *= 10
	}
	if value > 10000 {
		return 10000
	}
	return value
}

// Marketing money is always settled at whole-yuan precision.
// Jiao/fen are discarded directly; never rounded up.
func floorMarketingYuanCents(value uint64) uint64 {
	return (value / 100) * 100
}

func calculateMarketingPayableCents(baseCents uint64, discountBPS uint32) uint64 {
	baseCents = floorMarketingYuanCents(baseCents)
	discountBPS = normalizeMarketingDiscountBPS(discountBPS)
	if baseCents == 0 || discountBPS == 0 {
		return 0
	}
	return floorMarketingYuanCents(baseCents * uint64(discountBPS) / 10000)
}

func normalizeMarketingCampaignItem(item model.MarketingCampaignItem) (model.MarketingCampaignItem, bool) {
	item.TargetType = strings.TrimSpace(item.TargetType)
	item.PricingMode = strings.TrimSpace(item.PricingMode)
	if item.TargetID <= 0 {
		return model.MarketingCampaignItem{}, false
	}
	switch item.TargetType {
	case "membership", "time_card", "device_product":
	default:
		return model.MarketingCampaignItem{}, false
	}
	if item.PricingMode == "" {
		item.PricingMode = "discount"
	}
	switch item.PricingMode {
	case "discount", "package", "fixed", "free":
	default:
		return model.MarketingCampaignItem{}, false
	}
	if item.PricingMode == "package" {
		if item.TargetType != "membership" {
			return model.MarketingCampaignItem{}, false
		}
		if item.PackageMonths == 0 || item.PackageMonths > 120 {
			return model.MarketingCampaignItem{}, false
		}
	} else {
		item.PackageMonths = 1
	}
	if item.PricingMode == "fixed" && (item.FixedPriceCents == nil || *item.FixedPriceCents > 10000000000) {
		return model.MarketingCampaignItem{}, false
	}
	if item.PricingMode != "fixed" {
		item.FixedPriceCents = nil
	}
	if item.Quantity == 0 {
		item.Quantity = 1
	}
	item.DiscountBPS = normalizeMarketingDiscountBPS(item.DiscountBPS)
	return item, true
}

func parseMarketingCampaign(record model.FeatureRecord) (model.MarketingCampaign, bool) {
	var payload marketingCampaignPayload
	if err := json.Unmarshal([]byte(record.PayloadJSON), &payload); err != nil {
		return model.MarketingCampaign{}, false
	}

	items := make([]model.MarketingCampaignItem, 0, len(payload.Items)+1)
	for _, rawItem := range payload.Items {
		if item, ok := normalizeMarketingCampaignItem(rawItem); ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 && payload.TargetID > 0 {
		if item, ok := normalizeMarketingCampaignItem(model.MarketingCampaignItem{
			TargetType:    payload.TargetType,
			TargetID:      payload.TargetID,
			PricingMode:   payload.PricingMode,
			PackageMonths: payload.PackageMonths,
			DiscountBPS:   payload.DiscountBPS,
			Quantity:      1,
		}); ok {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return model.MarketingCampaign{}, false
	}
	first := items[0]

	displayLocations := normalizeMarketingDisplayLocations(payload.DisplayLocations)
	if len(payload.DisplayLocations) == 0 {
		membershipOnly := true
		for _, item := range items {
			if item.TargetType != "membership" {
				membershipOnly = false
				break
			}
		}
		if membershipOnly {
			displayLocations = []string{"membership"}
		} else {
			displayLocations = []string{"shop"}
		}
	}

	return model.MarketingCampaign{
		ID:               record.ID,
		Code:             record.RecordKey,
		Name:             record.Title,
		Description:      strings.TrimSpace(payload.Description),
		Status:           record.Status,
		SortOrder:        record.SortOrder,
		PricingRule:      strings.TrimSpace(payload.PricingRule),
		Items:            items,
		DisplayLocations: displayLocations,
		TargetType:       first.TargetType,
		TargetID:         first.TargetID,
		PricingMode:      first.PricingMode,
		PackageMonths:    first.PackageMonths,
		DiscountBPS:      first.DiscountBPS,
		StartsAt:         strings.TrimSpace(payload.StartsAt),
		EndsAt:           strings.TrimSpace(payload.EndsAt),
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}, true
}

func marketingCampaignTargetItem(
	campaign model.MarketingCampaign,
	targetType string,
	targetID int64,
) (model.MarketingCampaignItem, bool) {
	for _, item := range campaign.Items {
		if item.TargetType != targetType {
			continue
		}
		if targetID > 0 && item.TargetID != targetID {
			continue
		}
		return item, true
	}
	return model.MarketingCampaignItem{}, false
}

func marketingCampaignHasTarget(campaign model.MarketingCampaign, targetType string, targetID int64) bool {
	_, ok := marketingCampaignTargetItem(campaign, targetType, targetID)
	return ok
}

func marketingCampaignWindowActive(campaign model.MarketingCampaign, now time.Time) bool {
	if campaign.Status != "active" {
		return false
	}
	if campaign.StartsAt != "" {
		if value, err := time.Parse(time.RFC3339, campaign.StartsAt); err == nil && now.Before(value) {
			return false
		}
	}
	if campaign.EndsAt != "" {
		if value, err := time.Parse(time.RFC3339, campaign.EndsAt); err == nil && !now.Before(value) {
			return false
		}
	}
	return true
}

func (s *Store) ListMarketingCampaigns(
	ctx context.Context,
	targetType string,
	targetID int64,
	activeOnly bool,
	displayLocations ...string,
) ([]model.MarketingCampaign, error) {
	all, err := s.listNormalizedMarketingCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	items := make([]model.MarketingCampaign, 0, len(all))
	for _, campaign := range all {
		if targetType != "" {
			if !marketingCampaignHasTarget(campaign, targetType, targetID) {
				continue
			}
		} else if targetID > 0 {
			matched := false
			for _, item := range campaign.Items {
				if item.TargetID == targetID {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if len(displayLocations) > 0 {
			matchedPlacement := false
			for _, location := range displayLocations {
				if marketingCampaignHasDisplayLocation(campaign, location) {
					matchedPlacement = true
					break
				}
			}
			if !matchedPlacement {
				continue
			}
		}
		if activeOnly && !marketingCampaignWindowActive(campaign, now) {
			continue
		}
		items = append(items, campaign)
	}
	return items, nil
}

func marketingCampaignIDFromCycle(value string) int64 {
	const prefix = "campaign:"
	if !strings.HasPrefix(value, prefix) {
		return 0
	}
	id, _ := strconv.ParseInt(strings.TrimPrefix(value, prefix), 10, 64)
	return id
}

func marketingCycleKey(campaign model.MarketingCampaign) string {
	return "campaign:" + strconv.FormatInt(campaign.ID, 10)
}

// migrateLegacyMembershipMarketingCampaigns performs the one-way separation
// of package pricing from the membership product. Stable record keys make the
// migration idempotent; after records are created, legacy package columns are
// neutralized so deleting a campaign will not recreate it on the next boot.
func (s *Store) migrateLegacyMembershipMarketingCampaigns(ctx context.Context) error {
	type legacyPackage struct {
		suffix string
		title  string
		months uint32
		column string
	}
	packages := []legacyPackage{
		{suffix: "month", title: "包月优惠", months: 1, column: "recurring_month_discount_bps"},
		{suffix: "quarter", title: "包季优惠", months: 3, column: "recurring_quarter_discount_bps"},
		{suffix: "annual", title: "包年优惠", months: 12, column: "annual_discount_bps"},
	}

	for _, item := range packages {
		query := "INSERT IGNORE INTO sys_feature_records " +
			"(feature_key, record_key, title, status, sort_order, payload_json) " +
			"SELECT 'marketing-campaigns', " +
			"CONCAT('legacy-membership-', p.id, '-', ?), CONCAT(p.name, ?), " +
			"'active', ?, JSON_OBJECT(" +
			"'target_type','membership','target_id',p.id,'pricing_mode','package'," +
			"'package_months',?,'discount_bps'," +
			"CASE WHEN v." + item.column + " >= 100 AND v." + item.column + " < 1000 " +
			"THEN v." + item.column + " * 10 ELSE v." + item.column + " END) " +
			"FROM catalog_membership_plans p " +
			"INNER JOIN catalog_membership_plan_versions v ON v.id=(" +
			"SELECT v2.id FROM catalog_membership_plan_versions v2 " +
			"WHERE v2.plan_id=p.id ORDER BY " +
			"CASE v2.lifecycle_status WHEN 'draft' THEN 0 WHEN 'active' THEN 1 ELSE 2 END, " +
			"v2.version_no DESC LIMIT 1) " +
			"WHERE v." + item.column + " > 0 AND " +
			"(CASE WHEN v." + item.column + " >= 100 AND v." + item.column + " < 1000 " +
			"THEN v." + item.column + " * 10 ELSE v." + item.column + " END) < 10000"
		if _, err := s.db.ExecContext(
			ctx,
			query,
			item.suffix,
			item.title,
			int(item.months*10),
			item.months,
		); err != nil {
			return err
		}
	}

	_, err := s.db.ExecContext(
		ctx,
		"UPDATE catalog_membership_plan_versions "+
			"SET recurring_month_discount_bps=10000, "+
			"recurring_quarter_discount_bps=10000, "+
			"annual_discount_bps=10000 "+
			"WHERE recurring_month_discount_bps<>10000 "+
			"OR recurring_quarter_discount_bps<>10000 "+
			"OR annual_discount_bps<>10000",
	)
	return err
}
