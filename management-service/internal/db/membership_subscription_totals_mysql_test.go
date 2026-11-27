package db

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"
)

func TestMembershipSubscriptionTotalsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	exec(`INSERT INTO biz_orders(id, order_no, tenant_id, order_type, status, paid_amount_cents, paid_at)
		VALUES (1,'before',1,'membership','paid',1300,'2026-10-31 15:59:59.999'),
		       (2,'start',1,'membership','paid',1700,'2026-10-31 16:00:00.000'),
		       (3,'free-renewal',1,'membership','fulfilled',0,'2026-11-02 04:00:00.000'),
		       (4,'pending',1,'membership','pending',0,NULL),
		       (5,'after',1,'membership','paid',9000,'2026-11-30 16:00:00.000'),
		       (6,'card',1,'time_card','paid',5888,'2026-11-02 04:00:00.000'),
		       (7,'other-plan',1,'membership','completed',8000,'2026-11-02 04:00:00.000'),
		       (8,'no-payment-time',1,'membership','paid',2300,NULL)`)
	exec(`INSERT INTO biz_order_items(order_id, product_type, product_id, product_version_id, product_name_snapshot)
		VALUES (1,'membership',11,22,'基础会员'),
		       (2,'membership',11,23,'基础会员'),
		       (2,'membership',11,23,'基础会员'),
		       (3,'membership',11,23,'基础会员'),
		       (4,'membership',11,23,'基础会员'),
		       (5,'membership',11,23,'基础会员'),
		       (6,'time_card',11,23,'时长卡'),
		       (7,'membership',12,24,'标准会员'),
		       (8,'membership',11,23,'基础会员')`)
	start := time.Date(2026, time.October, 31, 16, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 30)
	check := func(from, until time.Time, count int64, amount uint64) {
		t.Helper()
		totals, err := s.MembershipSubscriptionTotals(ctx, from, until)
		want := map[int64]MembershipSubscriptionTotal{11: {Count: count, AmountCents: amount}, 12: {Count: 1, AmountCents: 8000}}
		if err != nil || !maps.Equal(totals, want) {
			t.Fatalf("subscriptions = %+v, err = %v, want %+v", totals, err, want)
		}
	}
	check(start, end, 2, 1700)
	check(time.Time{}, time.Time{}, 4, 12000)
}
