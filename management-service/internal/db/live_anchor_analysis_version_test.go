package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

func TestLegacyAnalysisUsesOnlyFingerprintMatchedSource(t *testing.T) {
	samples := []model.LiveAnchorStyleSample{{ID: 2, ReadableText: "话术乙"}, {ID: 1, ReadableText: "话术甲"}}
	for _, source := range []string{"话术甲", "话术甲\n\n话术乙"} {
		profile := emptyLiveAnchorStyleProfile()
		profile.Delivery = &model.LiveAnchorDeliverySpec{MetaPromptMD: "【情绪】\n1. 温暖", SourceSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}
		item, err := legacyEditableAnchorAnalysis(profile, samples)
		if err != nil || item.ID != 0 || item.SourceText != source || item.Text != profile.Delivery.MetaPromptMD {
			t.Fatalf("bad legacy snapshot %+v %v", item, err)
		}
		profile.Delivery.SourceSHA256 = "does-not-match"
		if _, err = legacyEditableAnchorAnalysis(profile, samples); err == nil {
			t.Fatal("substituted unmatched original sample")
		}
	}
	if samples[0].ID != 2 {
		t.Fatal("source input order changed")
	}
}

func TestManualAnalysisRejectsConcurrentProfileEditEvenWithoutVersionChange(t *testing.T) {
	for _, valid := range []bool{false, true} {
		c := &analysisVersionConn{active: 2, rows: map[int64]model.LiveAnchorAnalysisVersion{}}
		s := analysisVersionStore(c)
		expected := "outdated-profile"
		if valid {
			expected = AnchorAnalysisProfileSHA(emptyLiveAnchorStyleProfile())
		}
		_, err := s.SaveLiveAnchorAnalysisVersion(context.Background(), 14, 7, 9, 2, "manual_edit", nil, "raw", emptyLiveAnchorStyleProfile(), expected)
		s.db.Close()
		if valid && (err != nil || !c.committed) {
			t.Fatalf("valid fingerprint rejected %v", err)
		}
		if !valid && (!errors.Is(err, ErrAnchorAnalysisConflict) || len(c.writes) != 0 || !c.rolled) {
			t.Fatalf("overwrote concurrent profile %v", err)
		}
	}
}

type analysisVersionConnector struct{ c *analysisVersionConn }

func (c analysisVersionConnector) Connect(context.Context) (driver.Conn, error) { return c.c, nil }
func (analysisVersionConnector) Driver() driver.Driver                          { return openingAssetTestDriver{} }

type analysisVersionConn struct {
	active            int64
	ids               []int64
	rows              map[int64]model.LiveAnchorAnalysisVersion
	writes            []string
	committed, rolled bool
	failProjection    bool
}

func (*analysisVersionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*analysisVersionConn) Close() error                { return nil }
func (c *analysisVersionConn) Begin() (driver.Tx, error) { return analysisVersionTx{c}, nil }
func (c *analysisVersionConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) && opts.Isolation != 0 {
		return nil, errors.New("isolation")
	}
	return c.Begin()
}

type analysisVersionTx struct{ c *analysisVersionConn }

