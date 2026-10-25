package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
)

// The connector models separate SQL connections and transactional row locks;
// no network/database credentials are read by these tests.
type costSQLState struct {
	mu         sync.Mutex
	rows       map[string]costSQLRow
	failUpdate bool
	locks      int
}
type costSQLRow struct {
	entry livecost.Entry
	at    time.Time
	over  bool
}
type costSQLConnector struct{ state *costSQLState }

func (c costSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &costSQLConn{state: c.state}, nil
}
func (costSQLConnector) Driver() driver.Driver { return costSQLDriver{} }

type costSQLDriver struct{}

func (costSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }

type costSQLConn struct {
	state               *costSQLState
	transaction, locked bool
	staged              map[string]costSQLRow
}

func (*costSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (*costSQLConn) Close() error { return nil }
func (c *costSQLConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *costSQLConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	if options.Isolation != driver.IsolationLevel(sql.LevelReadCommitted) {
		return nil, errors.New("unexpected isolation")
	}
	c.transaction = true
	c.staged = map[string]costSQLRow{}
	return costSQLTx{c}, nil
}

type costSQLTx struct{ conn *costSQLConn }

func (tx costSQLTx) Commit() error {
	c := tx.conn
	for key, row := range c.staged {
		c.state.rows[key] = row
	}
	c.transaction = false
	if c.locked {
		c.locked = false
		c.state.mu.Unlock()
	}
	return nil
}
func (tx costSQLTx) Rollback() error {
	c := tx.conn
	c.transaction = false
	c.staged = nil
	if c.locked {
		c.locked = false
		c.state.mu.Unlock()
	}
	return nil
}
func costSQLKey(tenant, room any, key any) string { return fmt.Sprintf("%v/%v/%v", tenant, room, key) }
func costArgs(args []driver.NamedValue) []any {
	out := make([]any, len(args))
	for i, x := range args {
		out[i] = x.Value
	}
	return out
}
func (c *costSQLConn) ExecContext(_ context.Context, query string, named []driver.NamedValue) (driver.Result, error) {
	args := costArgs(named)
	if strings.Contains(query, "INSERT INTO live_cost_room_locks") {
		if !c.transaction || len(args) != 2 {
			return nil, errors.New("room lock creation must be transactional")
		}
		return driver.RowsAffected(1), nil
	}
	if !c.transaction || !c.locked {
		return nil, errors.New("request write happened without room lock")
	}
	if strings.Contains(query, "INSERT INTO live_cost_requests(") {
		var request livecost.Request
		if err := json.Unmarshal([]byte(args[7].(string)), &request); err != nil {
			return nil, err
		}
		key := costSQLKey(args[0], args[1], args[2])
		if _, exists := c.state.rows[key]; exists {
			return nil, errors.New("duplicate request")
		}
		c.staged[key] = costSQLRow{entry: livecost.Entry{Request: request, Status: args[6].(string), ReservedMicros: args[9].(int64)}, at: args[11].(time.Time), over: args[10].(bool)}
		return driver.RowsAffected(1), nil
	}
	if strings.Contains(query, "UPDATE live_cost_requests SET") {
		if c.state.failUpdate {
			return nil, errors.New("injected update failure")
		}
		key := costSQLKey(args[7], args[8], args[9])
		row := c.state.rows[key]
		row.entry.Status = args[0].(string)
		row.entry.ReservedMicros = args[1].(int64)
		row.entry.ChargedMicros = args[2].(int64)
		row.entry.UsageSource = args[3].(string)
		if err := json.Unmarshal([]byte(args[5].(string)), &row.entry.Receipt); err != nil {
			return nil, err
		}
		c.staged[key] = row
		return driver.RowsAffected(1), nil
	}
	return nil, fmt.Errorf("unexpected cost write: %s", query)
}
func (c *costSQLConn) QueryContext(_ context.Context, query string, named []driver.NamedValue) (driver.Rows, error) {
	args := costArgs(named)
	if strings.Contains(query, "SELECT room_id FROM live_cost_room_locks") {
		if !c.transaction || !strings.Contains(query, "FOR UPDATE") || len(args) != 2 {
			return nil, errors.New("missing room row lock")
		}
		c.state.mu.Lock()
		c.locked = true
		c.state.locks++
		return &costSQLRows{columns: []string{"room_id"}, values: [][]driver.Value{{args[1]}}}, nil
	}
	if !c.locked {
		c.state.mu.Lock()
		defer c.state.mu.Unlock()
	}
	if strings.Contains(query, "SELECT request_json,") {
		if !c.transaction || !c.locked || !strings.Contains(query, "FOR UPDATE") || len(args) != 3 {
			return nil, errors.New("unscoped entry read")
		}
		out := &costSQLRows{columns: []string{"request_json", "receipt_json", "status", "reserved_micros", "charged_micros", "usage_source", "over_budget"}}
		row, ok := c.state.rows[costSQLKey(args[0], args[1], args[2])]
		if ok {
			request, _ := json.Marshal(row.entry.Request)
			receipt, _ := json.Marshal(row.entry.Receipt)
			out.values = append(out.values, []driver.Value{string(request), string(receipt), row.entry.Status, row.entry.ReservedMicros, row.entry.ChargedMicros, row.entry.UsageSource, row.over})
		}
		return out, nil
	}
	if strings.Contains(query, "COALESCE(SUM(") {
		if len(args) != 7 || !strings.Contains(query, "OR status IN ('reserved','unknown')") || !strings.Contains(query, "WHEN in_window AND status") || !strings.Contains(query, "WHEN is_aged AND status") ||
			!strings.Contains(query, "in_window AND priority='optional'") || !strings.Contains(query, "attribution_source='recording_mainline'") || !strings.Contains(query, "JSON_EXTRACT(request_json,'$.attribution.source')") {
			return nil, errors.New("missing rolling-window and historical unresolved separation")
		}
		var values [13]int64
		for _, row := range c.state.rows {
			e := row.entry
			r := e.Request
			if r.TenantID != args[3].(int64) || r.RoomID != args[4].(int64) {
				continue
			}
			pending := e.Status == livecost.StatusReserved || e.Status == livecost.StatusUnknown
			inWindow := !row.at.Before(args[0].(time.Time)) && !row.at.After(args[1].(time.Time))
			if !pending && !inWindow {
				continue
			}
			if pending && inWindow {
				values[5] += e.ReservedMicros
				values[7]++
				if e.Status == livecost.StatusUnknown {
					values[6] += e.ReservedMicros
				}
			}
			if pending && row.at.Before(args[2].(time.Time)) {
				values[9] += e.ReservedMicros
				values[10]++
			}
			if inWindow && r.Priority == livecost.PriorityOptional {
				amount := int64(0)
				if pending {
					amount = e.ReservedMicros
				} else if e.Status == livecost.StatusSettled {
					amount = e.ChargedMicros
				}
				values[11] += amount
				if r.Attribution != nil && r.Attribution.Source == "recording_mainline" {
					values[12] += amount
				}
			}
			if e.Status == livecost.StatusSettled {
				if r.Kind == livecost.KindModel {
					values[0] += e.ChargedMicros
				} else {
					values[1] += e.ChargedMicros
				}
				switch e.UsageSource {
				case livecost.SourceSentTextEstimate:
					values[2] += e.ChargedMicros
				case livecost.SourceSupplierUsage:
					values[3] += e.ChargedMicros
				case livecost.SourceSupplierInvoice:
					values[4] += e.ChargedMicros
				}
				values[8]++
			}
		}
		result := make([]driver.Value, len(values))
		for i, value := range values {
			result[i] = value
		}
		return &costSQLRows{columns: []string{"model", "tts", "estimate", "usage", "invoice", "reserved", "unknown", "unresolved", "settled", "aged_unresolved_micros", "aged_unresolved_requests", "optional", "mainline_optional"}, values: [][]driver.Value{result}}, nil
	}
	if strings.Contains(query, "SELECT request_key FROM live_cost_requests") {
		if len(args) != 4 {
			return nil, errors.New("unscoped invoice lookup")
		}
		out := &costSQLRows{columns: []string{"request_key"}}
		for _, row := range c.state.rows {
			r := row.entry.Request
			if r.TenantID == args[0].(int64) && r.RoomID == args[1].(int64) && r.Provider == args[2].(string) && row.entry.Receipt.ProviderRequestID == args[3].(string) {
				out.values = append(out.values, []driver.Value{r.Key})
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected cost query: %s", query)
}

type costSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *costSQLRows) Columns() []string { return r.columns }
func (*costSQLRows) Close() error        { return nil }
func (r *costSQLRows) Next(values []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(values, r.values[r.index])
	r.index++
	return nil
}
func newCostSQLStore(t *testing.T) (*Store, *costSQLState) {
	t.Helper()
	state := &costSQLState{rows: map[string]costSQLRow{}}
	database := sql.OpenDB(costSQLConnector{state})
	database.SetMaxOpenConns(64)
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}, state
}
func sqlCostRequest(key string) livecost.Request {
	return livecost.Request{Key: key, TenantID: 14, RoomID: 29, Kind: livecost.KindTTS, Provider: "test-tts", Priority: livecost.PriorityOptional, EstimatedMicros: 40000, Characters: 250}
}

func TestLiveCostConcurrentConnectionsCannotOverReserveOptionalBudget(t *testing.T) {
	store, state := newCostSQLStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	policy := livecost.DefaultPolicy()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed, declined := 0, 0
	var failure error
	for index := 0; index < 60; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			res, err := store.ReserveLiveCost(ctx, sqlCostRequest(fmt.Sprintf("request-%d", index)), policy, now)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failure = err
			} else if res.Allowed {
				allowed++
			} else if res.Status == livecost.StatusDeclined {
				declined++
			}
		}(index)
	}
	wg.Wait()
	if failure != nil || allowed != 30 || declined != 30 {
		t.Fatal(allowed, declined, failure)
	}
	summary, err := store.LiveCostSummary(ctx, livecost.Scope{TenantID: 14, RoomID: 29}, policy, now)
	if err != nil || summary.TotalMicros != 1200000 || summary.OptionalMicros != 1200000 || summary.MainlineOptionalMicros != 0 || summary.UnresolvedRequests != 30 || state.locks != 60 {
		t.Fatal(summary, err, state.locks)
	}
}

