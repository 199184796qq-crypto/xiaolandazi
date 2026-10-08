package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"sort"
	"strings"
)

var ErrAnchorAnalysisConflict = errors.New("综合风格版本已变化，请刷新重试")
var ErrAnchorAnalysisVersionNotFound = errors.New("综合分析版本不存在")
var ErrAnchorSinglePromotionSkipped = errors.New("主播样本已变化，本次仅保存单样本分析")

const anchorAnalysisVersionSelect = `SELECT id,version_no,kind,sample_ids_json,profile_json,source_text,created_at FROM live_anchor_style_analysis_versions`

func scanAnchorAnalysisVersion(row interface{ Scan(...any) error }) (model.LiveAnchorAnalysisVersion, error) {
	var item model.LiveAnchorAnalysisVersion
	var ids, profile string
	err := row.Scan(&item.ID, &item.VersionNo, &item.Kind, &ids, &profile, &item.SourceText, &item.CreatedAt)
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(profile), &item.Profile); err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(ids), &item.SampleIDs); err != nil {
		return item, err
	}
	if item.Profile.Delivery != nil {
		item.Text = item.Profile.Delivery.MetaPromptMD
	}
	return item, nil
}

func (s *Store) LiveAnchorAnalysisState(ctx context.Context, tenant, style int64) (model.LiveAnchorAnalysisState, error) {
	state := model.LiveAnchorAnalysisState{Items: []model.LiveAnchorAnalysisVersion{}}
	err := s.db.QueryRowContext(ctx, `SELECT active_analysis_version_id FROM live_anchor_styles WHERE id=? AND tenant_id=? AND status='active'`, style, tenant).Scan(&state.ActiveVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrLiveAnchorStyleNotFound
	}
	if err != nil {
		return state, err
	}
	rows, err := s.db.QueryContext(ctx, anchorAnalysisVersionSelect+` WHERE tenant_id=? AND style_id=? ORDER BY version_no DESC`, tenant, style)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		item, e := scanAnchorAnalysisVersion(rows)
		if e != nil {
			return state, e
		}
		state.Items = append(state.Items, item)
	}
	return state, rows.Err()
}

