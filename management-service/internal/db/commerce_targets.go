package db

import (
	"context"
)

type CommerceRuleTarget struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (s *Store) CommerceRuleTargets(ctx context.Context) ([]CommerceRuleTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,'membership' FROM catalog_membership_plans UNION ALL SELECT id,name,'time_card' FROM catalog_time_card_products UNION ALL SELECT id,name,'device' FROM catalog_device_products UNION ALL SELECT id,name,'campaign' FROM mkt_campaigns ORDER BY 3,1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CommerceRuleTarget{}
	for rows.Next() {
		var t CommerceRuleTarget
		if err = rows.Scan(&t.ID, &t.Name, &t.Kind); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}