func TestLiveCostSummarySeparatesMainlineOptionalFromNecessaryAndOtherWork(t *testing.T) {
	store, _ := newCostSQLStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	policy := livecost.DefaultPolicy()
	scope := livecost.Scope{TenantID: 14, RoomID: 29}
	reserve := func(key, priority, source, kind string, amount int64, at time.Time) livecost.Request {
		t.Helper()
		r := sqlCostRequest(key)
		r.Priority, r.Kind, r.EstimatedMicros = priority, kind, amount
		if priority == livecost.PriorityNecessary {
			r.Reason = "首次必要声音"
		}
		if source != "" {
			r.Attribution = &livecost.Attribution{Source: source}
			if source == "recording_mainline" {
				r.Attribution.Round, r.Attribution.TabID = 1, "tab"
			}
		}
		if result, err := store.ReserveLiveCost(ctx, r, policy, at); err != nil || !result.Allowed {
			t.Fatalf("reserve %s: %+v err=%v", key, result, err)
		}
		return r
	}
	finish := func(r livecost.Request, status string, quote *int64) {
		t.Helper()
		receipt := livecost.Receipt{TenantID: 14, RoomID: 29, Status: status, EstimatedMicros: quote}
		if err := store.FinishLiveCost(ctx, scope, r.Key, receipt, now); err != nil {
			t.Fatal(err)
		}
	}
	necessary := reserve("first-audio", livecost.PriorityNecessary, "recording_mainline", livecost.KindTTS, 200_000, now)
	finish(necessary, livecost.ReceiptSuccess, nil)
	mainModel := reserve("mainline-model", livecost.PriorityOptional, "recording_mainline", livecost.KindModel, 100_000, now)
	paidQuote := int64(90_000)
	finish(mainModel, livecost.ReceiptSuccess, &paidQuote)
	mainUnknown := reserve("mainline-unknown", livecost.PriorityOptional, "recording_mainline", livecost.KindTTS, 150_000, now)
	finish(mainUnknown, livecost.ReceiptUnknown, nil)
	reserve("other-optional", livecost.PriorityOptional, "other", livecost.KindModel, 100_000, now)
	legacy := reserve("unattributed-optional", livecost.PriorityOptional, "", livecost.KindTTS, 50_000, now)
	finish(legacy, livecost.ReceiptSuccess, nil)
	old := reserve("old-unknown", livecost.PriorityOptional, "recording_mainline", livecost.KindTTS, 90_000, now.Add(-2*time.Hour))
	finish(old, livecost.ReceiptUnknown, nil)
	unsent := reserve("known-unsent", livecost.PriorityOptional, "recording_mainline", livecost.KindTTS, 40_000, now)
	finish(unsent, livecost.ReceiptNotSent, nil)
	declined := sqlCostRequest("too-large-optional")
	declined.EstimatedMicros = 900_000
	if result, err := store.ReserveLiveCost(ctx, declined, policy, now); err != nil || result.Allowed || result.Status != livecost.StatusDeclined {
		t.Fatal(result, err)
	}
	otherRoom := sqlCostRequest("other-room")
	otherRoom.RoomID = 30
	if result, err := store.ReserveLiveCost(ctx, otherRoom, policy, now); err != nil || !result.Allowed {
		t.Fatal(result, err)
	}
	summary, err := store.LiveCostSummary(ctx, scope, policy, now)
	if err != nil || summary.TotalMicros != 590_000 || summary.OptionalMicros != 390_000 || summary.MainlineOptionalMicros != 240_000 ||
		summary.ReservedMicros != 250_000 || summary.UnknownMicros != 150_000 || summary.AgedUnresolvedMicros != 90_000 || summary.AgedUnresolvedRequests != 1 {
		t.Fatalf("cohorts double-counted pending/necessary/aged/unrelated work: %+v err=%v", summary, err)
	}
}

