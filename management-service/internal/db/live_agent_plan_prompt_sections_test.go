package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/planprompt"
)

type promptSectionSQL struct {
	rows    map[[2]int64][]driver.Value
	writes  int
	fail    bool
	columns map[string]bool
	alters  int
}
type promptSectionConnector struct{ s *promptSectionSQL }

func (c promptSectionConnector) Connect(context.Context) (driver.Conn, error) {
	return &promptSectionConn{c.s}, nil
}
func (c promptSectionConnector) Driver() driver.Driver { return promptSectionDriver{} }

type promptSectionDriver struct{}

func (promptSectionDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector only")
}

type promptSectionConn struct{ s *promptSectionSQL }

func (*promptSectionConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no prepare") }
func (*promptSectionConn) Close() error                        { return nil }
func (*promptSectionConn) Begin() (driver.Tx, error) {
	return nil, errors.New("single statement expected")
}
func (c *promptSectionConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "ALTER TABLE live_agent_plan_prompts ADD COLUMN ") {
		if c.s.columns == nil {
			c.s.columns = map[string]bool{}
		}
		name := strings.Fields(q)[5]
		if c.s.columns[name] {
			return nil, errors.New("duplicate migration")
		}
		c.s.columns[name] = true
		c.s.alters++
		return driver.RowsAffected(0), nil
	}
	if c.s.fail {
		return nil, errors.New("injected write failure")
	}
	if !strings.Contains(q, "sections_json=VALUES(sections_json)") || !strings.Contains(q, "speech_prompt_md=VALUES(speech_prompt_md)") || len(args) != 6 {
		return nil, errors.New("all assets must be saved atomically")
	}
	key := [2]int64{args[0].Value.(int64), args[1].Value.(int64)}
	c.s.rows[key] = []driver.Value{args[2].Value, args[3].Value, args[4].Value, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)}
	c.s.writes++
	return driver.RowsAffected(1), nil
}
func (c *promptSectionConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "information_schema.columns") && len(args) == 1 {
		count := int64(0)
		if c.s.columns[args[0].Value.(string)] {
			count = 1
		}
		return &promptSectionRows{columns: []string{"count"}, data: [][]driver.Value{{count}}}, nil
	}
	if !strings.Contains(q, "tenant_id=? AND plan_id=?") || len(args) != 2 {
		return nil, errors.New("missing tenant/plan scope")
	}
	values, ok := c.s.rows[[2]int64{args[0].Value.(int64), args[1].Value.(int64)}]
	columns := []string{"prompt_md", "sections_json", "speech_prompt_md", "updated_at"}
	if !strings.Contains(q, "sections_json") {
		columns = []string{"prompt_md", "updated_at"}
		if ok {
			values = []driver.Value{values[0], values[3]}
		}
	}
	var data [][]driver.Value
	if ok {
		data = [][]driver.Value{values}
	}
	return &promptSectionRows{columns: columns, data: data}, nil
}

type promptSectionRows struct {
	columns []string
	data    [][]driver.Value
}

func (r *promptSectionRows) Columns() []string { return r.columns }
func (*promptSectionRows) Close() error        { return nil }
func (r *promptSectionRows) Next(dest []driver.Value) error {
	if len(r.data) == 0 {
		return io.EOF
	}
	copy(dest, r.data[0])
	r.data = r.data[1:]
	return nil
}

func TestPlanPromptSectionsAtomicStorageAndRuntimeRead(t *testing.T) {
	state := &promptSectionSQL{rows: map[[2]int64][]driver.Value{}}
	database := sql.OpenDB(promptSectionConnector{state})
	defer database.Close()
	s := &Store{db: database}
	sections := planprompt.Sections{PersonaMD: "主播人设内容", FactsMD: "本方案事实", SpeechRequirementsMD: "主线专用内容", InteractionRequirementsMD: "互动专用内容"}
	doc, err := s.SaveLiveAgentPlanPromptSections(context.Background(), 14, 4, 28, sections)
	if err != nil || state.writes != 1 || doc.Sections != sections || doc.SpeechPromptMD != sections.Speech() || doc.InteractionPromptMD != sections.Interaction() {
		t.Fatal(doc, err, state.writes)
	}
	runtime, _, err := s.GetLiveAgentPlanPrompt(context.Background(), 14, 4)
	if err != nil || runtime != sections.Interaction() || strings.Contains(runtime, "主线专用内容") {
		t.Fatal(runtime, err)
	}
	other, err := s.GetLiveAgentPlanPromptDocument(context.Background(), 15, 4)
	if err != nil || !other.Sections.Empty() || other.PromptMD != "" {
		t.Fatal("tenant leakage", other, err)
	}
	state.fail = true
	if _, err = s.SaveLiveAgentPlanPromptSections(context.Background(), 14, 4, 28, planprompt.Sections{FactsMD: "不能落库"}); err == nil {
		t.Fatal("failure accepted")
	}
	state.fail = false
	after, _ := s.GetLiveAgentPlanPromptDocument(context.Background(), 14, 4)
	if after.Sections != sections {
		t.Fatal("failed save corrupted previous document", after)
	}
	state.rows[[2]int64{14, 7}] = []driver.Value{"【身份设定】\n旧人设\n【事实依据】\n旧事实\n【话术规则】\n旧主线\n【互动约束】\n旧回答", nil, nil, time.Now()}
	legacy, err := s.GetLiveAgentPlanPromptDocument(context.Background(), 14, 7)
	if err != nil || legacy.Sections.FactsMD != "旧事实" || strings.Contains(legacy.SpeechPromptMD, "旧回答") || state.writes != 1 {
		t.Fatal(legacy, err)
	}
	if _, err = s.SaveLiveAgentPlanPromptSections(context.Background(), 14, 4, 28, planprompt.Sections{FactsMD: strings.Repeat("字", 20000)}); err == nil {
		t.Fatal("oversized compiled input saved")
	}
	for i := 0; i < 2; i++ {
		if err := s.ensureLiveAgentPlanPromptColumns(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if state.alters != 2 || state.writes != 1 {
		t.Fatal("migration should be additive and idempotent", state.alters, state.writes)
	}
	state.rows[[2]int64{14, 8}] = []driver.Value{"原文", "invalid-json", nil, time.Now()}
	if _, err := s.GetLiveAgentPlanPromptDocument(context.Background(), 14, 8); err == nil {
		t.Fatal("corrupt sections silently accepted")
	}
}
