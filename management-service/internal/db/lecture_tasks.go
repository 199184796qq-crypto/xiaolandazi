package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MigrateLectureTasks 保留历史部署的表结构与事实修订触发器迁移。
// 任务讲解业务已下线（生成、调度、题库读取全部删除），这里只做 schema 兜底：
// 老库里可能仍有 live_plan_lecture_* 表与 story_lecture 触发条件，迁移保持幂等，
// 不再写入或读取任何讲解数据。
func (s *Store) MigrateLectureTasks(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS live_plan_lecture_tasks (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, tenant_id BIGINT UNSIGNED NOT NULL, plan_id BIGINT UNSIGNED NOT NULL, source_sha CHAR(64) NOT NULL, source_text MEDIUMTEXT NOT NULL, title VARCHAR(160) NOT NULL, purpose TEXT NOT NULL, angles_json MEDIUMTEXT NOT NULL, facts_json MEDIUMTEXT NOT NULL, links_json TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE, status VARCHAR(20) NOT NULL DEFAULT 'active', revision BIGINT NOT NULL DEFAULT 1, review_json MEDIUMTEXT NOT NULL, created_by BIGINT UNSIGNED NOT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3), PRIMARY KEY(id), UNIQUE KEY uq_lecture_source(tenant_id,plan_id,source_sha), KEY idx_lecture_plan(tenant_id,plan_id,status)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS live_plan_lecture_policies (tenant_id BIGINT UNSIGNED NOT NULL, plan_id BIGINT UNSIGNED NOT NULL, policy_json TEXT NOT NULL, PRIMARY KEY(tenant_id,plan_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	// Story facts belong to the independent library. Ordinary fact mutations
	// still invalidate content pools, including transitions into/out of stories.
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		name := "xl_pool_source_0_" + op
		var body string
		err := s.db.QueryRowContext(ctx, `SELECT ACTION_STATEMENT FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?`, name).Scan(&body)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if strings.Contains(body, "story_lecture") {
			continue
		}
		row, condition := "NEW", "NEW.source_type <> 'story_lecture'"
		if op == "DELETE" {
			row, condition = "OLD", "OLD.source_type <> 'story_lecture'"
		}
		if op == "UPDATE" {
			condition = "NEW.source_type <> 'story_lecture' OR OLD.source_type <> 'story_lecture'"
		}
		if _, err = s.db.ExecContext(ctx, "DROP TRIGGER "+name); err != nil {
			return err
		}
		q := fmt.Sprintf("CREATE TRIGGER %s AFTER %s ON live_agent_plan_facts FOR EACH ROW BEGIN IF %s THEN INSERT INTO live_plan_content_revisions(tenant_id,plan_id,revision) VALUES(%s.tenant_id,%s.plan_id,1) ON DUPLICATE KEY UPDATE revision=revision+1; END IF; END", name, op, condition, row, row)
		if _, err = s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("story fact revision trigger %s: %w", name, err)
		}
	}
	return nil
}