func TestLiveCostOptionalReservationChecksItsOwnSoftLineAndWholeRoomTarget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		necessary int64
		optional  int64
		wantAllow bool
	}{
		{"both_exact_boundaries", 100_000, 1_200_000, true},
		{"room_target_before_optional_soft_line", 300_000, 1_000_000, true},
		{"room_target_would_be_crossed", 300_000, 1_000_001, false},
		{"optional_soft_line_would_be_crossed", 0, 1_200_001, false},
		{"necessary_already_at_target", 1_300_000, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newCostSQLStore(t)
			ctx, now, policy := context.Background(), time.Now().UTC(), livecost.DefaultPolicy()
			if tc.necessary > 0 {
				r := sqlCostRequest("necessary")
				r.Priority, r.EstimatedMicros = livecost.PriorityNecessary, tc.necessary
				r.Reason = "首次必要声音"
				if result, err := store.ReserveLiveCost(ctx, r, policy, now); err != nil || !result.Allowed {
					t.Fatal(result, err)
				}
			}
			optional := sqlCostRequest("optional")
			optional.EstimatedMicros = tc.optional
			result, err := store.ReserveLiveCost(ctx, optional, policy, now)
			if err != nil || result.Allowed != tc.wantAllow {
				t.Fatalf("optional result=%+v err=%v", result, err)
			}
			if tc.wantAllow {
				more := sqlCostRequest("one-more-optional")
				more.EstimatedMicros = 1
				if result, err := store.ReserveLiveCost(ctx, more, policy, now); err != nil || result.Allowed || result.Status != livecost.StatusDeclined {
					t.Fatal("optional crossed an exhausted boundary", result, err)
				}
			}
			// Neither optional limit is allowed to interrupt required supply.
			required := sqlCostRequest("required-overuse")
			required.Priority, required.EstimatedMicros = livecost.PriorityNecessary, 1_400_000
			required.Reason = "事实变化后的必要声音"
			if result, err := store.ReserveLiveCost(ctx, required, policy, now); err != nil || !result.Allowed || !result.OverBudget {
				t.Fatal("required supply was blocked or overuse lost", result, err)
			}
		})
	}
}
func TestLiveCostDuplicateScopedReservationAndNecessaryOverspend(t *testing.T) {
	store, _ := newCostSQLStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	policy := livecost.DefaultPolicy()
	request := sqlCostRequest("same")
	request.EstimatedMicros = 1200000
	first, err := store.ReserveLiveCost(ctx, request, policy, now)
	if err != nil || !first.Allowed {
		t.Fatal(first, err)
	}
	duplicate, err := store.ReserveLiveCost(ctx, request, policy, now)
	if err != nil || duplicate.Allowed || !duplicate.Existing {
		t.Fatal(duplicate, err)
	}
	changed := request
	changed.Fingerprint = "different"
	if _, err = store.ReserveLiveCost(ctx, changed, policy, now); !errors.Is(err, livecost.ErrRequestConflict) {
		t.Fatal(err)
	}
	otherTenant := request
	otherTenant.TenantID = 15
	if res, e := store.ReserveLiveCost(ctx, otherTenant, policy, now); e != nil || !res.Allowed {
		t.Fatal(res, e)
	}
	necessary := sqlCostRequest("necessary")
	necessary.Priority = livecost.PriorityNecessary
	necessary.Reason = "事实更新必须换声"
	necessary.EstimatedMicros = 200000
	if res, e := store.ReserveLiveCost(ctx, necessary, policy, now); e != nil || !res.Allowed || !res.OverBudget {
		t.Fatal(res, e)
	}
	denied := sqlCostRequest("optional")
	if res, e := store.ReserveLiveCost(ctx, denied, policy, now); e != nil || res.Allowed || res.Status != livecost.StatusDeclined {
		t.Fatal(res, e)
	}
}
func TestLiveCostUnknownSurvivesWindowAndKnownUnsentReleasesOnce(t *testing.T) {
	store, _ := newCostSQLStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	policy := livecost.DefaultPolicy()
	scope := livecost.Scope{TenantID: 14, RoomID: 29}
	request := sqlCostRequest("old")
	if _, err := store.ReserveLiveCost(ctx, request, policy, now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	unknown := livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptUnknown, ProviderRequestID: "unknown-supplier"}
	if err := store.FinishLiveCost(ctx, scope, request.Key, unknown, now); err != nil {
		t.Fatal(err)
	}
	summary, err := store.LiveCostSummary(ctx, scope, policy, now)
	if err != nil || summary.TotalMicros != 0 || summary.UnknownMicros != 0 || summary.AgedUnresolvedMicros != 40000 || summary.AgedUnresolvedRequests != 1 {
		t.Fatal(summary, err)
	}
	notSent := livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptNotSent}
	if err = store.FinishLiveCost(ctx, scope, request.Key, notSent, now); !errors.Is(err, livecost.ErrReceiptConflict) {
		t.Fatal("unknown refunded", err)
	}
	request.Key = "not-sent"
	if _, err = store.ReserveLiveCost(ctx, request, policy, now); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = store.FinishLiveCost(ctx, scope, request.Key, notSent, now); err != nil {
			t.Fatal(err)
		}
	}
	summary, err = store.LiveCostSummary(ctx, scope, policy, now)
	if err != nil || summary.TotalMicros != 0 || summary.AgedUnresolvedMicros != 40000 || summary.AgedUnresolvedRequests != 1 {
		t.Fatal(summary, err)
	}
}

