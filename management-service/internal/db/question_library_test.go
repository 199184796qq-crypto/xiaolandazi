package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

type questionLibraryConnector struct{ conn *questionLibraryConn }

func (c questionLibraryConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c questionLibraryConnector) Driver() driver.Driver                        { return questionLibraryDriver{} }

type questionLibraryDriver struct{}

func (questionLibraryDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use test connector")
}

type questionLibraryConn struct {
	query string
	args  []driver.NamedValue
	rows  [][]driver.Value
}

func (*questionLibraryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*questionLibraryConn) Close() error { return nil }
func (*questionLibraryConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *questionLibraryConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.query, c.args = query, append([]driver.NamedValue(nil), args...)
	return &questionLibraryRows{rows: append([][]driver.Value(nil), c.rows...)}, nil
}

type questionLibraryRows struct{ rows [][]driver.Value }

func (*questionLibraryRows) Columns() []string {
	return strings.Fields("id tenant_id room_id plan_id question_type question answer sense_key sense_name exclude_json status updated_at")
}
func (*questionLibraryRows) Close() error { return nil }
func (r *questionLibraryRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func TestQuestionLibraryReadsLegacyNullableExcludes(t *testing.T) {
	// Production contains unanswered historical rows with SQL NULL exclusions
	// alongside answered rows with a JSON list. One legacy row must not hide all.
	for _, reader := range []string{"room", "plan", "entry"} {
		for _, raw := range []driver.Value{nil, "", "null", "[]", `["价","优惠"]`} {
			t.Run(reader+"/"+stringForNullableQuestionTest(raw), func(t *testing.T) {
				now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
				conn := &questionLibraryConn{rows: [][]driver.Value{{int64(1), int64(14), int64(29), int64(4), "fact", "能切块吗", "可以切块。", "spec", "规格", raw, "active", now}}}
				database := sql.OpenDB(questionLibraryConnector{conn})
				t.Cleanup(func() { _ = database.Close() })
				s := &Store{db: database}
				ctx := context.Background()
				var gotQuestion, gotAnswer string
				var gotExcludes []string
				switch reader {
				case "room":
					items, err := s.ListQuestionLibrary(ctx, 14, 29)
					if err != nil {
						t.Fatal(err)
					}
					if len(items) != 1 {
						t.Fatalf("items=%d", len(items))
					}
					gotQuestion, gotAnswer, gotExcludes = items[0].Question, items[0].Answer, items[0].ExcludeWords
					if !strings.Contains(conn.query, "tenant_id = ? AND room_id = ?") || conn.args[0].Value != int64(14) || conn.args[1].Value != int64(29) {
						t.Fatal("room scope lost")
					}
				case "plan":
					items, err := s.ListQuestionLibraryByPlan(ctx, 14, 4)
					if err != nil {
						t.Fatal(err)
					}
					if len(items) != 1 {
						t.Fatalf("items=%d", len(items))
					}
					gotQuestion, gotAnswer, gotExcludes = items[0].Question, items[0].Answer, items[0].ExcludeWords
					if !strings.Contains(conn.query, "tenant_id = ? AND plan_id = ?") || conn.args[0].Value != int64(14) || conn.args[1].Value != int64(4) {
						t.Fatal("shared-plan scope lost")
					}
				case "entry":
					item, err := s.questionLibraryEntry(ctx, 1)
					if err != nil {
						t.Fatal(err)
					}
					gotQuestion, gotAnswer, gotExcludes = item.Question, item.Answer, item.ExcludeWords
				}
				if gotQuestion != "能切块吗" || gotAnswer != "可以切块。" {
					t.Fatal("question or answer lost")
				}
				if raw == `["价","优惠"]` {
					if !reflect.DeepEqual(gotExcludes, []string{"价", "优惠"}) {
						t.Fatalf("exclusions=%v", gotExcludes)
					}
				} else if len(gotExcludes) != 0 {
					t.Fatalf("unexpected exclusions=%v", gotExcludes)
				}
			})
		}
	}
}

func stringForNullableQuestionTest(value driver.Value) string {
	if value == nil {
		return "SQL_NULL"
	}
	if value == "" {
		return "empty"
	}
	return value.(string)
}
