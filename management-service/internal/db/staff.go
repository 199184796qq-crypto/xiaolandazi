package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"livecompanion/management/internal/model"
)

const staffSeedVersion = "staff_seed_v17"

var ErrMutuallyExclusiveStaffRoles = errors.New("mutually exclusive staff roles")

var staffPermissionSeeds = []struct {
	Code        string
	Module      string
	Action      string
	Description string
}{
	{"system.architecture.view", "system", "view_architecture", "查看系统业务与组织架构"},
	{"system.settings.view", "system", "view_settings", "查看本人权限范围内的系统设定"},
	{"system.settings.liveops.manage", "system", "manage_liveops_settings", "维护直播运维相关系统设定"},
	{"system.settings.inventory.manage", "system", "manage_inventory_settings", "维护仓库、物流与产品基础字典"},
	{"staff.group.view", "staff", "view_groups", "查看部门"},
	{"staff.group.manage", "staff", "manage_groups", "管理部门"},
	{"staff.role.view", "staff", "view_roles", "查看角色"},
	{"staff.role.manage", "staff", "manage_roles", "管理角色与权限"},
	{"staff.permission.view", "staff", "view_permissions", "查看权限目录"},
	{"staff.employee.view", "staff", "view_employees", "查看员工"},
	{"staff.employee.create", "staff", "create_employee", "创建员工"},
	{"staff.employee.disable", "staff", "disable_employee", "停用员工"},
	{"staff.employee.role_assign", "staff", "assign_role", "分配员工已有角色"},
	{"customer.view_all", "customer", "view_all", "查看全部终端"},
	{"customer.password_reset", "customer", "password_reset", "按权限范围重置终端密码"},
	{"agent.view_all", "agent", "view_all", "查看全部代理"},
	{"agent.level.manage", "agent", "manage_level", "管理代理等级政策"},
	{"agent.contract.manage", "agent", "manage_contract", "管理代理合同"},
	{"agent.exit.finalize", "agent", "finalize_exit", "完成代理退出清算"},
	{"sales.view_all", "sales", "view_all", "查看全部销售人员"},
	{"sales.customer.view_assigned", "sales", "view_assigned_customers", "查看分配给自己的终端"},
	{"sales.customer.view_group", "sales", "view_group_customers", "查看销售部终端"},
	{"sales.assignment.manage", "sales", "manage_assignment", "管理销售终端分配"},
	{"liveops.view_all", "liveops", "view_all", "查看全部直播运维对象"},
	{"liveops.configure", "liveops", "configure", "配置终端直播间与直播设备"},
	{"liveops.ticket.manage", "liveops", "manage_ticket", "管理直播运维工单"},
	{"liveops.room_quota.view", "liveops", "view_room_quota", "查看终端直播间数量额度"},
	{"liveops.room_quota.manage", "liveops", "manage_room_quota", "调整终端直播间数量额度"},
	{"livepolicy.view", "livepolicy", "view", "查看系统与行业直播策略"},
	{"livepolicy.manage_l2", "livepolicy", "manage_l2", "维护并发布行业默认直播策略"},
	{"livepolicy.manage_l1", "livepolicy", "manage_l1", "维护并发布系统底层直播规则"},
	{"livepolicy.manage_l3_authorized", "livepolicy", "manage_l3_authorized", "经客户授权后代维护指定直播间 L3 策略"},
	{"livecoach.anchor_authorized", "livecoach", "anchor_authorized", "经客户授权后协助指定直播间主播训练"},
	{"livevoice.clone_authorized", "livevoice", "clone_authorized", "经客户授权后协助指定直播间声音复刻"},
	{"finance.dashboard.view", "finance", "view_dashboard", "查看财务数据"},
	{"finance.recharge.create", "finance", "create_recharge", "发起充值"},
	{"finance.recharge.approve", "finance", "approve_recharge", "审核充值"},
	{"finance.refund.create", "finance", "create_refund", "发起退款"},
	{"finance.refund.approve", "finance", "approve_refund", "审核退款"},
	{"finance.reward.grant", "finance", "grant_reward", "发起奖励发放"},
	{"finance.reward.approve", "finance", "approve_reward", "审核奖励发放"},
	{"finance.membership.adjust", "finance", "adjust_membership", "调整终端会员权益"},
	{"finance.resource.view", "finance", "view_ai_time", "查看终端或代理 AI 时长"},
	{"finance.resource.adjust", "finance", "adjust_ai_time", "超级管理员紧急调整终端或代理 AI 时长"},
	{"finance.ai_time.approve", "finance", "approve_ai_time_grant", "审核营销运维发起的 AI 时长增加申请"},
	{"finance.operating.view", "finance", "view_operating_finance", "查看设备采购、物流、报废处置和 Token 采购经营收支"},
	{"finance.operating.manage", "finance", "manage_operating_finance", "登记 Token 采购等公司经营成本"},
	{"finance.settlement.create", "finance", "create_settlement", "生成收益结算批次"},
	{"finance.settlement.approve", "finance", "approve_settlement", "审核收益结算批次"},
	{"finance.settlement.pay", "finance", "pay_settlement", "确认收益结算支付"},
	{"commercial.membership.view", "commercial", "view_membership", "查看会员方案"},
	{"commercial.membership.manage", "commercial", "manage_membership", "管理会员方案"},
	{"commercial.time_card.view", "commercial", "view_time_card", "查看时长卡商品"},
	{"commercial.time_card.manage", "commercial", "manage_time_card", "新增、修改、归档及上下架时长卡"},
	{"commercial.device.view", "commercial", "view_device_product", "查看设备商品及可售库存"},
	{"commercial.device.listing.manage", "commercial", "manage_device_listing", "控制设备商品上架与下架"},
	{"commercial.marketing.view", "commercial", "view_marketing", "查看营销活动与标的挂链"},
	{"commercial.marketing.manage", "commercial", "manage_marketing", "创建、修改、启停和归档营销活动"},
	{"commercial.referral.view", "commercial", "view_referral_rules", "查看营销推荐奖励规则"},
	{"commercial.referral.manage", "commercial", "manage_referral_rules", "创建、修改和发布营销推荐奖励规则"},
	{"commercial.ai_time.view", "commercial", "view_ai_time", "查看代理与终端 AI 时长及流水"},
	{"commercial.ai_time.request", "commercial", "request_ai_time", "发起 AI 时长增加申请并提交财务审核"},
	{"finance.settlement_rules.view", "finance", "view_settlement_rules", "查看销售提成与代理返佣结算规则"},
	{"finance.settlement_rules.manage", "finance", "manage_settlement_rules", "创建、修改和发布销售提成与代理返佣结算规则"},
	{"inventory.view", "inventory", "view", "查看设备档案与库存"},
	{"inventory.manage", "inventory", "manage", "执行设备入库、出库、调拨与状态变更"},
	{"inventory.after_sales.view", "inventory", "view_after_sales", "查看设备售后维修单、维修状态与费用"},
	{"inventory.after_sales.manage", "inventory", "manage_after_sales", "受理并处理设备售后维修、费用与返还"},
	{"logistics.view", "logistics", "view", "查看设备物流与签收"},
	{"logistics.manage", "logistics", "manage", "创建物流单并更新物流签收状态"},
	{"after_sales.view", "after_sales", "view", "查看设备退换、维修、翻新与报废"},
	{"after_sales.manage", "after_sales", "manage", "处理设备退换、维修、翻新与报废"},
	{"resources.view", "resources", "view", "查看资源账户"},
	{"resources.adjust", "resources", "adjust", "调整资源账户"},
	{"invitations.view_all", "invitations", "view_all", "查看全系统邀请关系"},
	{"audit.view", "audit", "view", "查看审计日志"},
}

