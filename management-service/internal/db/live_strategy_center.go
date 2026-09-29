package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

func defaultLiveStrategyCenter(tenantID int64) model.LiveStrategyCenterConfig {
	rules := []model.LiveStrategyRule{
		{Category: "interrupt", Key: "read_comment_softly", Name: "小声读一次弹幕", Description: "先轻声复述观众问题，再自然回答。", Enabled: true, BaseProbability: 20, MinProbability: 10, SystemDefault: true},
		{Category: "interrupt", Key: "hard_cut", Name: "直接硬切", Description: "紧急、高价值问题直接切入回答。", Enabled: true, BaseProbability: 20, MinProbability: 10, SystemDefault: true},
		{Category: "interrupt", Key: "ask_controller", Name: "询问中控", Description: "需要确认事实时先向中控求证。", Enabled: true, BaseProbability: 10, MinProbability: 10, SystemDefault: true},
		{Category: "interrupt", Key: "thinking_pause", Name: "短暂停顿", Description: "保留短暂思考停顿，减少机器式秒答。", Enabled: true, BaseProbability: 20, MinProbability: 10, SystemDefault: true},
		{Category: "interrupt", Key: "repeat_confirm", Name: "重复确认", Description: "用一句自然确认重新说清观众问题。", Enabled: true, BaseProbability: 30, MinProbability: 10, SystemDefault: true},

		{Category: "resume", Key: "DIRECT", Name: "直接续播", Description: "从安全句界继续主线。", Enabled: true, BaseProbability: 20, MinProbability: 20, SystemDefault: true},
		{Category: "resume", Key: "BRIDGE", Name: "桥接恢复", Description: "先说自然桥接，再回主线。", Enabled: true, BaseProbability: 20, MinProbability: 20, SystemDefault: true},
		{Category: "resume", Key: "FUSION_SKIP", Name: "融合跳过", Description: "回答已覆盖后续内容时跳过重复语义。", Enabled: true, BaseProbability: 20, MinProbability: 20, SystemDefault: true},
		{Category: "resume", Key: "CROSS_RESUME", Name: "跨段恢复", Description: "跨过多个已覆盖段，落到新的独立入口。", Enabled: true, BaseProbability: 20, MinProbability: 20, SystemDefault: true},
		{Category: "resume", Key: "RE_ANCHOR", Name: "重新锚定", Description: "长中断后重建上下文再继续。", Enabled: true, BaseProbability: 20, MinProbability: 20, SystemDefault: true},
		{Category: "resume", Key: "SWITCH_PLAN", Name: "切换主线方案", Description: "原主线已失效时切换到新主线；安全条件可强制触发。", Enabled: false, BaseProbability: 0, MinProbability: 20, SystemDefault: true},

		{Category: "interaction", Key: "welcome_named", Name: "点名欢迎", Description: "低流速时优先点名欢迎新人。", Enabled: true, BaseProbability: 10, MinProbability: 5, SystemDefault: true, Config: map[string]any{"traffic_sensitive": true}},
		{Category: "interaction", Key: "welcome_batch", Name: "打包欢迎", Description: "进房较快时聚合欢迎一批新人。", Enabled: true, BaseProbability: 15, MinProbability: 5, SystemDefault: true, Config: map[string]any{"traffic_sensitive": true}},
		{Category: "interaction", Key: "reply_like", Name: "回应点赞", Description: "适度感谢点赞，不逐条刷屏。", Enabled: true, BaseProbability: 8, MinProbability: 5, SystemDefault: true},
		{Category: "interaction", Key: "reply_follow", Name: "回应关注", Description: "对新增关注做轻量感谢。", Enabled: true, BaseProbability: 8, MinProbability: 5, SystemDefault: true},
		{Category: "interaction", Key: "reply_chat", Name: "回复弹幕", Description: "对可回答弹幕进行回复，是核心互动策略。", Enabled: true, BaseProbability: 50, MinProbability: 20, SystemDefault: true},
	}
	return model.LiveStrategyCenterConfig{
		TenantID:       tenantID,
		Rules:          rules,
		AddressingMode: "system",
		Addressing: []model.LiveAddressingOption{
			{Key: "friend", Text: "朋友", Enabled: true, Probability: 40, SystemDefault: true},
			{Key: "boss", Text: "老板", Enabled: true, Probability: 20, SystemDefault: true},
			{Key: "everyone", Text: "大家", Enabled: true, Probability: 20, SystemDefault: true},
			{Key: "baozi", Text: "宝子", Enabled: true, Probability: 20, SystemDefault: true},
		},
	}
}

