package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

var ErrLiveCardCopyInvalid = errors.New("cannot copy live card")
var cardLinkNumberPattern = regexp.MustCompile(`^(链接\s*)?([0-9]+)(\s*(?:号(?:链接)?|链接)?)$`)
var copiedCardParentPattern = regexp.MustCompile(`^copy:(product|benefit):([0-9]+):v[0-9]+$`)

// Restore copy placement on reload. Missing/deleted parents remain ordinary
// cards; invalid/cyclic lineage never hides a card. Inputs are tenant/plan scoped.
func orderCopiedCards[T any](items []T, kind string, id func(T) int64, ref func(T) string) []T {
	positions := map[int64]int{}
	for i, item := range items {
		positions[id(item)] = i
	}
	children := map[int64][]int{}
	parents := map[int64]int64{}
	for i, item := range items {
		match := copiedCardParentPattern.FindStringSubmatch(ref(item))
		if len(match) == 0 || match[1] != kind {
			continue
		}
		parent, _ := strconv.ParseInt(match[2], 10, 64)
		if _, exists := positions[parent]; !exists || parent == id(item) {
			continue
		}
		parents[id(item)] = parent
		children[parent] = append(children[parent], i)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool { return id(items[children[parent][i]]) > id(items[children[parent][j]]) })
	}
	out := make([]T, 0, len(items))
	seen := map[int64]bool{}
	var appendCard func(int)
	appendCard = func(i int) {
		key := id(items[i])
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, items[i])
		for _, child := range children[key] {
			appendCard(child)
		}
	}
	for i, item := range items {
		if parents[id(item)] == 0 {
			appendCard(i)
		}
	}
	for i := range items {
		appendCard(i)
	}
	return out
}

func cardLinkNumber(key string) int64 {
	m := cardLinkNumberPattern.FindStringSubmatch(strings.TrimSpace(key))
	if len(m) == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(m[2], 10, 64)
	return n
}

// Allocate from database rows, including disabled rows whose keys are reserved.
func nextCopiedCardLink(source string, keys []string) (string, error) {
	var max int64
	for _, key := range keys {
		if n := cardLinkNumber(key); n > max {
			max = n
		}
	}
	if max == math.MaxInt64 {
		return "", fmt.Errorf("%w: 链接编号已达到上限", ErrLiveCardCopyInvalid)
	}
	m := cardLinkNumberPattern.FindStringSubmatch(strings.TrimSpace(source))
	if len(m) > 0 {
		return m[1] + strconv.FormatInt(max+1, 10) + m[3], nil
	}
	return strconv.FormatInt(max+1, 10) + "号链接", nil
}

