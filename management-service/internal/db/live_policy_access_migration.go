package db

import (
	"context"
	"database/sql"
)

// A targeted, idempotent upgrade: do not rerun the global staff seeder and reset
// other departments while another developer is editing their roles/workflows.
func (s *Store) migrateLivePolicyAccessSeparation(ctx context.Context) error {
	const key = "staff_live_policy_access_separation_v2"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	err = tx.QueryRowContext(ctx, "SELECT meta_value FROM staff_meta WHERE meta_key=? LIMIT 1", key).Scan(&state)
	if err == nil && state == "done" {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	permissionSeeds := []struct {
		code, module, action, description string
	}{
		{"livepolicy.manage_l1", "livepolicy", "manage_l1", "维护并发布系统底层直播规则"},
		{"livepolicy.manage_l3_authorized", "livepolicy", "manage_l3_authorized", "经客户授权后代维护指定直播间 L3 策略"},
		{"livecoach.anchor_authorized", "livecoach", "anchor_authorized", "经客户授权后协助指定直播间主播训练"},
		{"livevoice.clone_authorized", "livevoice", "clone_authorized", "经客户授权后协助指定直播间声音复刻"},
	}
	for _, item := range permissionSeeds {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_permissions (code, module, action, description)
			VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				module=VALUES(module),
				action=VALUES(action),
				description=VALUES(description)
		`, item.code, item.module, item.action, item.description); err != nil {
			return err
		}
	}
	rolePermissions := map[string][]string{
		"live_operations_manager": {
			"livepolicy.view",
			"livepolicy.manage_l1",
			"livepolicy.manage_l2",
			"livecoach.anchor_authorized",
			"livevoice.clone_authorized",
		},
		"live_operations_staff": {
			"livepolicy.view",
			"livepolicy.manage_l2",
			"livepolicy.manage_l3_authorized",
			"livecoach.anchor_authorized",
			"livevoice.clone_authorized",
		},
	}
	for roleCode, permissions := range rolePermissions {
		for _, permissionCode := range permissions {
			if _, err := tx.ExecContext(ctx, `
				INSERT IGNORE INTO staff_role_permissions (role_id, permission_id)
				SELECT r.id, p.id
				FROM staff_roles r
				JOIN staff_permissions p ON p.code=?
				WHERE r.code=?
			`, permissionCode, roleCode); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO staff_role_permissions (role_id, permission_id)
		SELECT rp.role_id, l2.id
		FROM staff_role_permissions rp
		JOIN staff_permissions l1 ON l1.id=rp.permission_id AND l1.code='livepolicy.manage_l1'
		JOIN staff_permissions l2 ON l2.code='livepolicy.manage_l2'
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE delegated FROM staff_role_permissions delegated
		JOIN staff_permissions l3 ON l3.id=delegated.permission_id AND l3.code='livepolicy.manage_l3_authorized'
		JOIN staff_role_permissions global_role ON global_role.role_id=delegated.role_id
		JOIN staff_permissions l1 ON l1.id=global_role.permission_id AND l1.code IN ('livepolicy.manage_l1', '*')
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO staff_meta (meta_key, meta_value) VALUES (?, 'done') ON DUPLICATE KEY UPDATE meta_value='done'", key,
	); err != nil {
		return err
	}
	return tx.Commit()
}
