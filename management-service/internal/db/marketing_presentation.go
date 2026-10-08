package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

var marketingPresentationColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
var ErrMarketingPresentationAssetNotFound = errors.New("marketing presentation asset not found")

func defaultMarketingCampaignPresentation() model.MarketingCampaignPresentation {
	return model.MarketingCampaignPresentation{
		SchemaVersion:   1,
		TemplateKey:     "blue_tech",
		DisplaySize:     "standard",
		BackgroundStyle: "soft_gradient",
		PrimaryColor:    "#356DFF",
		AccentColor:     "#FF586B",
		Eyebrow:         "限时活动",
		BadgeText:       "当前优惠",
		CTAButtonText:   "立即参与",
		ShowCountdown:   true,
		ShowSaving:      true,
		ShowConditions:  true,
	}
}

func normalizeMarketingCampaignPresentation(input *model.MarketingCampaignPresentation) (model.MarketingCampaignPresentation, error) {
	if input == nil {
		return defaultMarketingCampaignPresentation(), nil
	}
	item := *input
	if item.SchemaVersion == 0 {
		item.SchemaVersion = 1
	}
	item.TemplateKey = strings.TrimSpace(item.TemplateKey)
	item.DisplaySize = strings.TrimSpace(item.DisplaySize)
	item.BackgroundStyle = strings.TrimSpace(item.BackgroundStyle)
	item.PrimaryColor = strings.ToUpper(strings.TrimSpace(item.PrimaryColor))
	item.AccentColor = strings.ToUpper(strings.TrimSpace(item.AccentColor))
	item.Eyebrow = strings.TrimSpace(item.Eyebrow)
	item.BadgeText = strings.TrimSpace(item.BadgeText)
	item.CTAButtonText = strings.TrimSpace(item.CTAButtonText)
	defaults := defaultMarketingCampaignPresentation()
	if item.TemplateKey == "" {
		item.TemplateKey = defaults.TemplateKey
	}
	if item.DisplaySize == "" {
		item.DisplaySize = defaults.DisplaySize
	}
	if item.BackgroundStyle == "" {
		item.BackgroundStyle = defaults.BackgroundStyle
	}
	if item.PrimaryColor == "" {
		item.PrimaryColor = defaults.PrimaryColor
	}
	if item.AccentColor == "" {
		item.AccentColor = defaults.AccentColor
	}
	if item.Eyebrow == "" {
		item.Eyebrow = defaults.Eyebrow
	}
	if item.BadgeText == "" {
		item.BadgeText = defaults.BadgeText
	}
	if item.CTAButtonText == "" {
		item.CTAButtonText = defaults.CTAButtonText
	}
	if item.SchemaVersion != 1 {
		return model.MarketingCampaignPresentation{}, errors.New("不支持的活动皮肤版本")
	}
	if !oneOf(item.TemplateKey, "blue_tech", "festival_red", "newcomer_gold", "fresh_green", "minimal_light") {
		return model.MarketingCampaignPresentation{}, errors.New("不支持的活动皮肤模板")
	}
	if !oneOf(item.DisplaySize, "featured", "standard", "compact") {
		return model.MarketingCampaignPresentation{}, errors.New("不支持的活动展示尺寸")
	}
	if !oneOf(item.BackgroundStyle, "soft_gradient", "spotlight", "solid") {
		return model.MarketingCampaignPresentation{}, errors.New("不支持的活动背景样式")
	}
	if !marketingPresentationColor.MatchString(item.PrimaryColor) || !marketingPresentationColor.MatchString(item.AccentColor) {
		return model.MarketingCampaignPresentation{}, errors.New("活动皮肤颜色必须为六位十六进制颜色")
	}
	if utf8.RuneCountInString(item.Eyebrow) > 32 || utf8.RuneCountInString(item.BadgeText) > 24 || utf8.RuneCountInString(item.CTAButtonText) > 16 {
		return model.MarketingCampaignPresentation{}, errors.New("活动皮肤文案过长")
	}
	if item.CoverAssetID != nil && *item.CoverAssetID <= 0 {
		return model.MarketingCampaignPresentation{}, errors.New("活动主视觉资产无效")
	}
	return item, nil
}

// ValidateMarketingCampaignPresentation validates presentation-only campaign
// input before persistence so API clients receive a 4xx response for invalid
// skin configuration instead of a generic storage failure.
func ValidateMarketingCampaignPresentation(input *model.MarketingCampaignPresentation) error {
	_, err := normalizeMarketingCampaignPresentation(input)
	return err
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func saveMarketingCampaignPresentationTx(ctx context.Context, tx *sql.Tx, campaignID, userID int64, input *model.MarketingCampaignPresentation) error {
	presentation, err := normalizeMarketingCampaignPresentation(input)
	if err != nil {
		return err
	}
	if presentation.CoverAssetID != nil {
		var exists int
		if err := tx.QueryRowContext(ctx, `
			SELECT 1 FROM mkt_campaign_skin_assets WHERE id=? LIMIT 1
		`, *presentation.CoverAssetID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrMarketingPresentationAssetNotFound
			}
			return err
		}
	}
	raw, err := json.Marshal(presentation)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO mkt_campaign_presentations (
			campaign_id, schema_version, template_key, display_size,
			presentation_json, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			schema_version=VALUES(schema_version),
			template_key=VALUES(template_key),
			display_size=VALUES(display_size),
			presentation_json=VALUES(presentation_json),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, campaignID, presentation.SchemaVersion, presentation.TemplateKey, presentation.DisplaySize, raw, userID)
	return err
}

func (s *Store) loadMarketingCampaignPresentations(ctx context.Context, campaigns []model.MarketingCampaign) error {
	if len(campaigns) == 0 {
		return nil
	}
	indexByID := make(map[int64]int, len(campaigns))
	for index := range campaigns {
		campaigns[index].Presentation = defaultMarketingCampaignPresentation()
		indexByID[campaigns[index].ID] = index
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT campaign_id, CAST(presentation_json AS CHAR)
		FROM mkt_campaign_presentations
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var campaignID int64
		var raw string
		if err := rows.Scan(&campaignID, &raw); err != nil {
			return err
		}
		index, ok := indexByID[campaignID]
		if !ok {
			continue
		}
		var stored model.MarketingCampaignPresentation
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return err
		}
		normalized, err := normalizeMarketingCampaignPresentation(&stored)
		if err != nil {
			return err
		}
		campaigns[index].Presentation = normalized
	}
	return rows.Err()
}