func copiedCardName(source string, names []string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("%w: 请先填写商品名称", ErrLiveCardCopyInvalid)
	}
	used := map[string]bool{}
	for _, name := range names {
		used[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for n := 1; n <= 100000; n++ {
		candidate := source + fmt.Sprintf("%02d", n)
		if utf8.RuneCountInString(candidate) > 255 {
			return "", fmt.Errorf("%w: 商品名称过长，请缩短后再复制", ErrLiveCardCopyInvalid)
		}
		if !used[strings.ToLower(candidate)] {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: 副本名称数量已达到上限", ErrLiveCardCopyInvalid)
}

func lockCardCopyPlan(ctx context.Context, tx *sql.Tx, tenant, plan int64) error {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE`, plan, tenant).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveAgentPlanNotFound
	}
	return err
}

func (s *Store) CopyLiveAgentPlanProductLink(ctx context.Context, tenant, plan, sourceID, actor, expected int64) (model.LiveAgentPlanProductLink, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	defer tx.Rollback()
	if err = lockCardCopyPlan(ctx, tx, tenant, plan); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	item, err := copyProductCardTx(ctx, tx, tenant, plan, sourceID, actor, expected)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return item, nil
}

func copyProductCardTx(ctx context.Context, tx *sql.Tx, tenant, plan, sourceID, actor, expected int64) (model.LiveAgentPlanProductLink, error) {
	source, err := scanLiveAgentPlanProductLink(tx.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=? AND status='active' FOR UPDATE`, sourceID, tenant, plan))
	if errors.Is(err, sql.ErrNoRows) {
		return source, ErrLiveAgentPlanProductLinkNotFound
	}
	if err != nil {
		return source, err
	}
	if expected > 0 && source.VersionNo != expected {
		return source, ErrLiveAgentPlanProductLinkVersionConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT link_key, product_name FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan)
	if err != nil {
		return source, err
	}
	keys, names := []string{}, []string{}
	for rows.Next() {
		var key, name string
		if err = rows.Scan(&key, &name); err != nil {
			rows.Close()
			return source, err
		}
		keys = append(keys, key)
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return source, err
	}
	key, err := nextCopiedCardLink(source.LinkKey, keys)
	if err != nil {
		return source, err
	}
	name, err := copiedCardName(source.ProductName, names)
	if err != nil {
		return source, err
	}
	ref := fmt.Sprintf("copy:product:%d:v%d", source.ID, source.VersionNo)
	result, err := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_product_links (
 tenant_id,plan_id,link_key,product_name,room_roles_json,field_labels_json,spec,daily_price,quantity,audience,
 source_quote,source_review_bucket,source_review_reason,source_type,source_ref,status,version_no,created_by_user_id,updated_by_user_id)
 SELECT tenant_id,plan_id,?,?,room_roles_json,field_labels_json,spec,daily_price,quantity,audience,
 source_quote,source_review_bucket,source_review_reason,'manual_copy',?,'active',1,?,?
 FROM live_agent_plan_product_links WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'`, key, name, ref, actor, actor, sourceID, tenant, plan)
	if err != nil {
		return source, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return source, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_product_link_revisions (
 tenant_id,plan_id,product_link_id,version_no,action,link_key,product_name,room_roles_json,field_labels_json,spec,daily_price,quantity,audience,status,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,actor_user_id)
 SELECT tenant_id,plan_id,id,version_no,'copy',link_key,product_name,room_roles_json,field_labels_json,spec,daily_price,quantity,audience,status,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,?
 FROM live_agent_plan_product_links WHERE id=? AND tenant_id=? AND plan_id=?`, actor, id, tenant, plan); err != nil {
		return source, err
	}
	// Independent attribute IDs and V1 audit records; the original attributes stay intact.
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_product_attributes (
 tenant_id,plan_id,product_link_id,attribute_code,display_name,value_text,unit,display_type,display_priority,source_quote,source_type,source_ref,status,version_no,created_by_user_id,updated_by_user_id)
 SELECT tenant_id,plan_id,?,attribute_code,display_name,value_text,unit,display_type,display_priority,source_quote,'manual_copy',?,'active',1,?,?
 FROM live_agent_plan_product_attributes WHERE product_link_id=? AND tenant_id=? AND plan_id=? AND status='active'`, id, ref, actor, actor, sourceID, tenant, plan); err != nil {
		return source, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_product_attribute_revisions (
 tenant_id,plan_id,product_link_id,attribute_id,version_no,action,attribute_code,display_name,value_text,unit,display_type,display_priority,status,source_quote,source_type,source_ref,actor_user_id)
 SELECT tenant_id,plan_id,product_link_id,id,version_no,'copy',attribute_code,display_name,value_text,unit,display_type,display_priority,status,source_quote,source_type,source_ref,?
 FROM live_agent_plan_product_attributes WHERE product_link_id=? AND tenant_id=? AND plan_id=? AND status='active'`, actor, id, tenant, plan); err != nil {
		return source, err
	}
	item, err := scanLiveAgentPlanProductLink(tx.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, id, tenant, plan))
	if err != nil {
		return item, err
	}
	attributes, err := tx.QueryContext(ctx, liveAgentPlanProductAttributeSelect+` WHERE product_link_id=? AND tenant_id=? AND plan_id=? AND status='active' ORDER BY display_priority,id`, id, tenant, plan)
	if err != nil {
		return item, err
	}
	defer attributes.Close()
	item.Attributes = []model.LiveAgentPlanProductAttribute{}
	for attributes.Next() {
		a, e := scanLiveAgentPlanProductAttribute(attributes)
		if e != nil {
			return item, e
		}
		item.Attributes = append(item.Attributes, a)
	}
	return item, attributes.Err()
}

// A benefit copy moves to the next existing product, never an invented link.
func (s *Store) CopyLiveAgentPlanBenefit(ctx context.Context, tenant, plan, sourceID, actor, expected int64) (model.LiveAgentPlanBenefit, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	defer tx.Rollback()
	if err = lockCardCopyPlan(ctx, tx, tenant, plan); err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	source, err := scanLiveAgentPlanBenefit(tx.QueryRowContext(ctx, liveAgentPlanBenefitSelect+` WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled' FOR UPDATE`, sourceID, tenant, plan))
	if errors.Is(err, sql.ErrNoRows) {
		return source, ErrLiveAgentPlanBenefitNotFound
	}
	if err != nil {
		return source, err
	}
	if expected > 0 && source.VersionNo != expected {
		return source, ErrLiveAgentPlanBenefitVersionConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT link_key FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? AND status='active' FOR UPDATE`, tenant, plan)
	if err != nil {
		return source, err
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return source, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return source, err
	}
	target, err := nextExistingCardLink(source.LinkKey, keys)
	if err != nil {
		return source, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT benefit_key, product_name FROM live_agent_plan_benefits WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan)
	if err != nil {
		return source, err
	}
	usedKeys, names := []string{}, []string{}
	for rows.Next() {
		var key, name string
		if err = rows.Scan(&key, &name); err != nil {
			rows.Close()
			return source, err
		}
		usedKeys = append(usedKeys, key)
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return source, err
	}
	key, err := copiedCardName(source.Key, usedKeys)
	if err != nil {
		return source, err
	}
	label := source.ProductName
	if strings.TrimSpace(label) == "" {
		label = source.Key
	}
	name, err := copiedCardName(label, names)
	if err != nil {
		return source, err
	}
	ref := fmt.Sprintf("copy:benefit:%d:v%d", source.ID, source.VersionNo)
	result, err := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_benefits (
 tenant_id,plan_id,benefit_key,link_key,product_name,activity_price,gift,activity_text,validity_mode,starts_at,ends_at,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,status,version_no,created_by_user_id,updated_by_user_id)
 SELECT tenant_id,plan_id,?,?,?,activity_price,gift,activity_text,validity_mode,starts_at,ends_at,source_quote,source_review_bucket,source_review_reason,'manual_copy',?,status,1,?,?
 FROM live_agent_plan_benefits WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled'`, key, target, name, ref, actor, actor, sourceID, tenant, plan)
	if err != nil {
		return source, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return source, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_benefit_revisions (
 tenant_id,plan_id,benefit_id,version_no,action,benefit_key,link_key,product_name,activity_price,gift,activity_text,validity_mode,starts_at,ends_at,status,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,actor_user_id)
 SELECT tenant_id,plan_id,id,version_no,'copy',benefit_key,link_key,product_name,activity_price,gift,activity_text,validity_mode,starts_at,ends_at,status,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,?
 FROM live_agent_plan_benefits WHERE id=? AND tenant_id=? AND plan_id=?`, actor, id, tenant, plan); err != nil {
		return source, err
	}
	item, err := scanLiveAgentPlanBenefit(tx.QueryRowContext(ctx, liveAgentPlanBenefitSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, id, tenant, plan))
	if err != nil {
		return item, err
	}
	if err = tx.Commit(); err != nil {
		return item, err
	}
	return item, nil
}

func nextExistingCardLink(source string, keys []string) (string, error) {
	number := cardLinkNumber(source)
	if strings.TrimSpace(source) != "" && number == 0 {
		return "", fmt.Errorf("%w: 原福利链接编号无法识别，请先修改为真实数字链接", ErrLiveCardCopyInvalid)
	}
	var next int64
	target := ""
	for _, key := range keys {
		n := cardLinkNumber(key)
		if n > number && (next == 0 || n < next) {
			next = n
			target = key
		}
	}
	if target == "" {
		return "", fmt.Errorf("%w: 当前方案没有下一条真实商品链接，请先添加商品或复制商品卡", ErrLiveCardCopyInvalid)
	}
	return target, nil
}
