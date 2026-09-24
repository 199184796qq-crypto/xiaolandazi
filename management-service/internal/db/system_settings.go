package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

type seededSystemSetting struct {
	Key       string
	Group     string
	Label     string
	Value     string
	InputType string
	SortOrder int
}

type seededDictionaryItem struct {
	Category    string
	Code        string
	Label       string
	Description string
	SortOrder   int
}

func (s *Store) MigrateSystemSettings(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS mgmt_system_settings (
			setting_key VARCHAR(96) NOT NULL,
			setting_group VARCHAR(64) NOT NULL DEFAULT 'general',
			label VARCHAR(160) NOT NULL,
			value_text TEXT NOT NULL,
			input_type VARCHAR(32) NOT NULL DEFAULT 'text',
			sort_order INT NOT NULL DEFAULT 0,
			updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (setting_key),
			KEY idx_mgmt_system_settings_group (setting_group, sort_order, setting_key)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS mgmt_system_dictionary_items (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			category VARCHAR(64) NOT NULL,
			code VARCHAR(96) NOT NULL,
			label VARCHAR(160) NOT NULL,
			description VARCHAR(512) NOT NULL DEFAULT '',
			sort_order INT NOT NULL DEFAULT 0,
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			system_seeded TINYINT(1) NOT NULL DEFAULT 0,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_mgmt_system_dictionary_category_code (category, code),
			KEY idx_mgmt_system_dictionary_category (category, enabled, sort_order, id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply system settings schema: %w", err)
		}
	}

	settings := []seededSystemSetting{
		{Key: "site_name", Group: "brand", Label: "系统显示名称", Value: "小蓝搭子", InputType: "text", SortOrder: 10},
		{Key: "internal_agent_name", Group: "agent", Label: "后台智能体名称", Value: "小蓝工作搭子", InputType: "text", SortOrder: 10},
		{Key: "client_agent_name", Group: "agent", Label: "前端智能体名称", Value: "小蓝直播搭子", InputType: "text", SortOrder: 20},
		{Key: "device_order_hold_minutes", Group: "commerce", Label: "设备订单未支付锁库分钟数", Value: "15", InputType: "number", SortOrder: 10},
		{Key: "footer_enabled", Group: "footer", Label: "显示全局页脚", Value: "true", InputType: "boolean", SortOrder: 10},
		{Key: "footer_copyright", Group: "footer", Label: "版权文字", Value: "© 2026 小蓝搭子", InputType: "text", SortOrder: 20},
		{Key: "footer_icp_text", Group: "footer", Label: "ICP备案文字", Value: "", InputType: "text", SortOrder: 30},
		{Key: "footer_icp_url", Group: "footer", Label: "ICP备案链接", Value: "", InputType: "url", SortOrder: 40},
		{Key: "footer_police_text", Group: "footer", Label: "公安备案文字", Value: "", InputType: "text", SortOrder: 50},
		{Key: "footer_police_url", Group: "footer", Label: "公安备案链接", Value: "", InputType: "url", SortOrder: 60},
		{Key: "footer_report_text", Group: "footer", Label: "举报中心文字", Value: "", InputType: "text", SortOrder: 70},
		{Key: "footer_report_url", Group: "footer", Label: "举报中心链接", Value: "", InputType: "url", SortOrder: 80},
		{Key: "footer_extra_text", Group: "footer", Label: "页脚补充文字", Value: "", InputType: "text", SortOrder: 90},
	}
	for _, item := range settings {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO mgmt_system_settings (
				setting_key, setting_group, label, value_text, input_type, sort_order
			)
			VALUES (?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				setting_group=VALUES(setting_group),
				label=VALUES(label),
				input_type=VALUES(input_type),
				sort_order=VALUES(sort_order)
		`, item.Key, item.Group, item.Label, item.Value, item.InputType, item.SortOrder); err != nil {
			return fmt.Errorf("seed system setting %s: %w", item.Key, err)
		}
	}

	dictionaries := []seededDictionaryItem{
		{Category: "logistics_provider", Code: "SF", Label: "顺丰速运", Description: "常用快递/物流承运商", SortOrder: 10},
		{Category: "logistics_provider", Code: "JD", Label: "京东物流", Description: "常用快递/物流承运商", SortOrder: 20},
		{Category: "logistics_provider", Code: "ZTO", Label: "中通快递", Description: "常用快递/物流承运商", SortOrder: 30},
		{Category: "logistics_provider", Code: "YTO", Label: "圆通速递", Description: "常用快递/物流承运商", SortOrder: 40},
		{Category: "logistics_provider", Code: "STO", Label: "申通快递", Description: "常用快递/物流承运商", SortOrder: 50},
		{Category: "logistics_provider", Code: "YUNDA", Label: "韵达快递", Description: "常用快递/物流承运商", SortOrder: 60},
		{Category: "logistics_provider", Code: "JT", Label: "极兔速递", Description: "常用快递/物流承运商", SortOrder: 70},
		{Category: "logistics_provider", Code: "DEPPON", Label: "德邦物流", Description: "常用快递/物流承运商", SortOrder: 80},
		{Category: "logistics_provider", Code: "EMS", Label: "EMS", Description: "邮政特快专递", SortOrder: 90},
		{Category: "logistics_provider", Code: "CHINA_POST", Label: "中国邮政", Description: "中国邮政寄递", SortOrder: 100},

		{Category: "product_unit", Code: "piece", Label: "件", Description: "通用商品单位", SortOrder: 10},
		{Category: "product_unit", Code: "unit", Label: "台", Description: "设备常用单位", SortOrder: 20},
		{Category: "product_unit", Code: "each", Label: "个", Description: "通用商品单位", SortOrder: 30},
		{Category: "product_unit", Code: "set", Label: "套", Description: "成套商品", SortOrder: 40},
		{Category: "product_unit", Code: "box", Label: "盒", Description: "包装单位", SortOrder: 50},
		{Category: "product_unit", Code: "carton", Label: "箱", Description: "包装单位", SortOrder: 60},
		{Category: "product_unit", Code: "pack", Label: "包", Description: "包装单位", SortOrder: 70},
		{Category: "product_unit", Code: "bag", Label: "袋", Description: "包装单位", SortOrder: 80},
		{Category: "product_unit", Code: "bottle", Label: "瓶", Description: "包装单位", SortOrder: 90},
		{Category: "product_unit", Code: "can", Label: "罐", Description: "包装单位", SortOrder: 100},
		{Category: "product_unit", Code: "bucket", Label: "桶", Description: "包装单位", SortOrder: 110},
		{Category: "product_unit", Code: "stick", Label: "支", Description: "计件单位", SortOrder: 120},
		{Category: "product_unit", Code: "sheet", Label: "张", Description: "平面材料单位", SortOrder: 130},
		{Category: "product_unit", Code: "slice", Label: "片", Description: "计件单位", SortOrder: 140},
		{Category: "product_unit", Code: "roll", Label: "卷", Description: "卷材单位", SortOrder: 150},
		{Category: "product_unit", Code: "group", Label: "组", Description: "组合单位", SortOrder: 160},
		{Category: "product_unit", Code: "pair", Label: "对", Description: "成对商品", SortOrder: 170},
		{Category: "product_unit", Code: "pair2", Label: "双", Description: "成双商品", SortOrder: 180},
		{Category: "product_unit", Code: "book", Label: "本", Description: "书册单位", SortOrder: 190},
		{Category: "product_unit", Code: "root", Label: "根", Description: "条状物单位", SortOrder: 200},
		{Category: "product_unit", Code: "meter", Label: "米", Description: "长度单位", SortOrder: 210},
		{Category: "product_unit", Code: "centimeter", Label: "厘米", Description: "长度单位", SortOrder: 220},
		{Category: "product_unit", Code: "sqm", Label: "平方米", Description: "面积单位", SortOrder: 230},
		{Category: "product_unit", Code: "gram", Label: "克", Description: "重量单位", SortOrder: 240},
		{Category: "product_unit", Code: "kilogram", Label: "千克", Description: "重量单位", SortOrder: 250},
		{Category: "product_unit", Code: "jin", Label: "斤", Description: "重量单位", SortOrder: 260},
		{Category: "product_unit", Code: "ton", Label: "吨", Description: "重量单位", SortOrder: 270},
		{Category: "product_unit", Code: "liter", Label: "升", Description: "容量单位", SortOrder: 280},
		{Category: "product_unit", Code: "milliliter", Label: "毫升", Description: "容量单位", SortOrder: 290},
	}
	for _, item := range dictionaries {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO mgmt_system_dictionary_items (
				category, code, label, description, sort_order, enabled, system_seeded
			)
			VALUES (?, ?, ?, ?, ?, 1, 1)
			ON DUPLICATE KEY UPDATE
				system_seeded=1
		`, item.Category, item.Code, item.Label, item.Description, item.SortOrder); err != nil {
			return fmt.Errorf("seed system dictionary %s/%s: %w", item.Category, item.Code, err)
		}
	}
	return nil
}

func (s *Store) ListSystemSettings(ctx context.Context) ([]model.SystemSetting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT setting_key, setting_group, label, value_text, input_type,
		       sort_order, updated_by_user_id, updated_at
		FROM mgmt_system_settings
		ORDER BY setting_group ASC, sort_order ASC, setting_key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.SystemSetting, 0)
	for rows.Next() {
		var item model.SystemSetting
		if err := rows.Scan(
			&item.Key,
			&item.Group,
			&item.Label,
			&item.Value,
			&item.InputType,
			&item.SortOrder,
			&item.UpdatedByUserID,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateSystemSettings(
	ctx context.Context,
	updates []model.SystemSettingUpdate,
	actorUserID int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, update := range updates {
		key := strings.TrimSpace(update.Key)
		if key == "" {
			continue
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE mgmt_system_settings
			SET value_text=?, updated_by_user_id=?
			WHERE setting_key=?
		`, update.Value, actorUserID, key)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			var exists int
			if err := tx.QueryRowContext(
				ctx,
				"SELECT 1 FROM mgmt_system_settings WHERE setting_key=? LIMIT 1",
				key,
			).Scan(&exists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("unknown system setting: %s", key)
				}
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) ListSystemDictionaryItems(
	ctx context.Context,
	category string,
	includeDisabled bool,
) ([]model.SystemDictionaryItem, error) {
	query := `
		SELECT id, category, code, label, description, sort_order,
		       enabled, system_seeded, created_at, updated_at
		FROM mgmt_system_dictionary_items
		WHERE 1=1
	`
	args := make([]any, 0, 1)
	if strings.TrimSpace(category) != "" {
		query += " AND category=?"
		args = append(args, strings.TrimSpace(category))
	}
	if !includeDisabled {
		query += " AND enabled=1"
	}
	query += " ORDER BY category ASC, sort_order ASC, id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.SystemDictionaryItem, 0)
	for rows.Next() {
		var item model.SystemDictionaryItem
		if err := rows.Scan(
			&item.ID,
			&item.Category,
			&item.Code,
			&item.Label,
			&item.Description,
			&item.SortOrder,
			&item.Enabled,
			&item.SystemSeeded,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateSystemDictionaryItem(
	ctx context.Context,
	input model.SystemDictionaryItemInput,
) (model.SystemDictionaryItem, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_system_dictionary_items (
			category, code, label, description, sort_order, enabled, system_seeded
		)
		VALUES (?, ?, ?, ?, ?, ?, 0)
	`,
		strings.TrimSpace(input.Category),
		strings.TrimSpace(input.Code),
		strings.TrimSpace(input.Label),
		strings.TrimSpace(input.Description),
		input.SortOrder,
		input.Enabled,
	)
	if err != nil {
		return model.SystemDictionaryItem{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.SystemDictionaryItem{}, err
	}
	return s.GetSystemDictionaryItem(ctx, id)
}

func (s *Store) GetSystemDictionaryItem(
	ctx context.Context,
	id int64,
) (model.SystemDictionaryItem, error) {
	var item model.SystemDictionaryItem
	err := s.db.QueryRowContext(ctx, `
		SELECT id, category, code, label, description, sort_order,
		       enabled, system_seeded, created_at, updated_at
		FROM mgmt_system_dictionary_items
		WHERE id=?
	`, id).Scan(
		&item.ID,
		&item.Category,
		&item.Code,
		&item.Label,
		&item.Description,
		&item.SortOrder,
		&item.Enabled,
		&item.SystemSeeded,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (s *Store) UpdateSystemDictionaryItem(
	ctx context.Context,
	id int64,
	input model.SystemDictionaryItemInput,
) (model.SystemDictionaryItem, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE mgmt_system_dictionary_items
		SET category=?, code=?, label=?, description=?, sort_order=?, enabled=?
		WHERE id=?
	`,
		strings.TrimSpace(input.Category),
		strings.TrimSpace(input.Code),
		strings.TrimSpace(input.Label),
		strings.TrimSpace(input.Description),
		input.SortOrder,
		input.Enabled,
		id,
	)
	if err != nil {
		return model.SystemDictionaryItem{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return model.SystemDictionaryItem{}, sql.ErrNoRows
	}
	return s.GetSystemDictionaryItem(ctx, id)
}

func (s *Store) SystemSettingsDashboard(ctx context.Context) (model.SystemSettingsDashboard, error) {
	settings, err := s.ListSystemSettings(ctx)
	if err != nil {
		return model.SystemSettingsDashboard{}, err
	}
	items, err := s.ListSystemDictionaryItems(ctx, "", true)
	if err != nil {
		return model.SystemSettingsDashboard{}, err
	}
	warehouses, err := s.ListAllWarehouses(ctx)
	if err != nil {
		return model.SystemSettingsDashboard{}, err
	}
	membershipRoomLimits, err := s.ListMembershipRoomLimits(ctx)
	if err != nil {
		return model.SystemSettingsDashboard{}, err
	}
	dictionaries := make(map[string][]model.SystemDictionaryItem)
	for _, item := range items {
		dictionaries[item.Category] = append(dictionaries[item.Category], item)
	}
	return model.SystemSettingsDashboard{
		Settings:             settings,
		Dictionaries:         dictionaries,
		Warehouses:           warehouses,
		MembershipRoomLimits: membershipRoomLimits,
	}, nil
}

func (s *Store) PublicSystemConfig(ctx context.Context) (model.PublicSystemConfig, error) {
	settings, err := s.ListSystemSettings(ctx)
	if err != nil {
		return model.PublicSystemConfig{}, err
	}
	values := make(map[string]string, len(settings))
	for _, item := range settings {
		values[item.Key] = item.Value
	}
	footerEnabled := true
	if raw, ok := values["footer_enabled"]; ok {
		if parsed, parseErr := strconv.ParseBool(strings.TrimSpace(raw)); parseErr == nil {
			footerEnabled = parsed
		}
	}
	internalAgentName := strings.TrimSpace(values["internal_agent_name"])
	if internalAgentName == "" {
		internalAgentName = "小蓝工作搭子"
	}
	clientAgentName := strings.TrimSpace(values["client_agent_name"])
	if clientAgentName == "" {
		clientAgentName = "小蓝直播搭子"
	}
	return model.PublicSystemConfig{
		SiteName:          values["site_name"],
		InternalAgentName: internalAgentName,
		ClientAgentName:   clientAgentName,
		FooterEnabled:     footerEnabled,
		FooterCopyright:   values["footer_copyright"],
		FooterICPText:     values["footer_icp_text"],
		FooterICPURL:      values["footer_icp_url"],
		FooterPoliceText:  values["footer_police_text"],
		FooterPoliceURL:   values["footer_police_url"],
		FooterReportText:  values["footer_report_text"],
		FooterReportURL:   values["footer_report_url"],
		FooterExtraText:   values["footer_extra_text"],
	}, nil
}