func TestLiveCostAgedUnknownKeepsFenceWithoutBlockingNewSessionRefresh(t *testing.T) {
	store, state := newCostSQLStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC)
	policy := livecost.DefaultPolicy()
	scope := livecost.Scope{TenantID: 14, RoomID: 29}
	request := sqlCostRequest("old-session-unknown")
	request.SessionID = 479
	request.EstimatedMicros = policy.OptionalLimitMicros
	request.Attribution = &livecost.Attribution{Source: "recording_mainline", Round: 2, TabID: "tab-a", SourceIndex: 3}
	if res, err := store.ReserveLiveCost(ctx, request, policy, now); err != nil || !res.Allowed {
		t.Fatal(res, err)
	}
	if err := store.FinishLiveCost(ctx, scope, request.Key, livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptUnknown}, now); err != nil {
		t.Fatal(err)
	}
	// The previous request still occupies its actual rolling hour, including
	// the boundary, rather than being released merely by a new session ID.
	boundary := now.Add(time.Hour)
	current, err := store.LiveCostSummary(ctx, scope, policy, boundary)
	if err != nil || current.OptionalMicros != policy.OptionalLimitMicros || current.MainlineOptionalMicros != policy.OptionalLimitMicros || current.UnknownMicros != policy.OptionalLimitMicros {
		t.Fatal("in-window unknown lost one of its budget cohorts", current, err)
	}
	blocked := sqlCostRequest("new-session-before-expiry")
	blocked.SessionID = 480
	if res, err := store.ReserveLiveCost(ctx, blocked, policy, boundary); err != nil || res.Allowed || res.Status != livecost.StatusDeclined {
		t.Fatal(res, err)
	}
	// Only the budget window advances. The old unknown row, its attribution,
	// reservation amount and its permanent same-key fence remain intact.
	afterWindow := boundary.Add(time.Microsecond)
	summary, err := store.LiveCostSummary(ctx, scope, policy, afterWindow)
	if err != nil || summary.TotalMicros != 0 || summary.OptionalMicros != 0 || summary.MainlineOptionalMicros != 0 || summary.ReservedMicros != 0 || summary.UnresolvedRequests != 0 || summary.AgedUnresolvedMicros != policy.OptionalLimitMicros || summary.AgedUnresolvedRequests != 1 {
		t.Fatal(summary, err)
	}
	duplicate, err := store.ReserveLiveCost(ctx, request, policy, afterWindow)
	if err != nil || duplicate.Allowed || !duplicate.Existing || duplicate.Status != livecost.StatusUnknown {
		t.Fatal("old request became sendable again", duplicate, err)
	}
	stored := state.rows[costSQLKey(request.TenantID, request.RoomID, request.Key)].entry
	if stored.Status != livecost.StatusUnknown || stored.ReservedMicros != policy.OptionalLimitMicros || stored.Request.Attribution == nil || *stored.Request.Attribution != *request.Attribution {
		t.Fatal("historical liability or attribution was lost", stored)
	}
	fresh := sqlCostRequest("new-session-after-expiry")
	fresh.SessionID = 480
	fresh.Attribution = &livecost.Attribution{Source: "recording_mainline", Round: 1, TabID: "tab-a", SourceIndex: 0}
	if res, err := store.ReserveLiveCost(ctx, fresh, policy, afterWindow); err != nil || !res.Allowed {
		t.Fatal("new round stayed blocked by historical unknown", res, err)
	}
	summary, err = store.LiveCostSummary(ctx, scope, policy, afterWindow)
	if err != nil || summary.TotalMicros != fresh.EstimatedMicros || summary.OptionalMicros != fresh.EstimatedMicros || summary.MainlineOptionalMicros != fresh.EstimatedMicros || summary.ReservedMicros != fresh.EstimatedMicros || summary.UnresolvedRequests != 1 || summary.AgedUnresolvedMicros != policy.OptionalLimitMicros || summary.AgedUnresolvedRequests != 1 {
		t.Fatal(summary, err)
	}
	if err = store.FinishLiveCost(ctx, scope, request.Key, livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptNotSent}, afterWindow); !errors.Is(err, livecost.ErrReceiptConflict) {
		t.Fatal("aging silently allowed an unknown refund", err)
	}
}

