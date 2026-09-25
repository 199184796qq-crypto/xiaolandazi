package db

import (
	"context"
	"livecompanion/management/internal/model"
	"time"
)

type CustomerBusinessAccount struct {
	TenantID  int64     `json:"tenant_id"`
	Name      string    `json:"name"`
	Username  string    `json:"username"`
	Qualified bool      `json:"qualified"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CustomerBusinessAccounts(ctx context.Context, sc model.CustomerBusinessScope, search, qualification string, page, size int) ([]CustomerBusinessAccount, int64, error) {
	page, size = businessPage(page, size)
	w, a := customerScopeSQL(sc, "t.id")
	w = " WHERE t.org_type='customer' AND (" + w + ")"
	if qualification == "unconfirmed" {
		w += " AND c.tenant_id IS NULL"
	}
	if qualification == "qualified" {
		w += " AND c.tenant_id IS NOT NULL"
	}
	if search != "" {
		w += " AND (t.name LIKE ? ESCAPE '!' OR u.username LIKE ? ESCAPE '!')"
		a = append(a, businessLike(search), businessLike(search))
	}
	from := ` FROM mgmt_tenants t JOIN mgmt_users u ON u.id=(SELECT MIN(u2.id) FROM mgmt_users u2 WHERE u2.tenant_id=t.id AND u2.role='customer') LEFT JOIN fin_customer_confirmations c ON c.tenant_id=t.id`
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+from+w, a...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT t.id,t.name,u.username,c.tenant_id IS NOT NULL,t.created_at"+from+w+" ORDER BY t.id DESC LIMIT ? OFFSET ?", append(append([]any{}, a...), size, (page-1)*size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []CustomerBusinessAccount{}
	for rows.Next() {
		var v CustomerBusinessAccount
		if err = rows.Scan(&v.TenantID, &v.Name, &v.Username, &v.Qualified, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