func (s *Store) ActiveLiveAnchorAnalysis(ctx context.Context, tenant, style int64) (model.LiveAnchorAnalysisVersion, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT active_analysis_version_id FROM live_anchor_styles WHERE id=? AND tenant_id=? AND status='active'`, style, tenant).Scan(&id)
	if err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	if id == 0 {
		return model.LiveAnchorAnalysisVersion{}, ErrAnchorAnalysisVersionNotFound
	}
	return s.LiveAnchorAnalysisVersion(ctx, tenant, style, id)
}

// Legacy analyses have a complete profile but no adopted-version row. Only an
// exact source fingerprint can supply their score-calibration sample snapshot.
func legacyEditableAnchorAnalysis(profile model.LiveAgentPlanAnchorStyleProfile, samples []model.LiveAnchorStyleSample) (model.LiveAnchorAnalysisVersion, error) {
	item := model.LiveAnchorAnalysisVersion{Kind: "legacy_import", Profile: profile}
	if profile.Delivery == nil || strings.TrimSpace(profile.Delivery.MetaPromptMD) == "" || profile.Delivery.SourceSHA256 == "" {
		return item, errors.New("旧风格缺少原始分析指纹，请先重新分析样本")
	}
	item.Text = profile.Delivery.MetaPromptMD
	samples = append([]model.LiveAnchorStyleSample(nil), samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i].ID < samples[j].ID })
	texts, ids := []string{}, []int64{}
	for _, sample := range samples {
		text, _ := model.TruncateAnchorSample(sample.ReadableText)
		if fmt.Sprintf("%x", sha256.Sum256([]byte(text))) == profile.Delivery.SourceSHA256 {
			item.SourceText, item.SampleIDs = text, []int64{sample.ID}
			return item, nil
		}
		texts, ids = append(texts, text), append(ids, sample.ID)
	}
	corpus := strings.Join(texts, "\n\n")
	if corpus != "" && fmt.Sprintf("%x", sha256.Sum256([]byte(corpus))) == profile.Delivery.SourceSHA256 {
		item.SourceText, item.SampleIDs = corpus, ids
		return item, nil
	}
	return item, errors.New("找不到与旧风格匹配的原始样本，未使用其他样本代替；请先重新分析")
}

func AnchorAnalysisProfileSHA(profile model.LiveAgentPlanAnchorStyleProfile) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(encodeJSON(decodeLiveAnchorStyleProfile(encodeJSON(profile))))))
}

func (s *Store) EditableLiveAnchorAnalysis(ctx context.Context, tenant, style int64) (model.LiveAnchorAnalysisVersion, error) {
	item, err := s.ActiveLiveAnchorAnalysis(ctx, tenant, style)
	if !errors.Is(err, ErrAnchorAnalysisVersionNotFound) {
		return item, err
	}
	presenter, err := s.GetLiveAnchorStyle(ctx, tenant, style)
	if err != nil {
		return item, err
	}
	samples, err := s.ListLiveAnchorStyleSamples(ctx, tenant, style)
	if err != nil {
		return item, err
	}
	return legacyEditableAnchorAnalysis(presenter.Profile, samples)
}
func (s *Store) LiveAnchorAnalysisVersion(ctx context.Context, tenant, style, id int64) (model.LiveAnchorAnalysisVersion, error) {
	item, err := scanAnchorAnalysisVersion(s.db.QueryRowContext(ctx, anchorAnalysisVersionSelect+` WHERE id=? AND tenant_id=? AND style_id=?`, id, tenant, style))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrAnchorAnalysisVersionNotFound
	}
	return item, err
}

func (s *Store) SaveLiveAnchorAnalysisVersion(ctx context.Context, tenant, style, actor, expected int64, kind string, ids []int64, source string, profile model.LiveAgentPlanAnchorStyleProfile, expectedProfileSHA ...string) (model.LiveAnchorAnalysisVersion, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	defer tx.Rollback()
	var active int64
	var oldRaw string
	err = tx.QueryRowContext(ctx, `SELECT active_analysis_version_id,profile_json FROM live_anchor_styles WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE`, style, tenant).Scan(&active, &oldRaw)
	if err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	if kind == "single_sample" {
		// Lock the complete tenant/presenter sample range, not only analyzed rows.
		// Inserts during analysis must not silently promote a single result.
		rows, e := tx.QueryContext(ctx, `SELECT id FROM live_anchor_style_samples WHERE tenant_id=? AND style_id=? FOR UPDATE`, tenant, style)
		if e != nil {
			return model.LiveAnchorAnalysisVersion{}, e
		}
		current := []int64{}
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return model.LiveAnchorAnalysisVersion{}, e
			}
			current = append(current, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return model.LiveAnchorAnalysisVersion{}, e
		}
		if len(ids) != 1 || len(current) != 1 || current[0] != ids[0] {
			return model.LiveAnchorAnalysisVersion{}, ErrAnchorSinglePromotionSkipped
		}
	}
	if active != expected {
		return model.LiveAnchorAnalysisVersion{}, ErrAnchorAnalysisConflict
	}
	if len(expectedProfileSHA) > 0 && AnchorAnalysisProfileSHA(decodeLiveAnchorStyleProfile(oldRaw)) != expectedProfileSHA[0] {
		return model.LiveAnchorAnalysisVersion{}, ErrAnchorAnalysisConflict
	}
	var number int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_no),0) FROM live_anchor_style_analysis_versions WHERE tenant_id=? AND style_id=?`, tenant, style).Scan(&number); err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	// Preserve the pre-upgrade active result before creating the first new result.
	old := decodeLiveAnchorStyleProfile(oldRaw)
	if number == 0 && old.Delivery != nil && old.Delivery.MetaPromptMD != "" {
		oldSource := ""
		oldIDs := []int64{}
		if source != "" && fmt.Sprintf("%x", sha256.Sum256([]byte(source))) == old.Delivery.SourceSHA256 {
			oldSource, oldIDs = source, ids
		}
		rows, e := tx.QueryContext(ctx, `SELECT id,readable_text FROM live_anchor_style_samples WHERE tenant_id=? AND style_id=? ORDER BY id`, tenant, style)
		if e != nil {
			return model.LiveAnchorAnalysisVersion{}, e
		}
		for rows.Next() {
			var id int64
			var text string
			if e = rows.Scan(&id, &text); e != nil {
				rows.Close()
				return model.LiveAnchorAnalysisVersion{}, e
			}
			if fmt.Sprintf("%x", sha256.Sum256([]byte(text))) == old.Delivery.SourceSHA256 {
				oldSource = text
				oldIDs = []int64{id}
				break
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return model.LiveAnchorAnalysisVersion{}, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO live_anchor_style_analysis_versions (tenant_id,style_id,version_no,kind,sample_ids_json,source_text,profile_json,created_by_user_id) VALUES (?,?,1,'legacy_import',?,?,?,?)`, tenant, style, encodeJSON(oldIDs), oldSource, oldRaw, actor); err != nil {
			return model.LiveAnchorAnalysisVersion{}, err
		}
		number = 1
	}
	number++
	result, err := tx.ExecContext(ctx, `INSERT INTO live_anchor_style_analysis_versions (tenant_id,style_id,version_no,kind,sample_ids_json,source_text,profile_json,created_by_user_id) VALUES (?,?,?,?,?,?,?,?)`, tenant, style, number, kind, encodeJSON(ids), source, encodeJSON(profile), actor)
	if err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE live_anchor_styles SET active_analysis_version_id=?,profile_json=?,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'`, id, encodeJSON(profile), actor, style, tenant); err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_anchor_style_analysis_activations (tenant_id,style_id,from_version_id,to_version_id,actor_user_id) VALUES (?,?,?,?,?)`, tenant, style, active, id, actor); err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.LiveAnchorAnalysisVersion{}, err
	}
	return s.LiveAnchorAnalysisVersion(ctx, tenant, style, id)
}

func (s *Store) UseLiveAnchorAnalysisVersion(ctx context.Context, tenant, style, id, actor, expected int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int64
	if err = tx.QueryRowContext(ctx, `SELECT active_analysis_version_id FROM live_anchor_styles WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE`, style, tenant).Scan(&active); err != nil {
		return err
	}
	if active != expected {
		return ErrAnchorAnalysisConflict
	}
	item, err := scanAnchorAnalysisVersion(tx.QueryRowContext(ctx, anchorAnalysisVersionSelect+` WHERE id=? AND tenant_id=? AND style_id=?`, id, tenant, style))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAnchorAnalysisVersionNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE live_anchor_styles SET active_analysis_version_id=?,profile_json=?,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'`, id, encodeJSON(item.Profile), actor, style, tenant); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_anchor_style_analysis_activations (tenant_id,style_id,from_version_id,to_version_id,actor_user_id) VALUES (?,?,?,?,?)`, tenant, style, active, id, actor); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateAnchorDescription(ctx context.Context, tenant, style, actor int64, description string) (model.LiveAnchorStyle, error) {
	if _, err := s.GetLiveAnchorStyle(ctx, tenant, style); err != nil {
		return model.LiveAnchorStyle{}, err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE live_anchor_styles SET description=?,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'`, description, actor, style, tenant)
	if err != nil {
		return model.LiveAnchorStyle{}, err
	}
	return s.GetLiveAnchorStyle(ctx, tenant, style)
}