func TestLiveCostAgedReservedAmountRemainsScopedAndMayBeSettled(t *testing.T) {
	store, _ := newCostSQLStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	policy := livecost.DefaultPolicy()
	scope := livecost.Scope{TenantID: 14, RoomID: 29}
	request := sqlCostRequest("aged-reserved")
	for _, request := range []livecost.Request{request, func() livecost.Request { r := request; r.TenantID++; return r }(), func() livecost.Request { r := request; r.RoomID++; return r }()} {
		if _, err := store.ReserveLiveCost(ctx, request, policy, now.Add(-2*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := store.LiveCostSummary(ctx, scope, policy, now)
	if err != nil || summary.TotalMicros != 0 || summary.AgedUnresolvedMicros != request.EstimatedMicros || summary.AgedUnresolvedRequests != 1 {
		t.Fatal("historical report crossed tenant or room", summary, err)
	}
	if err = store.FinishLiveCost(ctx, scope, request.Key, livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptSuccess, ProviderRequestID: "late-success"}, now); err != nil {
		t.Fatal(err)
	}
	summary, err = store.LiveCostSummary(ctx, scope, policy, now)
	if err != nil || summary.TotalMicros != 0 || summary.AgedUnresolvedMicros != 0 || summary.AgedUnresolvedRequests != 0 {
		t.Fatal("late receipt did not reconcile historical unresolved", summary, err)
	}
}
func TestLiveCostSupplierInvoiceReplacesEstimateWithoutDoubleCharge(t *testing.T) {
	store, state := newCostSQLStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	policy := livecost.DefaultPolicy()
	scope := livecost.Scope{TenantID: 14, RoomID: 29}
	request := sqlCostRequest("invoice")
	if _, err := store.ReserveLiveCost(ctx, request, policy, now); err != nil {
		t.Fatal(err)
	}
	receipt := livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptSuccess, ProviderRequestID: "supplier-id"}
	state.failUpdate = true
	if err := store.FinishLiveCost(ctx, scope, request.Key, receipt, now); err == nil {
		t.Fatal("failed SQL acknowledged")
	}
	state.failUpdate = false
	summary, err := store.LiveCostSummary(ctx, scope, policy, now)
	if err != nil || summary.ReservedMicros != 40000 || summary.SettledRequests != 0 {
		t.Fatal("rollback lost reservation", summary, err)
	}
	if err = store.FinishLiveCost(ctx, scope, request.Key, receipt, now); err != nil {
		t.Fatal(err)
	}
	invoice := livecost.Invoice{TenantID: 14, RoomID: 29, Provider: "test-tts", ProviderRequestID: "supplier-id", ActualMicros: 32000, Currency: "CNY"}
	for range 2 {
		if err = store.ReconcileLiveCostInvoice(ctx, invoice, now); err != nil {
			t.Fatal(err)
		}
	}
	summary, err = store.LiveCostSummary(ctx, scope, policy, now)
	if err != nil || summary.TotalMicros != 32000 || summary.SupplierInvoiceMicros != 32000 || summary.EstimatedMicros != 0 || summary.SettledRequests != 1 {
		t.Fatal(summary, err)
	}
	invoice.ActualMicros++
	if err = store.ReconcileLiveCostInvoice(ctx, invoice, now); !errors.Is(err, livecost.ErrReceiptConflict) {
		t.Fatal("conflicting invoice overwritten", err)
	}
	invoice.TenantID = 15
	if err = store.ReconcileLiveCostInvoice(ctx, invoice, now); !errors.Is(err, livecost.ErrRequestNotFound) {
		t.Fatal("invoice crossed tenant", err)
	}
}
