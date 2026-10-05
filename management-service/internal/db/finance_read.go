package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/model"
)

// Retrying this read cannot resubmit a payment/refund or alter wallet money.
func retryFinanceRead(ctx context.Context, read func() (model.FinanceDashboard, error)) (model.FinanceDashboard, error) {
	for attempt := 0; ; attempt++ {
		item, err := read()
		if err == nil || attempt >= 2 || !transientFinanceRead(err) || ctx.Err() != nil {
			return item, err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return model.FinanceDashboard{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func transientFinanceRead(err error) bool {
	if errors.Is(err, driver.ErrBadConn) {
		return true
	}
	var sqlErr *mysql.MySQLError
	return errors.As(err, &sqlErr) && (sqlErr.Number == 1205 || sqlErr.Number == 1213)
}

func (s *Store) GetFinanceDashboard(ctx context.Context, tenantID int64, limit int) (model.FinanceDashboard, error) {
	return retryFinanceRead(ctx, func() (model.FinanceDashboard, error) { return s.getFinanceDashboardOnce(ctx, tenantID, limit) })
}
