package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

type recommendationConnector struct{ c *recommendationConn }

func (c recommendationConnector) Connect(context.Context) (driver.Conn, error) { return c.c, nil }
func (c recommendationConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type recommendationConn struct {
	source                                      model.LiveAnchorStyle
	copied                                      *model.LiveAnchorStyle
	published                                   *model.AnchorRecommendation
	writes                                      []string
	committed, rolled, unavailable, failVersion bool
}

func (*recommendationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*recommendationConn) Close() error { return nil }
func (c *recommendationConn) Begin() (driver.Tx, error) {
	c.committed = false
	c.rolled = false
	return recommendationTx{c}, nil
}

type recommendationTx struct{ c *recommendationConn }

func (t recommendationTx) Commit() error   { t.c.committed = true; return nil }
func (t recommendationTx) Rollback() error { t.c.rolled = true; return nil }

type recommendationResult int64

func (r recommendationResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r recommendationResult) RowsAffected() (int64, error) { return 1, nil }
func (c *recommendationConn) recommendationRows() *contentPolicyTestRows {
	rec := model.AnchorRecommendation{ID: 1, SourceTenantID: 14, SourceStyleID: 7, Name: c.source.Name, Introduction: c.source.Description, Summary: strings.Repeat("风", 50), Snapshot: c.source, VersionNo: 1, Status: "active"}
	if c.published != nil {
		rec = *c.published
	}
	rows := &contentPolicyTestRows{columns: strings.Fields("id tenant style name introduction summary snapshot version status updated")}
	if !c.unavailable {
		rows.values = [][]driver.Value{{rec.ID, rec.SourceTenantID, rec.SourceStyleID, rec.Name, rec.Introduction, rec.Summary, encodeJSON(rec.Snapshot), rec.VersionNo, rec.Status, time.Now()}}
	}
	return rows
}
func (c *recommendationConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "JOIN mgmt_tenants"):
		if len(a) != 2 || a[0].Value != "%主播%" || a[1].Value != "%主播%" {
			return nil, errors.New("unbounded or invalid source search")
		}
		return &contentPolicyTestRows{columns: strings.Fields("id tenant tenant_name name description avatar data profile")}, nil
	case strings.HasPrefix(q, recommendationSelect):
		return c.recommendationRows(), nil
	case strings.Contains(q, "SELECT id,status FROM live_anchor_styles"):
		if len(a) != 2 || a[0].Value != int64(99) || a[1].Value != int64(1) {
			return nil, errors.New("unscoped import lookup")
		}
		rows := &contentPolicyTestRows{columns: []string{"id", "status"}}
		if c.copied != nil {
			rows.values = [][]driver.Value{{c.copied.ID, "active"}}
		}
		return rows, nil
	case strings.Contains(q, "SELECT name,description,avatar_type"):
		if len(a) != 2 || a[0].Value != int64(7) || a[1].Value != int64(14) || !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("source read must be scoped and locked")
		}
		s := c.source
		return &contentPolicyTestRows{columns: strings.Fields("name description avatar data profile"), values: [][]driver.Value{{s.Name, s.Description, s.AvatarType, s.AvatarURL, encodeJSON(s.Profile)}}}, nil
	case strings.Contains(q, "SELECT plugin_id,plugin_version"):
		if a[0].Value != int64(14) || a[1].Value != int64(7) {
			return nil, errors.New("unscoped source plugins")
		}
		return &contentPolicyTestRows{columns: strings.Fields("plugin version enabled parameters")}, nil
	case strings.Contains(q, "FROM live_anchor_style_plugins"):
		if a[0].Value != int64(99) || a[1].Value != int64(107) {
			return nil, errors.New("unscoped copied plugins")
		}
		return &contentPolicyTestRows{columns: strings.Fields("id plugin version enabled parameters updated")}, nil
	case strings.HasPrefix(q, liveAnchorStyleSelect):
		if c.copied == nil || len(a) != 2 || a[0].Value != int64(107) || a[1].Value != int64(99) {
			return nil, errors.New("unscoped copied style")
		}
		s := c.copied
		return &contentPolicyTestRows{columns: strings.Fields("id tenant name description status profile mode card samples trainings plans creator updater created updated avatar data source"), values: [][]driver.Value{{s.ID, s.TenantID, s.Name, s.Description, "active", encodeJSON(s.Profile), "sample", "", int64(0), int64(0), int64(0), int64(9), int64(9), time.Now(), time.Now(), s.AvatarType, s.AvatarURL, int64(1)}}}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", q)
}
func (c *recommendationConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if strings.Count(q, "?") != len(a) {
		return nil, errors.New("SQL placeholder count mismatch")
	}
	c.writes = append(c.writes, q)
	switch {
	case strings.Contains(q, "INSERT INTO live_anchor_styles"):
		if len(a) != 9 || a[0].Value != int64(99) || a[5].Value != int64(1) {
			return nil, errors.New("import overwrites source or foreign tenant")
		}
		s := model.LiveAnchorStyle{ID: 107, TenantID: 99, Name: a[1].Value.(string), Description: a[2].Value.(string), AvatarType: a[3].Value.(string), AvatarURL: a[4].Value.(string), Profile: decodeLiveAnchorStyleProfile(a[6].Value.(string))}
		c.copied = &s
		return recommendationResult(107), nil
	case strings.Contains(q, "INSERT INTO live_anchor_style_analysis_versions"):
		if c.failVersion {
			return nil, errors.New("version storage failed")
		}
		if a[0].Value != int64(99) || a[1].Value != int64(107) {
			return nil, errors.New("unscoped copied analysis version")
		}
		return recommendationResult(207), nil
	case strings.Contains(q, "UPDATE live_anchor_styles SET active_analysis_version_id"):
		if a[0].Value != int64(207) || a[1].Value != int64(107) || a[2].Value != int64(99) {
			return nil, errors.New("unscoped copied activation")
		}
		return recommendationResult(1), nil
	case strings.Contains(q, "INSERT INTO system_anchor_recommendations"):
		if a[0].Value != int64(14) || a[1].Value != int64(7) {
			return nil, errors.New("wrong recommendation source")
		}
		var snapshot model.LiveAnchorStyle
		if err := json.Unmarshal([]byte(a[5].Value.(string)), &snapshot); err != nil {
			return nil, err
		}
		c.published = &model.AnchorRecommendation{ID: 1, SourceTenantID: 14, SourceStyleID: 7, Name: a[2].Value.(string), Introduction: a[3].Value.(string), Summary: a[4].Value.(string), Snapshot: snapshot, VersionNo: 1, Status: "active"}
		return recommendationResult(1), nil
	}
	return nil, fmt.Errorf("unexpected write: %s", q)
}
func recommendationStore(t *testing.T) (*Store, *recommendationConn) {
	t.Helper()
	c := &recommendationConn{source: model.LiveAnchorStyle{ID: 7, TenantID: 14, Name: "源主播", Description: "原介绍", AvatarType: "female", AvatarURL: "data:image/jpeg;base64,abc", Profile: model.LiveAgentPlanAnchorStyleProfile{Summary: "稳定的风格", ReusableRules: []string{"简洁表达"}}, PluginSettings: []model.LiveAnchorStylePluginSetting{}}}
	c.source.Profile = decodeLiveAnchorStyleProfile(encodeJSON(c.source.Profile))
	database := sql.OpenDB(recommendationConnector{c})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}, c
}
func TestRecommendationImportCopiesScopedVersionAndIsIdempotent(t *testing.T) {
	s, c := recommendationStore(t)
	items, err := s.ImportAnchorRecommendations(context.Background(), 99, 9, []int64{1, 1})
	if err != nil || len(items) != 1 || !c.committed {
		t.Fatal(items, err)
	}
	if items[0].ID == c.source.ID || items[0].TenantID != 99 || items[0].Profile.Summary != c.source.Profile.Summary || items[0].AvatarURL != c.source.AvatarURL || items[0].SampleCount != 0 || items[0].BoundPlanCount != 0 {
		t.Fatal("copy scope or version lost", items[0])
	}
	writes := len(c.writes)
	c.copied.Name = "我的自定义名称"
	c.source.Profile.Summary = "后来更新的系统风格"
	items, err = s.ImportAnchorRecommendations(context.Background(), 99, 9, []int64{1})
	if err != nil || len(c.writes) != writes || items[0].Name != "我的自定义名称" || items[0].Profile.Summary != "稳定的风格" {
		t.Fatal("reimport overwrote user copy", items, err)
	}
	for _, q := range c.writes {
		if strings.Contains(q, "bindings") || strings.Contains(q, "samples") || strings.Contains(q, "trainings") {
			t.Fatal("import bound current plan or copied private samples")
		}
	}
}
func TestRecommendationUnavailableOrFailedVersionNeverCommits(t *testing.T) {
	for _, withdrawn := range []bool{true, false} {
		s, c := recommendationStore(t)
		c.unavailable = withdrawn
		c.failVersion = !withdrawn
		items, err := s.ImportAnchorRecommendations(context.Background(), 99, 9, []int64{1})
		if err == nil || items != nil || c.committed || !c.rolled {
			t.Fatal("failed import committed", items, err)
		}
		if withdrawn && len(c.writes) != 0 {
			t.Fatal("withdrawn style imported")
		}
	}
}
func TestPublishRequiresUnchangedSourceAndPublicListHidesSourceAssets(t *testing.T) {
	s, c := recommendationStore(t)
	source := c.source
	hash := model.AnchorRecommendationSHA(source)
	c.source.Name = "风格已被用户编辑"
	if _, err := s.PublishAnchorRecommendation(context.Background(), source, 9, "系统主播", "介绍", strings.Repeat("风", 50), hash, nil); !errors.Is(err, ErrAnchorRecommendationConflict) || len(c.writes) != 0 || c.committed {
		t.Fatal("stale source published", err)
	}
	c.source = source
	item, err := s.PublishAnchorRecommendation(context.Background(), source, 9, "系统主播", "介绍", strings.Repeat("风", 50), hash, map[string]any{"raw_text": "原始模型输出"})
	if err != nil || item.Name != "系统主播" || !c.committed {
		t.Fatal(item, err)
	}
	items, err := s.ListAnchorRecommendations(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(items)
	for _, key := range []string{"source_tenant_id", "source_style_id", "snapshot", "profile", "generation", "raw_text"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatal("public catalog exposed private source", string(raw))
		}
	}
}
func TestRecommendationSourcesUseManagementTenantDirectory(t *testing.T) {
	s, _ := recommendationStore(t)
	if _, err := s.ListAnchorRecommendationSources(context.Background(), "主播"); err != nil {
		t.Fatal(err)
	}
}
