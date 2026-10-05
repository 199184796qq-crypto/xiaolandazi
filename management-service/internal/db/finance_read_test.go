package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/model"
)

func TestRetryFinanceRead(t *testing.T) {
	for _, temporary := range []error{driver.ErrBadConn, &mysql.MySQLError{Number: 1205}, &mysql.MySQLError{Number: 1213}} {
		attempts := 0
		item, err := retryFinanceRead(context.Background(), func() (model.FinanceDashboard, error) {
			attempts++
			if attempts < 3 {
				return model.FinanceDashboard{}, temporary
			}
			return model.FinanceDashboard{CashBalanceCents: 1600}, nil
		})
		if err != nil || attempts != 3 || item.CashBalanceCents != 1600 {
			t.Fatalf("transient read: attempts=%d err=%v", attempts, err)
		}
	}
	attempts := 0
	_, err := retryFinanceRead(context.Background(), func() (model.FinanceDashboard, error) {
		attempts++
		return model.FinanceDashboard{}, driver.ErrBadConn
	})
	if attempts != 3 || !errors.Is(err, driver.ErrBadConn) {
		t.Fatal("retry must be bounded", attempts, err)
	}
	logical := errors.New("non-transient scan failure")
	attempts = 0
	_, err = retryFinanceRead(context.Background(), func() (model.FinanceDashboard, error) {
		attempts++
		return model.FinanceDashboard{}, logical
	})
	if attempts != 1 || !errors.Is(err, logical) {
		t.Fatal("logical errors must not retry", attempts, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	attempts = 0
	_, err = retryFinanceRead(ctx, func() (model.FinanceDashboard, error) {
		attempts++
		cancel()
		return model.FinanceDashboard{}, context.Canceled
	})
	if attempts != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request retried", attempts, err)
	}
}
