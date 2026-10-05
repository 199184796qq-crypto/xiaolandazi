package db

import (
	"context"
	"fmt"
	"strings"
)

// RoomCustomerContact is fetched only after checking access to a specific room.
// Phone numbers must never be included in the room list or browser room cache.
type RoomCustomerContact struct {
	TenantID int64  `json:"tenant_id"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
}

const roomCustomerJoin = `
	FROM mgmt_tenants t
	LEFT JOIN mgmt_users u ON u.id=(
		SELECT owner.id FROM mgmt_users owner
		WHERE owner.tenant_id=t.id AND owner.role='customer'
		ORDER BY (owner.status='active') DESC, owner.id ASC LIMIT 1
	)
`

const roomCustomerName = `COALESCE(NULLIF(TRIM(u.display_name),''),NULLIF(TRIM(t.name),''),NULLIF(TRIM(u.username),''),'未填写客户名称')`

func (s *Store) GetRoomCustomerNames(ctx context.Context, tenantIDs []int64) (map[int64]string, error) {
	names := make(map[int64]string)
	seen := make(map[int64]bool)
	args := make([]any, 0, len(tenantIDs))
	marks := make([]string, 0, len(tenantIDs))
	for _, id := range tenantIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		args = append(args, id)
		marks = append(marks, "?")
	}
	if len(args) == 0 {
		return names, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT t.id,"+roomCustomerName+roomCustomerJoin+" WHERE t.id IN ("+strings.Join(marks, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

func (s *Store) GetRoomCustomerContact(ctx context.Context, tenantID int64) (RoomCustomerContact, error) {
	if tenantID <= 0 {
		return RoomCustomerContact{}, fmt.Errorf("invalid tenant id")
	}
	var item RoomCustomerContact
	err := s.db.QueryRowContext(ctx, "SELECT t.id,"+roomCustomerName+",COALESCE(u.phone,'')"+roomCustomerJoin+" WHERE t.id=?", tenantID).Scan(&item.TenantID, &item.Name, &item.Phone)
	return item, err
}