type staffRoleSeed struct {
	GroupCode        string
	Code             string
	Name             string
	Description      string
	IsGroupManager   bool
	DefaultScopeType string
	Permissions      []string
}

var staffRoleSeeds = []staffRoleSeed{
	{
		GroupCode:        "management",
		Code:             "management_manager",
		Name:             "管理部负责人",
		Description:      "查看整个系统架构，并可按已有部门新增、停用员工和分配已有角色，但不能修改部门或权限定义。",
		IsGroupManager:   true,
		DefaultScopeType: "all_internal",
		Permissions: []string{
			"system.architecture.view", "staff.group.view", "staff.role.view", "staff.permission.view",
			"staff.employee.view", "staff.employee.create", "staff.employee.disable", "staff.employee.role_assign",
			"customer.view_all", "customer.password_reset", "agent.view_all", "sales.view_all", "commercial.membership.view", "commercial.marketing.view", "commercial.referral.view", "livepolicy.view",
			"finance.resource.view", "finance.settlement_rules.view", "inventory.view", "logistics.view", "inventory.after_sales.view", "audit.view",
		},
	},
	{
		GroupCode:        "management",
		Code:             "management_staff",
		Name:             "管理部员工",
		Description:      "只读查看系统业务与组织架构，负责运营协调与问题追踪。",
		DefaultScopeType: "all_internal",
		Permissions: []string{
			"system.architecture.view", "staff.group.view", "staff.role.view", "staff.permission.view", "staff.employee.view",
			"customer.view_all", "agent.view_all", "sales.view_all", "commercial.membership.view", "commercial.marketing.view", "commercial.referral.view", "livepolicy.view",
			"finance.resource.view", "finance.settlement_rules.view", "inventory.view", "logistics.view", "inventory.after_sales.view", "audit.view",
		},
	},
	{
		GroupCode:        "finance",
		Code:             "finance_manager",
		Name:             "财务主管",
		Description:      "管理财务部员工，并执行财务操作与审核。",
		IsGroupManager:   true,
		DefaultScopeType: "group",
		Permissions: []string{
			"staff.group.view", "staff.role.view", "staff.employee.view", "staff.employee.create", "staff.employee.disable", "staff.employee.role_assign",
			"customer.view_all", "agent.view_all", "finance.dashboard.view", "finance.recharge.create", "finance.recharge.approve",
			"finance.refund.create", "finance.refund.approve", "finance.reward.grant", "finance.reward.approve",
			"finance.membership.adjust", "finance.resource.view", "finance.ai_time.approve", "finance.operating.view", "finance.operating.manage", "finance.settlement_rules.view", "finance.settlement_rules.manage", "finance.settlement.create", "finance.settlement.approve", "finance.settlement.pay",
			"audit.view",
		},
	},
	{
		GroupCode:        "finance",
		Code:             "finance_operator",
		Name:             "财务操作员",
		Description:      "发起充值、退款、奖励和会员调整；AI 时长由营销运维发起、财务负责审核。",
		DefaultScopeType: "self",
		Permissions: []string{
			"customer.view_all", "agent.view_all", "finance.dashboard.view", "finance.recharge.create", "finance.refund.create",
			"finance.reward.grant", "finance.membership.adjust", "finance.resource.view", "finance.operating.view", "finance.operating.manage", "finance.settlement_rules.view", "finance.settlement_rules.manage", "finance.settlement.create",
		},
	},
	{
		GroupCode:        "finance",
		Code:             "finance_reviewer",
		Name:             "财务审核员",
		Description:      "审核充值、退款、奖励、AI 时长增加申请并查看审计记录。",
		DefaultScopeType: "group",
		Permissions: []string{
			"finance.dashboard.view", "finance.resource.view", "finance.operating.view", "finance.settlement_rules.view", "finance.recharge.approve", "finance.refund.approve", "finance.reward.approve", "finance.ai_time.approve", "finance.settlement.approve", "audit.view",
		},
	},
	{
		GroupCode:        "sales",
		Code:             "sales_manager",
		Name:             "销售主管",
		Description:      "管理销售部成员、销售终端分配并查看销售部终端。",
		IsGroupManager:   true,
		DefaultScopeType: "group",
		Permissions: []string{
			"staff.group.view", "staff.role.view", "staff.employee.view", "staff.employee.create", "staff.employee.disable", "staff.employee.role_assign",
			"customer.view_all", "customer.password_reset", "sales.view_all", "sales.customer.view_group", "sales.assignment.manage", "audit.view",
		},
	},
	{
		GroupCode:        "sales",
		Code:             "sales_staff",
		Name:             "销售人员",
		Description:      "查看分配给自己的终端并维护销售关系。",
		DefaultScopeType: "assigned",
		Permissions: []string{
			"customer.view_all", "customer.password_reset", "sales.view_all",
			"sales.customer.view_assigned", "audit.view",
		},
	},
	{
		GroupCode:        "live_operations",
		Code:             "live_operations_manager",
		Name:             "营销运维负责人",
		Description:      "管理营销运维部员工；负责直播配置、活动营销、时长卡运营、设备商城运营及相关异常处理。",
		IsGroupManager:   true,
		DefaultScopeType: "group",
		Permissions: []string{
			"staff.group.view", "staff.role.view", "staff.employee.view", "staff.employee.create", "staff.employee.disable", "staff.employee.role_assign",
			"liveops.view_all", "liveops.configure", "liveops.ticket.manage", "liveops.room_quota.view", "liveops.room_quota.manage",
			"livepolicy.view", "livepolicy.manage_l1", "livepolicy.manage_l2", "livecoach.anchor_authorized", "livevoice.clone_authorized",
			"commercial.membership.view", "commercial.membership.manage", "commercial.time_card.view", "commercial.time_card.manage",
			"commercial.device.view", "commercial.device.listing.manage", "commercial.marketing.view", "commercial.marketing.manage", "commercial.referral.view", "commercial.referral.manage", "commercial.ai_time.view", "commercial.ai_time.request", "invitations.view_all", "audit.view",
		},
	},
	{
		GroupCode:        "live_operations",
		Code:             "live_operations_staff",
		Name:             "营销运维专员",
		Description:      "执行直播配置与联调、活动营销、时长卡与设备商城运营；可维护 L2，并在客户授权后代维护 L3、协助主播训练和声音复刻。",
		DefaultScopeType: "assigned",
		Permissions: []string{
			"liveops.configure", "liveops.room_quota.view", "livepolicy.view", "livepolicy.manage_l2", "livepolicy.manage_l3_authorized", "livecoach.anchor_authorized", "livevoice.clone_authorized",
			"commercial.membership.view", "commercial.membership.manage", "commercial.time_card.view", "commercial.time_card.manage",
			"commercial.device.view", "commercial.device.listing.manage", "commercial.marketing.view", "commercial.marketing.manage", "commercial.referral.view", "commercial.referral.manage", "commercial.ai_time.view", "commercial.ai_time.request", "invitations.view_all",
		},
	},
	{
		GroupCode:        "warehouse_after_sales",
		Code:             "warehouse_after_sales_manager",
		Name:             "仓储售后主管",
		Description:      "管理仓储售后部员工，并负责设备库存、物流、退换、维修与报废业务。",
		IsGroupManager:   true,
		DefaultScopeType: "group",
		Permissions: []string{
			"staff.group.view", "staff.role.view", "staff.employee.view", "staff.employee.create", "staff.employee.disable", "staff.employee.role_assign",
			"system.settings.view", "system.settings.inventory.manage", "commercial.device.view",
			"inventory.view", "inventory.manage", "inventory.after_sales.view", "inventory.after_sales.manage", "logistics.view", "logistics.manage", "audit.view",
		},
	},
	{
		GroupCode:        "warehouse_after_sales",
		Code:             "warehouse_after_sales_staff",
		Name:             "仓储售后员工",
		Description:      "执行设备入库、出库、调拨、物流与售后处理。",
		DefaultScopeType: "group",
		Permissions: []string{
			"system.settings.view", "commercial.device.view", "inventory.view", "inventory.manage",
			"inventory.after_sales.view", "inventory.after_sales.manage", "logistics.view", "logistics.manage",
		},
	},
}