func (s *Store) GetLiveStrategyCenter(ctx context.Context, tenantID int64) (model.LiveStrategyCenterConfig, error) {
	load := func(id int64) (model.LiveStrategyCenterConfig, error) {
		var raw string
		var updatedBy sql.NullInt64
		var updatedAt sql.NullTime
		err := s.db.QueryRowContext(ctx, `
			SELECT CAST(config_json AS CHAR), updated_by_user_id, updated_at
			FROM live_strategy_center_configs
			WHERE tenant_id=?
		`, id).Scan(&raw, &updatedBy, &updatedAt)
		if err != nil {
			return model.LiveStrategyCenterConfig{}, err
		}
		item := defaultLiveStrategyCenter(tenantID)
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return model.LiveStrategyCenterConfig{}, err
		}
		item.TenantID = tenantID
		if updatedBy.Valid {
			v := updatedBy.Int64
			item.UpdatedBy = &v
		}
		if updatedAt.Valid {
			item.UpdatedAt = updatedAt.Time
		}
		return item, nil
	}

	if tenantID != 0 {
		global, globalErr := load(0)
		if globalErr != nil && !errors.Is(globalErr, sql.ErrNoRows) {
			return model.LiveStrategyCenterConfig{}, globalErr
		}
		local, localErr := load(tenantID)
		if localErr == nil {
			if globalErr == nil {
				local.Rules = global.Rules
				if !strings.EqualFold(strings.TrimSpace(local.AddressingMode), "custom") {
					local.AddressingMode = "system"
					local.Addressing = global.Addressing
				}
			}
			return local, nil
		}
		if !errors.Is(localErr, sql.ErrNoRows) {
			return model.LiveStrategyCenterConfig{}, localErr
		}
		if globalErr == nil {
			global.TenantID = tenantID
			return global, nil
		}
		return defaultLiveStrategyCenter(tenantID), nil
	}
	item, err := load(tenantID)
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.LiveStrategyCenterConfig{}, err
	}
	if tenantID != 0 {
		if inherited, inheritedErr := load(0); inheritedErr == nil {
			inherited.TenantID = tenantID
			return inherited, nil
		} else if !errors.Is(inheritedErr, sql.ErrNoRows) {
			return model.LiveStrategyCenterConfig{}, inheritedErr
		}
	}
	return defaultLiveStrategyCenter(tenantID), nil
}

func (s *Store) UpsertLiveStrategyCenter(ctx context.Context, tenantID, userID int64, input model.LiveStrategyCenterInput) (model.LiveStrategyCenterConfig, error) {
	item := model.LiveStrategyCenterConfig{
		TenantID:       tenantID,
		Rules:          input.Rules,
		AddressingMode: strings.TrimSpace(input.AddressingMode),
		Addressing:     input.Addressing,
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return model.LiveStrategyCenterConfig{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO live_strategy_center_configs (tenant_id, config_json, updated_by_user_id)
		VALUES (?, CAST(? AS JSON), ?)
		ON DUPLICATE KEY UPDATE
			config_json=VALUES(config_json),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, tenantID, string(raw), userID)
	if err != nil {
		return model.LiveStrategyCenterConfig{}, err
	}
	return s.GetLiveStrategyCenter(ctx, tenantID)
}