func (t analysisVersionTx) Commit() error   { t.c.committed = true; return nil }
func (t analysisVersionTx) Rollback() error { t.c.rolled = true; return nil }
func (c *analysisVersionConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	rows := &contentPolicyTestRows{}
	switch {
	case strings.Contains(q, "FROM live_anchor_styles"):
		if len(a) != 2 || a[0].Value != int64(7) || a[1].Value != int64(14) || !strings.Contains(q, "status='active'") || !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("missing presenter scope/lock")
		}
		rows.columns = []string{"active"}
		rows.values = [][]driver.Value{{c.active}}
		if strings.Contains(q, "profile_json") {
			rows.columns = append(rows.columns, "profile")
			rows.values[0] = append(rows.values[0], "{}")
		}
	case strings.HasPrefix(q, "SELECT id FROM live_anchor_style_samples"):
		if len(a) != 2 || a[0].Value != int64(14) || a[1].Value != int64(7) || !strings.Contains(q, "FOR UPDATE") || strings.Contains(q, "analysis_status") {
			return nil, errors.New("must lock all samples, not analyzed subset")
		}
		rows.columns = []string{"id"}
		for _, id := range c.ids {
			rows.values = append(rows.values, []driver.Value{id})
		}
	case strings.Contains(q, "MAX(version_no)"):
		rows.columns = []string{"n"}
		rows.values = [][]driver.Value{{int64(2)}}
	case strings.HasPrefix(q, anchorAnalysisVersionSelect):
		if len(a) != 3 || a[1].Value != int64(14) || a[2].Value != int64(7) {
			return nil, errors.New("unscoped version")
		}
		rows.columns = []string{"id", "version_no", "kind", "sample_ids_json", "profile_json", "source_text", "created_at"}
		if item, ok := c.rows[a[0].Value.(int64)]; ok {
			rows.values = [][]driver.Value{{item.ID, item.VersionNo, item.Kind, encodeJSON(item.SampleIDs), encodeJSON(item.Profile), item.SourceText, time.Now()}}
		}
	default:
		return nil, errors.New("unexpected query " + q)
	}
	return rows, nil
}
func (c *analysisVersionConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if strings.Count(q, "?") != len(a) {
		return nil, errors.New("placeholder mismatch")
	}
	c.writes = append(c.writes, q)
	if strings.HasPrefix(q, "INSERT INTO live_anchor_style_analysis_versions") {
		if a[0].Value != int64(14) || a[1].Value != int64(7) {
			return nil, errors.New("unscoped insert")
		}
		c.rows[30] = model.LiveAnchorAnalysisVersion{ID: 30, VersionNo: 3, Kind: a[3].Value.(string), Profile: decodeLiveAnchorStyleProfile(a[6].Value.(string))}
		return cardCopyResult{id: 30}, nil
	}
	if strings.HasPrefix(q, "UPDATE live_anchor_styles") {
		if a[3].Value != int64(7) || a[4].Value != int64(14) {
			return nil, errors.New("unscoped projection")
		}
		if c.failProjection {
			return nil, errors.New("projection failure")
		}
		c.active = a[0].Value.(int64)
		return cardCopyResult{}, nil
	}
	if strings.HasPrefix(q, "INSERT INTO live_anchor_style_analysis_activations") {
		if a[0].Value != int64(14) || a[1].Value != int64(7) {
			return nil, errors.New("unscoped audit")
		}
		return cardCopyResult{}, nil
	}
	return nil, errors.New("unexpected write " + q)
}
func analysisVersionStore(c *analysisVersionConn) *Store {
	return &Store{db: sql.OpenDB(analysisVersionConnector{c})}
}
func TestSingleSamplePromotionRechecksAllSamplesInTransaction(t *testing.T) {
	for _, ids := range [][]int64{{7, 8}, {8}, {}} {
		c := &analysisVersionConn{active: 2, ids: ids, rows: map[int64]model.LiveAnchorAnalysisVersion{}}
		s := analysisVersionStore(c)
		defer s.db.Close()
		_, err := s.SaveLiveAnchorAnalysisVersion(context.Background(), 14, 7, 9, 2, "single_sample", []int64{7}, "raw", model.LiveAgentPlanAnchorStyleProfile{})
		if !errors.Is(err, ErrAnchorSinglePromotionSkipped) || len(c.writes) != 0 || c.committed || !c.rolled {
			t.Fatalf("ids %v: %v writes=%v", ids, err, c.writes)
		}
	}
}
func TestAnalysisVersionStaleActiveCannotOverwrite(t *testing.T) {
	c := &analysisVersionConn{active: 3, ids: []int64{7}, rows: map[int64]model.LiveAnchorAnalysisVersion{}}
	s := analysisVersionStore(c)
	defer s.db.Close()
	_, err := s.SaveLiveAnchorAnalysisVersion(context.Background(), 14, 7, 9, 2, "single_sample", []int64{7}, "raw", model.LiveAgentPlanAnchorStyleProfile{})
	if !errors.Is(err, ErrAnchorAnalysisConflict) || len(c.writes) != 0 || !c.rolled {
		t.Fatal(err, c.writes)
	}
}
func TestSingleSamplePromotionStoresVersionProjectionAuditTogether(t *testing.T) {
	c := &analysisVersionConn{active: 2, ids: []int64{7}, rows: map[int64]model.LiveAnchorAnalysisVersion{}}
	s := analysisVersionStore(c)
	defer s.db.Close()
	_, err := s.SaveLiveAnchorAnalysisVersion(context.Background(), 14, 7, 9, 2, "single_sample", []int64{7}, "raw", model.LiveAgentPlanAnchorStyleProfile{})
	if err != nil || !c.committed || len(c.writes) != 3 || c.active != 30 {
		t.Fatal(err, c.writes)
	}
}
func TestUseHistoryRestoresFullProfileAndAudit(t *testing.T) {
	c := &analysisVersionConn{active: 2, rows: map[int64]model.LiveAnchorAnalysisVersion{1: {ID: 1, VersionNo: 1, SampleIDs: []int64{7}, Profile: emptyLiveAnchorStyleProfile()}}}
	s := analysisVersionStore(c)
	defer s.db.Close()
	if err := s.UseLiveAnchorAnalysisVersion(context.Background(), 14, 7, 1, 9, 2); err != nil || !c.committed || c.active != 1 || len(c.writes) != 2 {
		t.Fatal(err, c.writes)
	}
}
func TestUseHistoryRejectsMissingOrFailedProjection(t *testing.T) {
	for _, fail := range []bool{false, true} {
		c := &analysisVersionConn{active: 2, rows: map[int64]model.LiveAnchorAnalysisVersion{}, failProjection: fail}
		if fail {
			c.rows[1] = model.LiveAnchorAnalysisVersion{ID: 1, VersionNo: 1, SampleIDs: []int64{7}, Profile: emptyLiveAnchorStyleProfile()}
		}
		s := analysisVersionStore(c)
		defer s.db.Close()
		err := s.UseLiveAnchorAnalysisVersion(context.Background(), 14, 7, 1, 9, 2)
		if err == nil || c.committed || !c.rolled {
			t.Fatal("must roll back", err)
		}
	}
}