func (s *Store) MigrateStaff(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS staff_meta (
			meta_key VARCHAR(64) NOT NULL,
			meta_value VARCHAR(255) NOT NULL,
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (meta_key)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_groups (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			code VARCHAR(64) NOT NULL,
			name VARCHAR(128) NOT NULL,
			description VARCHAR(512) NOT NULL DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			sort_order INT NOT NULL DEFAULT 0,
			system_managed TINYINT(1) NOT NULL DEFAULT 0,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_staff_groups_code (code),
			KEY idx_staff_groups_status_sort (status, sort_order)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_permissions (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			code VARCHAR(128) NOT NULL,
			module VARCHAR(64) NOT NULL,
			action VARCHAR(64) NOT NULL,
			description VARCHAR(512) NOT NULL DEFAULT '',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_staff_permissions_code (code),
			KEY idx_staff_permissions_module (module)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_roles (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			group_id BIGINT UNSIGNED NOT NULL,
			code VARCHAR(64) NOT NULL,
			name VARCHAR(128) NOT NULL,
			description VARCHAR(512) NOT NULL DEFAULT '',
			is_group_manager TINYINT(1) NOT NULL DEFAULT 0,
			default_scope_type VARCHAR(32) NOT NULL DEFAULT 'self',
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_staff_roles_code (code),
			KEY idx_staff_roles_group (group_id),
			CONSTRAINT fk_staff_roles_group FOREIGN KEY (group_id) REFERENCES staff_groups(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_role_permissions (
			role_id BIGINT UNSIGNED NOT NULL,
			permission_id BIGINT UNSIGNED NOT NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (role_id, permission_id),
			CONSTRAINT fk_staff_role_permissions_role FOREIGN KEY (role_id) REFERENCES staff_roles(id) ON DELETE CASCADE,
			CONSTRAINT fk_staff_role_permissions_permission FOREIGN KEY (permission_id) REFERENCES staff_permissions(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_employees (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			user_id BIGINT UNSIGNED NOT NULL,
			employee_no VARCHAR(64) NOT NULL,
			primary_group_id BIGINT UNSIGNED NOT NULL,
			employment_status VARCHAR(32) NOT NULL DEFAULT 'active',
			joined_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_staff_employees_user (user_id),
			UNIQUE KEY uk_staff_employees_no (employee_no),
			KEY idx_staff_employees_group_status (primary_group_id, employment_status),
			CONSTRAINT fk_staff_employees_user FOREIGN KEY (user_id) REFERENCES mgmt_users(id),
			CONSTRAINT fk_staff_employees_group FOREIGN KEY (primary_group_id) REFERENCES staff_groups(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_employee_roles (
			employee_id BIGINT UNSIGNED NOT NULL,
			role_id BIGINT UNSIGNED NOT NULL,
			scope_type VARCHAR(32) NOT NULL DEFAULT 'self',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (employee_id, role_id),
			CONSTRAINT fk_staff_employee_roles_employee FOREIGN KEY (employee_id) REFERENCES staff_employees(id) ON DELETE CASCADE,
			CONSTRAINT fk_staff_employee_roles_role FOREIGN KEY (role_id) REFERENCES staff_roles(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_group_managers (
			group_id BIGINT UNSIGNED NOT NULL,
			employee_id BIGINT UNSIGNED NOT NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (group_id, employee_id),
			CONSTRAINT fk_staff_group_managers_group FOREIGN KEY (group_id) REFERENCES staff_groups(id) ON DELETE CASCADE,
			CONSTRAINT fk_staff_group_managers_employee FOREIGN KEY (employee_id) REFERENCES staff_employees(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_approval_policies (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			code VARCHAR(64) NOT NULL,
			name VARCHAR(128) NOT NULL,
			operation_code VARCHAR(128) NOT NULL,
			mode VARCHAR(32) NOT NULL DEFAULT 'manual',
			threshold_amount DECIMAL(18,2) NOT NULL DEFAULT 0,
			approver_role_code VARCHAR(64) NOT NULL DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_staff_approval_policies_code (code),
			KEY idx_staff_approval_operation (operation_code, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS staff_approval_tasks (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			policy_id BIGINT UNSIGNED NULL,
			operation_code VARCHAR(128) NOT NULL,
			requester_user_id BIGINT UNSIGNED NOT NULL,
			approver_user_id BIGINT UNSIGNED NULL,
			target_type VARCHAR(64) NOT NULL DEFAULT '',
			target_id BIGINT UNSIGNED NULL,
			amount DECIMAL(18,2) NOT NULL DEFAULT 0,
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			payload_json JSON NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			decided_at DATETIME(3) NULL,
			PRIMARY KEY (id),
			KEY idx_staff_approval_tasks_status (status, created_at),
			CONSTRAINT fk_staff_approval_tasks_policy FOREIGN KEY (policy_id) REFERENCES staff_approval_policies(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply staff schema: %w", err)
		}
	}

	hasSystemManaged, err := s.columnExists(ctx, "staff_groups", "system_managed")
	if err != nil {
		return err
	}
	if !hasSystemManaged {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE staff_groups
			ADD COLUMN system_managed TINYINT(1) NOT NULL DEFAULT 0 AFTER sort_order
		`); err != nil {
			return fmt.Errorf("add staff_groups.system_managed: %w", err)
		}
	}

	var seeded string
	err = s.db.QueryRowContext(
		ctx,
		"SELECT meta_value FROM staff_meta WHERE meta_key=? LIMIT 1",
		staffSeedVersion,
	).Scan(&seeded)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows {
		if err := s.seedInitialStaffModel(ctx); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(
			ctx,
			"INSERT INTO staff_meta (meta_key, meta_value) VALUES (?, 'done')",
			staffSeedVersion,
		); err != nil {
			return err
		}
	}

	if err := s.migrateLegacySalesStaff(ctx); err != nil {
		return err
	}
	return s.migrateLivePolicyAccessSeparation(ctx)
}

func (s *Store) seedInitialStaffModel(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	groupSeeds := []struct {
		Code        string
		Name        string
		Description string
		SortOrder   int
	}{
		{"management", "管理部", "查看系统架构、维护已有部门内员工，但不能修改部门和权限定义。", 10},
		{"finance", "财务部", "负责充值、退款、奖励、会员、AI 时长调整及审核。", 20},
		{"sales", "销售部", "负责直营终端销售关系、终端分配与销售人员管理。", 30},
		{"live_operations", "营销运维部", "负责直播运维、营销活动、时长卡运营、设备商城运营、联调监控、异常处理和远程协助。", 40},
		{"warehouse_after_sales", "仓储售后部", "负责设备入库、出库、调拨、物流、退换货、维修、翻新和报废。", 50},
	}
	for _, item := range groupSeeds {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_groups (
				code, name, description, status, sort_order, system_managed
			)
			VALUES (?, ?, ?, 'active', ?, 1)
			ON DUPLICATE KEY UPDATE
				name=VALUES(name),
				description=VALUES(description),
				status='active',
				sort_order=VALUES(sort_order),
				system_managed=1
		`, item.Code, item.Name, item.Description, item.SortOrder); err != nil {
			return err
		}
	}

	for _, item := range staffPermissionSeeds {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_permissions (code, module, action, description)
			VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				module=VALUES(module),
				action=VALUES(action),
				description=VALUES(description)
		`, item.Code, item.Module, item.Action, item.Description); err != nil {
			return err
		}
	}

	for _, role := range staffRoleSeeds {
		var groupID int64
		if err := tx.QueryRowContext(
			ctx,
			"SELECT id FROM staff_groups WHERE code=? LIMIT 1",
			role.GroupCode,
		).Scan(&groupID); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_roles (
				group_id, code, name, description,
				is_group_manager, default_scope_type, status
			)
			VALUES (?, ?, ?, ?, ?, ?, 'active')
			ON DUPLICATE KEY UPDATE
				group_id=VALUES(group_id),
				name=VALUES(name),
				description=VALUES(description),
				is_group_manager=VALUES(is_group_manager),
				default_scope_type=VALUES(default_scope_type),
				status='active'
		`,
			groupID,
			role.Code,
			role.Name,
			role.Description,
			role.IsGroupManager,
			role.DefaultScopeType,
		); err != nil {
			return err
		}

		var roleID int64
		if err := tx.QueryRowContext(
			ctx,
			"SELECT id FROM staff_roles WHERE code=? LIMIT 1",
			role.Code,
		).Scan(&roleID); err != nil {
			return err
		}

		// Seeded roles are system defaults. Synchronize their permission set exactly
		// on seed-version upgrades so permissions removed from a role do not linger.
		if _, err := tx.ExecContext(ctx, "DELETE FROM staff_role_permissions WHERE role_id=?", roleID); err != nil {
			return err
		}

		for _, permissionCode := range role.Permissions {
			if _, err := tx.ExecContext(ctx, `
				INSERT IGNORE INTO staff_role_permissions (role_id, permission_id)
				SELECT ?, id
				FROM staff_permissions
				WHERE code=?
			`, roleID, permissionCode); err != nil {
				return err
			}
		}
	}

	policies := []struct {
		Code         string
		Name         string
		Operation    string
		ApproverRole string
	}{
		{"finance_recharge", "充值审核", "finance.recharge", "finance_reviewer"},
		{"finance_refund", "退款审核", "finance.refund", "finance_reviewer"},
		{"finance_reward", "奖励发放审核", "finance.reward", "finance_reviewer"},
		{"finance_ai_time_grant", "AI 时长增加审核", "finance.ai_time_grant", "finance_reviewer"},
	}
	for _, policy := range policies {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_approval_policies (
				code, name, operation_code, mode,
				threshold_amount, approver_role_code, status
			)
			VALUES (?, ?, ?, 'manual', 0, ?, 'active')
			ON DUPLICATE KEY UPDATE code=VALUES(code)
		`, policy.Code, policy.Name, policy.Operation, policy.ApproverRole); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) migrateLegacySalesStaff(ctx context.Context) error {
	var groupID int64
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT id FROM staff_groups WHERE code='sales' LIMIT 1",
	).Scan(&groupID); err != nil {
		return err
	}
	var roleID int64
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT id FROM staff_roles WHERE code='sales_staff' LIMIT 1",
	).Scan(&roleID); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO staff_employees (
			user_id, employee_no, primary_group_id, employment_status, joined_at
		)
		SELECT
			s.user_id,
			s.employee_code,
			?,
			CASE WHEN s.status='active' THEN 'active' ELSE 'disabled' END,
			u.created_at
		FROM crm_sales_staff s
		INNER JOIN mgmt_users u ON u.id=s.user_id
	`, groupID); err != nil {
		return fmt.Errorf("migrate sales employees: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO staff_employee_roles (employee_id, role_id, scope_type)
		SELECT e.id, ?, 'assigned'
		FROM staff_employees e
		INNER JOIN mgmt_users u ON u.id=e.user_id
		WHERE e.primary_group_id=? AND u.role='sales_staff'
	`, roleID, groupID); err != nil {
		return fmt.Errorf("migrate sales roles: %w", err)
	}
	return nil
}

func strongerStaffScope(left string, right string) string {
	rank := map[string]int{
		"self":           1,
		"assigned":       2,
		"group":          3,
		"managed_groups": 4,
		"all_internal":   5,
		"all":            6,
	}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func sortedUniqueInt64(values []int64) []int64 {
	set := map[int64]struct{}{}
	for _, value := range values {
		if value > 0 {
			set[value] = struct{}{}
		}
	}
	result := make([]int64, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func containsInt64(values []int64, value int64) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func normalizeStaffCode(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (s *Store) GetStaffAccess(
	ctx context.Context,
	userID int64,
) (model.StaffAccessContext, error) {
	var userRole string
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT role FROM mgmt_users WHERE id=? AND status='active' LIMIT 1",
		userID,
	).Scan(&userRole); err != nil {
		return model.StaffAccessContext{}, err
	}

	if userRole == "platform_admin" {
		return model.StaffAccessContext{
			IsSuperAdmin:       true,
			RoleCodes:          []string{"super_system_admin"},
			Permissions:        []string{"*"},
			PermissionScopes:   map[string]string{"*": "all"},
			PermissionGroupIDs: map[string][]int64{},
			GroupIDs:           []int64{},
			ManagedGroupIDs:    []int64{},
		}, nil
	}

	var access model.StaffAccessContext
	err := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.primary_group_id, g.code, g.name
		FROM staff_employees e
		INNER JOIN staff_groups g ON g.id=e.primary_group_id
		WHERE e.user_id=?
		  AND e.employment_status='active'
		  AND g.status='active'
		LIMIT 1
	`, userID).Scan(
		&access.EmployeeID,
		&access.PrimaryGroupID,
		&access.PrimaryGroupCode,
		&access.PrimaryGroupName,
	)
	if err != nil {
		return model.StaffAccessContext{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.code,
			r.group_id,
			er.scope_type,
			r.is_group_manager,
			COALESCE(p.code, '')
		FROM staff_employee_roles er
		INNER JOIN staff_roles r ON r.id=er.role_id
		LEFT JOIN staff_role_permissions rp ON rp.role_id=r.id
		LEFT JOIN staff_permissions p ON p.id=rp.permission_id
		WHERE er.employee_id=?
		  AND r.status='active'
		ORDER BY r.id, p.id
	`, access.EmployeeID)
	if err != nil {
		return model.StaffAccessContext{}, err
	}
	defer rows.Close()

	roleSet := map[string]struct{}{}
	permissionSet := map[string]struct{}{}
	access.PermissionScopes = map[string]string{}
	access.PermissionGroupIDs = map[string][]int64{}
	access.GroupIDs = []int64{access.PrimaryGroupID}
	for rows.Next() {
		var roleCode string
		var roleGroupID int64
		var scopeType string
		var isManager bool
		var permissionCode string
		if err := rows.Scan(
			&roleCode,
			&roleGroupID,
			&scopeType,
			&isManager,
			&permissionCode,
		); err != nil {
			return model.StaffAccessContext{}, err
		}
		roleSet[roleCode] = struct{}{}
		access.GroupIDs = append(access.GroupIDs, roleGroupID)
		if permissionCode != "" {
			permissionSet[permissionCode] = struct{}{}
			current := access.PermissionScopes[permissionCode]
			access.PermissionScopes[permissionCode] = strongerStaffScope(
				current,
				scopeType,
			)
			if scopeType == "group" {
				access.PermissionGroupIDs[permissionCode] = append(
					access.PermissionGroupIDs[permissionCode],
					roleGroupID,
				)
			}
		}
		if isManager {
			access.ManagedGroupIDs = append(
				access.ManagedGroupIDs,
				roleGroupID,
			)
		}
	}
	if err := rows.Err(); err != nil {
		return model.StaffAccessContext{}, err
	}

	managerRows, err := s.db.QueryContext(ctx, `
		SELECT group_id
		FROM staff_group_managers
		WHERE employee_id=?
	`, access.EmployeeID)
	if err != nil {
		return model.StaffAccessContext{}, err
	}
	defer managerRows.Close()
	for managerRows.Next() {
		var groupID int64
		if err := managerRows.Scan(&groupID); err != nil {
			return model.StaffAccessContext{}, err
		}
		access.ManagedGroupIDs = append(access.ManagedGroupIDs, groupID)
	}
	if err := managerRows.Err(); err != nil {
		return model.StaffAccessContext{}, err
	}

	for code := range roleSet {
		access.RoleCodes = append(access.RoleCodes, code)
	}
	for code := range permissionSet {
		access.Permissions = append(access.Permissions, code)
	}
	sort.Strings(access.RoleCodes)
	sort.Strings(access.Permissions)
	access.GroupIDs = sortedUniqueInt64(access.GroupIDs)
	for permissionCode, groupIDs := range access.PermissionGroupIDs {
		access.PermissionGroupIDs[permissionCode] = sortedUniqueInt64(groupIDs)
	}
	access.ManagedGroupIDs = sortedUniqueInt64(access.ManagedGroupIDs)

	return access, nil
}

func (s *Store) ListStaffGroups(
	ctx context.Context,
) ([]model.StaffGroupSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			g.id,
			g.code,
			g.name,
			g.description,
			g.status,
			g.sort_order,
			g.system_managed,
			(
				SELECT COUNT(DISTINCT e.id)
				FROM staff_employees e
				WHERE e.employment_status='active'
				  AND (
					e.primary_group_id=g.id OR EXISTS (
						SELECT 1
						FROM staff_employee_roles er
						INNER JOIN staff_roles r ON r.id=er.role_id
						WHERE er.employee_id=e.id AND r.group_id=g.id
					)
				  )
			),
			(
				SELECT COUNT(DISTINCT gm.employee_id)
				FROM staff_group_managers gm
				WHERE gm.group_id=g.id
			),
			g.created_at
		FROM staff_groups g
		ORDER BY g.sort_order ASC, g.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffGroupSummary, 0)
	for rows.Next() {
		var item model.StaffGroupSummary
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.Description,
			&item.Status,
			&item.SortOrder,
			&item.SystemManaged,
			&item.MemberCount,
			&item.ManagerCount,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateStaffGroup(
	ctx context.Context,
	groupID int64,
	name string,
	description string,
	status string,
) error {
	var systemManaged bool
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT system_managed FROM staff_groups WHERE id=? LIMIT 1",
		groupID,
	).Scan(&systemManaged); err != nil {
		return err
	}
	if systemManaged {
		return fmt.Errorf("system managed staff group is locked")
	}

	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	status = strings.TrimSpace(status)
	if name == "" {
		return fmt.Errorf("group name is required")
	}
	if status != "active" && status != "disabled" {
		return fmt.Errorf("invalid group status")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE staff_groups
		SET name=?, description=?, status=?
		WHERE id=?
	`, name, description, status, groupID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListStaffPermissions(
	ctx context.Context,
) ([]model.StaffPermissionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, code, module, action, description
		FROM staff_permissions
		ORDER BY module ASC, code ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffPermissionSummary, 0)
	for rows.Next() {
		var item model.StaffPermissionSummary
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Module,
			&item.Action,
			&item.Description,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListStaffRoles(
	ctx context.Context,
	groupID int64,
) ([]model.StaffRoleSummary, error) {
	query := `
		SELECT
			r.id,
			r.group_id,
			g.code,
			g.name,
			r.code,
			r.name,
			r.description,
			r.is_group_manager,
			r.default_scope_type,
			r.status,
			r.created_at
		FROM staff_roles r
		INNER JOIN staff_groups g ON g.id=r.group_id
	`
	args := make([]any, 0, 1)
	if groupID > 0 {
		query += " WHERE r.group_id=?"
		args = append(args, groupID)
	}
	query += " ORDER BY g.sort_order ASC, r.id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffRoleSummary, 0)
	for rows.Next() {
		var item model.StaffRoleSummary
		if err := rows.Scan(
			&item.ID,
			&item.GroupID,
			&item.GroupCode,
			&item.GroupName,
			&item.Code,
			&item.Name,
			&item.Description,
			&item.IsGroupManager,
			&item.DefaultScopeType,
			&item.Status,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Permissions = []model.StaffPermissionSummary{}
		item.PermissionCodes = []string{}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for index := range items {
		permissions, err := s.listStaffRolePermissions(ctx, items[index].ID)
		if err != nil {
			return nil, err
		}
		items[index].Permissions = permissions
		for _, permission := range permissions {
			items[index].PermissionCodes = append(
				items[index].PermissionCodes,
				permission.Code,
			)
		}
	}
	return items, nil
}

func (s *Store) listStaffRolePermissions(
	ctx context.Context,
	roleID int64,
) ([]model.StaffPermissionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.code, p.module, p.action, p.description
		FROM staff_role_permissions rp
		INNER JOIN staff_permissions p ON p.id=rp.permission_id
		WHERE rp.role_id=?
		ORDER BY p.module ASC, p.code ASC
	`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffPermissionSummary, 0)
	for rows.Next() {
		var item model.StaffPermissionSummary
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Module,
			&item.Action,
			&item.Description,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) ListStaffEmployees(
	ctx context.Context,
	groupID int64,
) ([]model.StaffEmployeeSummary, error) {
	query := `
		SELECT
			e.id,
			e.user_id,
			e.employee_no,
			u.username,
			u.display_name,
			u.phone,
			u.email,
			u.province,
			u.city,
			u.district,
			e.primary_group_id,
			g.code,
			g.name,
			e.employment_status,
			u.status,
			e.created_at
		FROM staff_employees e
		INNER JOIN mgmt_users u ON u.id=e.user_id
		INNER JOIN staff_groups g ON g.id=e.primary_group_id
	`
	args := make([]any, 0, 1)
	if groupID > 0 {
		query += `
			WHERE e.primary_group_id=? OR EXISTS (
				SELECT 1
				FROM staff_employee_roles er
				INNER JOIN staff_roles r ON r.id=er.role_id
				WHERE er.employee_id=e.id AND r.group_id=?
			)
		`
		args = append(args, groupID, groupID)
	}
	query += " ORDER BY g.sort_order ASC, e.id DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffEmployeeSummary, 0)
	for rows.Next() {
		var item model.StaffEmployeeSummary
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.EmployeeNo,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.Email,
			&item.Province,
			&item.City,
			&item.District,
			&item.PrimaryGroupID,
			&item.PrimaryGroupCode,
			&item.PrimaryGroupName,
			&item.EmploymentStatus,
			&item.UserStatus,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Roles = []model.StaffEmployeeRoleSummary{}
		item.Groups = []model.StaffEmployeeGroupSummary{}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for index := range items {
		roles, err := s.listStaffEmployeeRoles(ctx, items[index].ID)
		if err != nil {
			return nil, err
		}
		items[index].Roles = roles
		groupMap := map[int64]model.StaffEmployeeGroupSummary{
			items[index].PrimaryGroupID: {
				GroupID:   items[index].PrimaryGroupID,
				GroupCode: items[index].PrimaryGroupCode,
				GroupName: items[index].PrimaryGroupName,
				IsPrimary: true,
			},
		}
		for _, role := range roles {
			entry := groupMap[role.GroupID]
			entry.GroupID = role.GroupID
			entry.GroupCode = role.GroupCode
			entry.GroupName = role.GroupName
			entry.IsPrimary = role.GroupID == items[index].PrimaryGroupID
			groupMap[role.GroupID] = entry
		}
		groupIDs := make([]int64, 0, len(groupMap))
		for groupID := range groupMap {
			groupIDs = append(groupIDs, groupID)
		}
		sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
		for _, groupID := range groupIDs {
			items[index].Groups = append(items[index].Groups, groupMap[groupID])
		}
	}
	return items, nil
}

func (s *Store) GetStaffEmployee(
	ctx context.Context,
	employeeID int64,
) (model.StaffEmployeeSummary, error) {
	items, err := s.ListStaffEmployees(ctx, 0)
	if err != nil {
		return model.StaffEmployeeSummary{}, err
	}
	for _, item := range items {
		if item.ID == employeeID {
			return item, nil
		}
	}
	return model.StaffEmployeeSummary{}, sql.ErrNoRows
}

func (s *Store) listStaffEmployeeRoles(
	ctx context.Context,
	employeeID int64,
) ([]model.StaffEmployeeRoleSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.id,
			r.group_id,
			g.code,
			g.name,
			r.code,
			r.name,
			er.scope_type,
			r.is_group_manager
		FROM staff_employee_roles er
		INNER JOIN staff_roles r ON r.id=er.role_id
		INNER JOIN staff_groups g ON g.id=r.group_id
		WHERE er.employee_id=?
		ORDER BY g.sort_order ASC, r.id ASC
	`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffEmployeeRoleSummary, 0)
	for rows.Next() {
		var item model.StaffEmployeeRoleSummary
		if err := rows.Scan(
			&item.RoleID,
			&item.GroupID,
			&item.GroupCode,
			&item.GroupName,
			&item.Code,
			&item.Name,
			&item.ScopeType,
			&item.IsGroupManager,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateStaffGroup(
	ctx context.Context,
	code string,
	name string,
	description string,
) (model.StaffGroupSummary, error) {
	code = normalizeStaffCode(code)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if code == "" || name == "" {
		return model.StaffGroupSummary{}, fmt.Errorf("group code and name are required")
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO staff_groups (
			code, name, description, status, sort_order
		)
		VALUES (
			?, ?, ?, 'active',
			COALESCE((SELECT MAX(g.sort_order)+10 FROM staff_groups g), 10)
		)
	`, code, name, description)
	if err != nil {
		return model.StaffGroupSummary{}, normalizeDuplicate(err)
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		return model.StaffGroupSummary{}, err
	}

	groups, err := s.ListStaffGroups(ctx)
	if err != nil {
		return model.StaffGroupSummary{}, err
	}
	for _, group := range groups {
		if group.ID == groupID {
			return group, nil
		}
	}
	return model.StaffGroupSummary{}, sql.ErrNoRows
}

func (s *Store) CreateStaffRole(
	ctx context.Context,
	groupID int64,
	code string,
	name string,
	description string,
	isGroupManager bool,
	defaultScope string,
	permissionIDs []int64,
) (model.StaffRoleSummary, error) {
	code = normalizeStaffCode(code)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	defaultScope = strings.TrimSpace(defaultScope)
	if code == "" || name == "" {
		return model.StaffRoleSummary{}, fmt.Errorf("role code and name are required")
	}
	if !validStaffScope(defaultScope) {
		return model.StaffRoleSummary{}, fmt.Errorf("invalid staff scope")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.StaffRoleSummary{}, err
	}
	defer tx.Rollback()

	var groupStatus string
	if err := tx.QueryRowContext(
		ctx,
		"SELECT status FROM staff_groups WHERE id=? LIMIT 1",
		groupID,
	).Scan(&groupStatus); err != nil {
		return model.StaffRoleSummary{}, err
	}
	if groupStatus != "active" {
		return model.StaffRoleSummary{}, fmt.Errorf("group is disabled")
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO staff_roles (
			group_id, code, name, description,
			is_group_manager, default_scope_type, status
		)
		VALUES (?, ?, ?, ?, ?, ?, 'active')
	`,
		groupID,
		code,
		name,
		description,
		isGroupManager,
		defaultScope,
	)
	if err != nil {
		return model.StaffRoleSummary{}, normalizeDuplicate(err)
	}
	roleID, err := result.LastInsertId()
	if err != nil {
		return model.StaffRoleSummary{}, err
	}

	if err := replaceRolePermissionsTx(
		ctx,
		tx,
		roleID,
		permissionIDs,
	); err != nil {
		return model.StaffRoleSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.StaffRoleSummary{}, err
	}

	roles, err := s.ListStaffRoles(ctx, groupID)
	if err != nil {
		return model.StaffRoleSummary{}, err
	}
	for _, role := range roles {
		if role.ID == roleID {
			return role, nil
		}
	}
	return model.StaffRoleSummary{}, sql.ErrNoRows
}

func (s *Store) UpdateStaffRole(
	ctx context.Context,
	roleID int64,
	name string,
	description string,
	isGroupManager bool,
	defaultScope string,
	status string,
	permissionIDs []int64,
) error {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	defaultScope = strings.TrimSpace(defaultScope)
	status = strings.TrimSpace(status)
	if name == "" || !validStaffScope(defaultScope) {
		return fmt.Errorf("invalid role input")
	}
	if status != "active" && status != "disabled" {
		return fmt.Errorf("invalid role status")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE staff_roles
		SET
			name=?,
			description=?,
			is_group_manager=?,
			default_scope_type=?,
			status=?
		WHERE id=?
	`,
		name,
		description,
		isGroupManager,
		defaultScope,
		status,
		roleID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	if err := replaceRolePermissionsTx(
		ctx,
		tx,
		roleID,
		permissionIDs,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceRolePermissionsTx(
	ctx context.Context,
	tx *sql.Tx,
	roleID int64,
	permissionIDs []int64,
) error {
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM staff_role_permissions WHERE role_id=?",
		roleID,
	); err != nil {
		return err
	}
	for _, permissionID := range sortedUniqueInt64(permissionIDs) {
		var count int
		if err := tx.QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM staff_permissions WHERE id=?",
			permissionID,
		).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return sql.ErrNoRows
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_role_permissions (role_id, permission_id)
			VALUES (?, ?)
		`, roleID, permissionID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateStaffEmployee(
	ctx context.Context,
	employeeNo string,
	groupID int64,
	roleIDs []int64,
	username string,
	displayName string,
	phone string,
	email string,
	province string,
	city string,
	district string,
	passwordHash string,
) (model.StaffEmployeeSummary, error) {
	employeeNo = strings.TrimSpace(employeeNo)
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	phone = strings.TrimSpace(phone)
	email = strings.TrimSpace(email)
	province = strings.TrimSpace(province)
	city = strings.TrimSpace(city)
	district = strings.TrimSpace(district)
	if employeeNo == "" ||
		username == "" ||
		displayName == "" ||
		phone == "" ||
		province == "" ||
		city == "" ||
		district == "" {
		return model.StaffEmployeeSummary{}, fmt.Errorf("staff employee input is incomplete")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.StaffEmployeeSummary{}, err
	}
	defer tx.Rollback()

	var groupStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM staff_groups
		WHERE id=?
		LIMIT 1
	`, groupID).Scan(&groupStatus); err != nil {
		return model.StaffEmployeeSummary{}, err
	}
	if groupStatus != "active" {
		return model.StaffEmployeeSummary{}, fmt.Errorf("group is disabled")
	}

	validatedRoles, err := loadStaffRolesForAssignmentTx(
		ctx,
		tx,
		groupID,
		roleIDs,
	)
	if err != nil {
		return model.StaffEmployeeSummary{}, err
	}
	if len(validatedRoles) == 0 {
		return model.StaffEmployeeSummary{}, fmt.Errorf("at least one role is required")
	}
	if err := validateStaffRoleCombination(validatedRoles); err != nil {
		return model.StaffEmployeeSummary{}, err
	}

	userRole := "staff"
	if staffRolesContainGroupCode(validatedRoles, "sales") {
		userRole = "sales_staff"
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO mgmt_users (
			tenant_id,
			username,
			password_hash,
			must_change_password,
			display_name,
			phone,
			email,
			province,
			city,
			district,
			role,
			status
		)
		VALUES (
			NULL, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, 'active'
		)
	`,
		username,
		passwordHash,
		displayName,
		phone,
		email,
		province,
		city,
		district,
		userRole,
	)
	if err != nil {
		return model.StaffEmployeeSummary{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return model.StaffEmployeeSummary{}, err
	}

	result, err = tx.ExecContext(ctx, `
		INSERT INTO staff_employees (
			user_id,
			employee_no,
			primary_group_id,
			employment_status
		)
		VALUES (?, ?, ?, 'active')
	`, userID, employeeNo, groupID)
	if err != nil {
		return model.StaffEmployeeSummary{}, normalizeDuplicate(err)
	}
	employeeID, err := result.LastInsertId()
	if err != nil {
		return model.StaffEmployeeSummary{}, err
	}

	for _, role := range validatedRoles {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_employee_roles (
				employee_id, role_id, scope_type
			)
			VALUES (?, ?, ?)
		`, employeeID, role.ID, role.DefaultScopeType); err != nil {
			return model.StaffEmployeeSummary{}, err
		}
		if role.IsGroupManager {
			if _, err := tx.ExecContext(ctx, `
				INSERT IGNORE INTO staff_group_managers (
					group_id, employee_id
				)
				VALUES (?, ?)
			`, role.GroupID, employeeID); err != nil {
				return model.StaffEmployeeSummary{}, err
			}
		}
	}

	if err := syncSalesStaffRoleTx(
		ctx,
		tx,
		userID,
		employeeNo,
		validatedRoles,
	); err != nil {
		return model.StaffEmployeeSummary{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.StaffEmployeeSummary{}, err
	}
	return s.GetStaffEmployee(ctx, employeeID)
}

func (s *Store) DisableStaffEmployee(
	ctx context.Context,
	employeeID int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID int64
	if err := tx.QueryRowContext(
		ctx,
		"SELECT user_id FROM staff_employees WHERE id=? LIMIT 1",
		employeeID,
	).Scan(&userID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE staff_employees
		SET employment_status='disabled'
		WHERE id=?
	`, employeeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mgmt_users
		SET status='disabled'
		WHERE id=?
	`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE crm_sales_staff
		SET status='disabled'
		WHERE user_id=?
	`, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM mgmt_sessions WHERE user_id=?",
		userID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReplaceStaffEmployeeRoles(
	ctx context.Context,
	employeeID int64,
	roleIDs []int64,
	allowManagerRoles bool,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var groupID int64
	var userID int64
	var employeeNo string
	if err := tx.QueryRowContext(ctx, `
		SELECT primary_group_id, user_id, employee_no
		FROM staff_employees
		WHERE id=? AND employment_status='active'
		LIMIT 1
	`, employeeID).Scan(&groupID, &userID, &employeeNo); err != nil {
		return err
	}

	roles, err := loadStaffRolesForAssignmentTx(
		ctx,
		tx,
		groupID,
		roleIDs,
	)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		return fmt.Errorf("at least one role is required")
	}
	if err := validateStaffRoleCombination(roles); err != nil {
		return err
	}
	if !allowManagerRoles {
		for _, role := range roles {
			if role.IsGroupManager {
				return fmt.Errorf("group manager role cannot be assigned")
			}
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM staff_employee_roles WHERE employee_id=?",
		employeeID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM staff_group_managers WHERE employee_id=?",
		employeeID,
	); err != nil {
		return err
	}

	for _, role := range roles {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO staff_employee_roles (
				employee_id, role_id, scope_type
			)
			VALUES (?, ?, ?)
		`, employeeID, role.ID, role.DefaultScopeType); err != nil {
			return err
		}
		if role.IsGroupManager {
			if _, err := tx.ExecContext(ctx, `
				INSERT IGNORE INTO staff_group_managers (
					group_id, employee_id
				)
				VALUES (?, ?)
			`, role.GroupID, employeeID); err != nil {
				return err
			}
		}
	}
	if err := syncSalesStaffRoleTx(ctx, tx, userID, employeeNo, roles); err != nil {
		return err
	}

	return tx.Commit()
}

func loadStaffRolesForAssignmentTx(
	ctx context.Context,
	tx *sql.Tx,
	primaryGroupID int64,
	roleIDs []int64,
) ([]model.StaffRoleSummary, error) {
	roleIDs = sortedUniqueInt64(roleIDs)
	items := make([]model.StaffRoleSummary, 0, len(roleIDs))
	hasPrimaryRole := false
	for _, roleID := range roleIDs {
		var item model.StaffRoleSummary
		var groupStatus string
		if err := tx.QueryRowContext(ctx, `
			SELECT
				r.id,
				r.group_id,
				g.code,
				g.name,
				r.code,
				r.name,
				r.is_group_manager,
				r.default_scope_type,
				r.status,
				g.status
			FROM staff_roles r
			INNER JOIN staff_groups g ON g.id=r.group_id
			WHERE r.id=?
			LIMIT 1
		`, roleID).Scan(
			&item.ID,
			&item.GroupID,
			&item.GroupCode,
			&item.GroupName,
			&item.Code,
			&item.Name,
			&item.IsGroupManager,
			&item.DefaultScopeType,
			&item.Status,
			&groupStatus,
		); err != nil {
			return nil, err
		}
		if item.Status != "active" || groupStatus != "active" {
			return nil, fmt.Errorf("role or department is disabled")
		}
		if item.GroupID == primaryGroupID {
			hasPrimaryRole = true
		}
		items = append(items, item)
	}
	if len(items) > 0 && !hasPrimaryRole {
		return nil, fmt.Errorf("at least one role must belong to the primary department")
	}
	return items, nil
}

func staffRolesContainGroupCode(roles []model.StaffRoleSummary, groupCode string) bool {
	groupCode = normalizeStaffCode(groupCode)
	for _, role := range roles {
		if normalizeStaffCode(role.GroupCode) == groupCode {
			return true
		}
	}
	return false
}

func syncSalesStaffRoleTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	employeeNo string,
	roles []model.StaffRoleSummary,
) error {
	if !staffRolesContainGroupCode(roles, "sales") {
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_sales_staff
			SET status='disabled'
			WHERE user_id=?
		`, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE mgmt_users
			SET role='staff'
			WHERE id=? AND role='sales_staff'
		`, userID)
		return err
	}

	var teamID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM crm_sales_teams
		WHERE code=? AND status='active'
		LIMIT 1
	`, defaultSalesTeamCode).Scan(&teamID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_sales_staff (
			user_id, employee_code, team_id, status
		)
		VALUES (?, ?, ?, 'active')
		ON DUPLICATE KEY UPDATE
			employee_code=VALUES(employee_code),
			team_id=COALESCE(crm_sales_staff.team_id, VALUES(team_id)),
			status='active'
	`, userID, employeeNo, teamID); err != nil {
		return normalizeDuplicate(err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mgmt_users
		SET role='sales_staff'
		WHERE id=?
	`, userID); err != nil {
		return err
	}
	return ensureUserInviteCodeTx(ctx, tx, userID, nil, "sales_staff")
}

func validateStaffRoleCombination(roles []model.StaffRoleSummary) error {
	hasLiveOperationsManager := false
	hasLiveOperationsStaff := false
	for _, role := range roles {
		switch role.Code {
		case "live_operations_manager":
			hasLiveOperationsManager = true
		case "live_operations_staff":
			hasLiveOperationsStaff = true
		}
	}
	if hasLiveOperationsManager && hasLiveOperationsStaff {
		return ErrMutuallyExclusiveStaffRoles
	}
	return nil
}

func validStaffScope(value string) bool {
	switch value {
	case "self", "assigned", "group", "managed_groups", "all_internal", "all":
		return true
	default:
		return false
	}
}

func (s *Store) ListStaffApprovalPolicies(
	ctx context.Context,
) ([]model.StaffApprovalPolicySummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			code,
			name,
			operation_code,
			mode,
			threshold_amount,
			approver_role_code,
			status
		FROM staff_approval_policies
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffApprovalPolicySummary, 0)
	for rows.Next() {
		var item model.StaffApprovalPolicySummary
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.OperationCode,
			&item.Mode,
			&item.ThresholdAmount,
			&item.ApproverRoleCode,
			&item.Status,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateStaffApprovalPolicy(
	ctx context.Context,
	policyID int64,
	mode string,
	threshold float64,
	approverRoleCode string,
	status string,
) error {
	mode = strings.TrimSpace(mode)
	approverRoleCode = normalizeStaffCode(approverRoleCode)
	status = strings.TrimSpace(status)
	if mode != "manual" && mode != "threshold" && mode != "direct" {
		return fmt.Errorf("invalid approval mode")
	}
	if threshold < 0 {
		return fmt.Errorf("invalid threshold")
	}
	if status != "active" && status != "disabled" {
		return fmt.Errorf("invalid approval status")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE staff_approval_policies
		SET
			mode=?,
			threshold_amount=?,
			approver_role_code=?,
			status=?
		WHERE id=?
	`, mode, threshold, approverRoleCode, status, policyID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
