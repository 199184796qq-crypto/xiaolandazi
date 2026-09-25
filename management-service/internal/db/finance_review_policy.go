package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrFinanceDistinctReviewer = errors.New("当前开启强制分人审核，经办人与审核人必须不同")
var ErrFinanceReviewPolicyUnavailable = errors.New("财务审核配置读取失败，请刷新后重试，未执行审核")

func ValidateFinanceReviewSetting(key, value string) error {
	if strings.TrimSpace(key) == model.FinanceDistinctReviewerSetting && value != "true" && value != "false" {
		return errors.New("强制分人审核必须为 true 或 false")
	}
	return nil
}

func loadFinanceReviewPolicy(ctx context.Context, q customerBusinessDB, lock bool) (model.FinanceReviewPolicy, error) {
	policy := model.FinanceReviewPolicy{RequireDistinctReviewer: true}
	query := "SELECT value_text FROM mgmt_system_settings WHERE setting_key=?"
	if lock {
		// Keep the setting stable until the approval commits. Settings writes wait.
		query += " FOR SHARE"
	}
	var value string
	err := q.QueryRowContext(ctx, query, model.FinanceDistinctReviewerSetting).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	if err != nil || ValidateFinanceReviewSetting(model.FinanceDistinctReviewerSetting, value) != nil {
		return policy, ErrFinanceReviewPolicyUnavailable
	}
	policy.RequireDistinctReviewer = value == "true"
	return policy, nil
}

func (s *Store) FinanceReviewPolicy(ctx context.Context) (model.FinanceReviewPolicy, error) {
	return loadFinanceReviewPolicy(ctx, s.db, false)
}
