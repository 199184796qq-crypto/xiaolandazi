package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/planprompt"
)

type promptSectionSQL struct {
	rows     map[[2]int64][]driver.Value
	writes   int
	fail     bool
	columns  map[string]bool
	alters   int
	rewrites map[[2]int64]string
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
	if !strings.Contains(q, "sections_json=VALUES(sections_json)") || !strings.Contains(q, "speech_prompt_md=VALUES(speech_prompt_md)") || (len(args) != 6 && len(args) != 7) {
		return nil, errors.New("all assets must be saved atomically")
	}
	key := [2]int64{args[0].Value.(int64), args[1].Value.(int64)}
	if len(args) == 7 {
		if !strings.Contains(q, "rewrite_prompt_md=VALUES(rewrite_prompt_md)") {
			return nil, errors.New("rewrite must be saved atomically")
		}
		if c.s.rewrites == nil {
			c.s.rewrites = map[[2]int64]string{}
		}
		c.s.rewrites[key] = args[5].Value.(string)
	}
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
	if strings.Contains(q, "SELECT rewrite_prompt_md") {
		return &promptSectionRows{columns: []string{"rewrite_prompt_md"}, data: [][]driver.Value{{c.s.rewrites[[2]int64{args[0].Value.(int64), args[1].Value.(int64)}]}}}, nil
	}
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

func TestPlanPromptRewriteEditorAtomicRoundTrip(t *testing.T) {
	state := &promptSectionSQL{rows: map[[2]int64][]driver.Value{}}
	database := sql.OpenDB(promptSectionConnector{state})
	defer database.Close()
	s := &Store{db: database}
	ctx := context.Background()
	sections := planprompt.Sections{PersonaMD: "原始人设", FactsMD: "原始事实", SpeechRequirementsMD: "主线要求", InteractionRequirementsMD: "回答要求"}
	rewrite := "  自定义话术规划\n\n{{样本话术}}\n{{已有开场}}\n〔提示〕此行也原样保存\n  "
	doc, err := s.SaveLiveAgentPlanPromptSectionsWithRewrite(ctx, 14, 4, 28, sections, rewrite)
	if err != nil || doc.RewritePromptMD != rewrite || doc.Sections != sections || state.writes != 1 {
		t.Fatalf("rewrite and originals not saved together: %#v %v writes=%d", doc, err, state.writes)
	}
	if strings.Contains(doc.InteractionPromptMD, "自定义话术规划") || strings.Contains(doc.SpeechPromptMD, "自定义话术规划") {
		t.Fatal("rewrite must remain separate from the other two prompts")
	}
	other, err := s.GetLiveAgentPlanPromptDocument(ctx, 15, 4)
	if err != nil || other.RewritePromptMD != "" {
		t.Fatal("tenant leakage", other, err)
	}
	other, err = s.GetLiveAgentPlanPromptDocument(ctx, 14, 5)
	if err != nil || other.RewritePromptMD != "" {
		t.Fatal("plan leakage", other, err)
	}
	state.fail = true
	if _, err = s.SaveLiveAgentPlanPromptSectionsWithRewrite(ctx, 14, 4, 28, planprompt.Sections{FactsMD: "失败事实"}, "失败规划"); err == nil {
		t.Fatal("failure accepted")
	}
	state.fail = false
	doc, err = s.GetLiveAgentPlanPromptDocument(ctx, 14, 4)
	if err != nil || doc.RewritePromptMD != rewrite || doc.Sections != sections || state.writes != 1 {
		t.Fatal("failed save corrupted document", doc, err)
	}
	if _, err = s.SaveLiveAgentPlanPromptSectionsWithRewrite(ctx, 14, 4, 28, sections, strings.Repeat("字", 20001)); err == nil || state.writes != 1 {
		t.Fatal("oversized rewrite saved", err)
	}
	if _, err = s.SaveLiveAgentPlanPromptSections(ctx, 14, 4, 28, sections); err != nil {
		t.Fatal(err)
	}
	doc, _ = s.GetLiveAgentPlanPromptDocument(ctx, 14, 4)
	if doc.RewritePromptMD != rewrite {
		t.Fatal("omitted field cleared rewrite")
	}
	doc, err = s.SaveLiveAgentPlanPromptSectionsWithRewrite(ctx, 14, 4, 28, sections, "")
	if err != nil || doc.RewritePromptMD != "" {
		t.Fatal("explicit clearing not preserved", doc, err)
	}
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
	if err != nil || !legacy.Sections.Empty() || legacy.LegacyPromptMD != state.rows[[2]int64{14, 7}][0] || !legacy.LegacyUnstructured || legacy.SpeechPromptMD != "" || state.writes != 1 {
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
	if state.alters != 3 || state.writes != 1 {
		t.Fatal("migration should be additive and idempotent", state.alters, state.writes)
	}
	state.rows[[2]int64{14, 8}] = []driver.Value{"原文", "invalid-json", nil, time.Now()}
	if _, err := s.GetLiveAgentPlanPromptDocument(context.Background(), 14, 8); err == nil {
		t.Fatal("corrupt sections silently accepted")
	}
}

func TestPlanPromptRawSectionsRoundTripWithoutRepartition(t *testing.T) {
	state := &promptSectionSQL{rows: map[[2]int64][]driver.Value{}}
	database := sql.OpenDB(promptSectionConnector{state})
	defer database.Close()
	s := &Store{db: database}
	original := planprompt.Sections{
		PersonaMD:                 "  我的原始人设\n\n【事实依据】\n这个标题仍属于人设页\n  ",
		FactsMD:                   "\n〔提示〕我在此页保留的原始说明\n  京东物流  \n",
		SpeechRequirementsMD:      "\n\n",
		InteractionRequirementsMD: "  只回答被问内容\n【主播人设】\n这是互动页原文，不能被挪走\n",
	}
	compiled := original.Normalize()
	doc, err := s.SaveLiveAgentPlanPromptSections(context.Background(), 14, 4, 28, original)
	if err != nil || doc.Sections != original {
		t.Fatalf("raw originals changed: got=%#v err=%v", doc.Sections, err)
	}
	if doc.InteractionPromptMD != compiled.Interaction() || doc.SpeechPromptMD != compiled.Speech() {
		t.Fatal("compiled assets must use a separate cleaned copy", doc)
	}
	state.rows[[2]int64{14, 5}] = []driver.Value{"其它方案", nil, nil, time.Now()}
	if _, err = s.GetLiveAgentPlanPromptDocument(context.Background(), 14, 5); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.GetLiveAgentPlanPromptDocument(context.Background(), 14, 4)
	if err != nil || reloaded.Sections != original || state.writes != 1 {
		t.Fatal("switching away and back must restore only original fields", reloaded, err)
	}
	if _, err = s.SaveLiveAgentPlanPromptSections(context.Background(), 14, 4, 28, planprompt.Sections{}); err != nil {
		t.Fatal(err)
	}
	empty, err := s.GetLiveAgentPlanPromptDocument(context.Background(), 14, 4)
	if err != nil || !empty.Sections.Empty() || empty.PromptMD != "" || empty.SpeechPromptMD != "" {
		t.Fatal("explicitly clearing all four pages must persist", empty, err)
	}
}

func TestPlanRewritePlanningSeparateAtomicStorage(t *testing.T) {
	state := &promptSectionSQL{rows: map[[2]int64][]driver.Value{}}
	database := sql.OpenDB(promptSectionConnector{state})
	defer database.Close()
	store := &Store{db: database}
	sections := planprompt.Sections{PersonaMD: "原始人设", FactsMD: "八十七元六角", RedLinesMD: "不编造", SpeechRequirementsMD: "主线要求", InteractionRequirementsMD: "回答要求"}
	planning := "  第一步：抽取直播流程骨架\r\n【事实依据】此行属于规则，不能拆出去\n{{已有开场}}\n{{开场方式}}\n  "
	doc, err := store.SaveLiveAgentPlanPromptPlanning(t.Context(), 14, 4, 28, sections, planning)
	if err != nil || state.writes != 1 || doc.RewritePlanningMD == nil || *doc.RewritePlanningMD != planning || doc.Sections != sections {
		t.Fatalf("raw planning failed round trip: %#v %v", doc, err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(state.rows[[2]int64{14, 4}][1].(string)), &raw); err != nil || raw["rewrite_planning_md"] != planning {
		t.Fatal("planning was not separately persisted", raw, err)
	}
	if doc.RewritePromptMD != planprompt.CompileRewritePlanning(sections, planning) || !strings.Contains(doc.RewritePromptMD, "【样本话术】\n{{样本话术}}") {
		t.Fatal("runtime envelope incomplete", doc.RewritePromptMD)
	}
	if strings.Contains(doc.SpeechPromptMD, "第一步") || strings.Contains(doc.InteractionPromptMD, "第一步") {
		t.Fatal("planning leaked to other prompts")
	}
	for _, key := range [][2]int64{{15, 4}, {14, 5}} {
		other, err := store.GetLiveAgentPlanPromptDocument(t.Context(), key[0], key[1])
		if err != nil || other.RewritePlanningMD != nil || other.RewritePromptMD != "" {
			t.Fatal("scope leakage", other, err)
		}
	}
	state.fail = true
	changed := sections
	changed.FactsMD = "失败事实"
	if _, err := store.SaveLiveAgentPlanPromptPlanning(t.Context(), 14, 4, 28, changed, "失败规则"); err == nil {
		t.Fatal("failed write accepted")
	}
	state.fail = false
	reloaded, err := store.GetLiveAgentPlanPromptDocument(t.Context(), 14, 4)
	if err != nil || reloaded.RewritePromptMD != doc.RewritePromptMD || *reloaded.RewritePlanningMD != planning || reloaded.Sections != sections || state.writes != 1 {
		t.Fatal("partial save", reloaded, err)
	}
	for _, oversized := range []string{strings.Repeat("字", 20001), strings.Repeat("字", 19980)} {
		if _, err := store.SaveLiveAgentPlanPromptPlanning(t.Context(), 14, 4, 28, sections, oversized); err == nil || state.writes != 1 {
			t.Fatal("raw or combined limit ignored", err)
		}
	}
	changed.FactsMD = "九十二元六角"
	doc, err = store.SaveLiveAgentPlanPromptPlanning(t.Context(), 14, 4, 28, changed, planning)
	if err != nil || *doc.RewritePlanningMD != planning || !strings.Contains(doc.RewritePromptMD, "九十二元六角") || strings.Contains(doc.RewritePromptMD, "八十七元六角") {
		t.Fatal("latest facts not composed", doc, err)
	}
	doc, err = store.SaveLiveAgentPlanPromptPlanning(t.Context(), 14, 4, 28, changed, "")
	if err != nil || doc.RewritePlanningMD == nil || *doc.RewritePlanningMD != "" || !strings.Contains(doc.RewritePromptMD, "{{样本话术}}") {
		t.Fatal("explicit empty rules lost sample slot", doc, err)
	}
}

func TestPlanRewriteLegacyPlanningReadDoesNotWrite(t *testing.T) {
	state := &promptSectionSQL{rows: map[[2]int64][]driver.Value{}, rewrites: map[[2]int64]string{}}
	database := sql.OpenDB(promptSectionConnector{state})
	defer database.Close()
	store := &Store{db: database}
	sections := planprompt.Sections{PersonaMD: "人设", FactsMD: "原事实"}
	raw, _ := json.Marshal(sections)
	state.rows[[2]int64{14, 4}] = []driver.Value{sections.Interaction(), string(raw), sections.Speech(), time.Now()}
	planning := "第一步：结构规则\n{{已有开场}}"
	for _, legacy := range []string{planprompt.CompileRewritePlanning(sections, planning), strings.Replace(planprompt.CompileRewritePlanning(sections, planning), "{{样本话术}}", "", 1), planning} {
		state.rewrites[[2]int64{14, 4}] = legacy
		doc, err := store.GetLiveAgentPlanPromptDocument(t.Context(), 14, 4)
		if err != nil || doc.RewritePlanningMD == nil || *doc.RewritePlanningMD != planning || doc.RewritePromptMD != legacy || state.writes != 0 {
			t.Fatal("legacy read modified assets", doc, err)
		}
	}
}
