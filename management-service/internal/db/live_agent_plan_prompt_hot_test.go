package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/planprompt"
)

type hotPromptSQL struct {
	*promptSectionSQL
	commits, rollbacks int
	failWrite          bool
}
type hotPromptConnector struct{ state *hotPromptSQL }

func (c hotPromptConnector) Connect(context.Context) (driver.Conn, error) {
	return &hotPromptConn{promptSectionConn: &promptSectionConn{c.state.promptSectionSQL}, state: c.state}, nil
}
func (hotPromptConnector) Driver() driver.Driver { return promptSectionDriver{} }

type hotPromptConn struct {
	*promptSectionConn
	state   *hotPromptSQL
	pending []driver.NamedValue
}

func (c *hotPromptConn) Begin() (driver.Tx, error) { return c, nil }
func (c *hotPromptConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c, nil
}
func (c *hotPromptConn) Commit() error {
	if a := c.pending; a != nil {
		key := [2]int64{a[5].Value.(int64), a[6].Value.(int64)}
		c.state.rows[key] = []driver.Value{a[0].Value, a[1].Value, a[2].Value, time.Now()}
		c.state.rewrites[key] = a[3].Value.(string)
		c.state.writes++
	}
	c.pending = nil
	c.state.commits++
	return nil
}
func (c *hotPromptConn) Rollback() error {
	c.pending = nil
	c.state.rollbacks++
	return nil
}
func (c *hotPromptConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FOR UPDATE") {
		if len(args) != 2 {
			return nil, errors.New("lock lost tenant/plan")
		}
		key := [2]int64{args[0].Value.(int64), args[1].Value.(int64)}
		value, ok := c.state.rows[key]
		if strings.Contains(q, "FROM live_agent_plans ") {
			rows := &promptSectionRows{columns: []string{"id"}}
			if ok {
				rows.data = [][]driver.Value{{key[1]}}
			}
			return rows, nil
		}
		rows := &promptSectionRows{columns: []string{"prompt_md", "sections_json", "rewrite_prompt_md"}}
		if ok {
			rows.data = [][]driver.Value{{value[0], value[1], c.state.rewrites[key]}}
		}
		return rows, nil
	}
	if strings.Contains(q, "live_runtime_sessions") {
		return nil, errors.New("three-field hot update must not use the full-save guard")
	}
	return c.promptSectionConn.QueryContext(ctx, q, args)
}
func (c *hotPromptConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.state.failWrite {
		return nil, errors.New("injected atomic update failure")
	}
	if !strings.HasPrefix(q, "UPDATE live_agent_plan_prompts SET ") || !strings.HasSuffix(q, "WHERE tenant_id=? AND plan_id=?") || len(args) != 7 {
		return nil, errors.New("unexpected non-prompt write")
	}
	c.pending = append([]driver.NamedValue(nil), args...)
	return driver.RowsAffected(1), nil
}

func TestRuntimePromptSaveAtomicRevisionAndProtectedPages(t *testing.T) {
	ctx := context.Background()
	sections := planprompt.Sections{PersonaMD: "本店主播", FactsMD: "八十七元", RedLinesMD: "不虚构", SpeechRequirementsMD: "旧主线要求", InteractionRequirementsMD: "旧互动要求"}
	raw, _ := json.Marshal(map[string]any{"persona_md": sections.PersonaMD, "facts_md": sections.FactsMD, "red_lines_md": sections.RedLinesMD, "speech_requirements_md": sections.SpeechRequirementsMD, "interaction_requirements_md": sections.InteractionRequirementsMD, "rewrite_planning_md": "旧规划", "future_metadata": "必须保留"})
	state := &hotPromptSQL{promptSectionSQL: &promptSectionSQL{rows: map[[2]int64][]driver.Value{{14, 4}: {sections.Interaction(), string(raw), sections.Speech(), time.Now()}}, rewrites: map[[2]int64]string{{14, 4}: planprompt.CompileRewritePlanning(sections, "旧规划")}}}
	database := sql.OpenDB(hotPromptConnector{state})
	defer database.Close()
	store := &Store{db: database}
	before, err := store.GetLiveAgentPlanPromptDocument(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	input := RuntimePromptUpdate{ExpectedRevision: before.Revision, SpeechRequirementsMD: "新主线要求", InteractionRequirementsMD: "新互动要求", RewritePlanningMD: "新规划"}
	after, err := store.SaveLiveAgentPlanRuntimePrompts(ctx, 14, 4, 8, input)
	if err != nil || state.commits != 1 || state.writes != 1 {
		t.Fatal(after, err, state)
	}
	if after.Sections.PersonaMD != sections.PersonaMD || after.Sections.FactsMD != sections.FactsMD || after.Sections.RedLinesMD != sections.RedLinesMD || !strings.Contains(state.rows[[2]int64{14, 4}][1].(string), "future_metadata") {
		t.Fatal("protected pages or metadata changed", after)
	}
	if after.Revision == before.Revision || !after.RuntimeHotUpdate || !strings.Contains(after.SpeechPromptMD, "新主线要求") || !strings.Contains(after.InteractionPromptMD, "新互动要求") || !strings.Contains(after.RewritePromptMD, "新规划") {
		t.Fatal("three compiled uses not updated together", after)
	}
	runtime, err := store.GetLiveAgentPlanRuntimePrompts(ctx, 14, 4)
	if err != nil || !runtime.Enabled || !runtime.Structured {
		t.Fatal("committed runtime settings were not readable", runtime, err)
	}
	if prompt, ok := runtime.RewriteFor(before.RewritePromptMD); !ok || prompt != after.RewritePromptMD {
		t.Fatal("runtime rewrite did not adopt committed planning", prompt, ok)
	}
	if _, err = store.SaveLiveAgentPlanRuntimePrompts(ctx, 14, 4, 8, input); !errors.Is(err, ErrPlanPromptChanged) || state.writes != 1 {
		t.Fatal("stale revision overwrote current prompts", err)
	}
	if _, err = store.SaveLiveAgentPlanRuntimePrompts(ctx, 15, 4, 8, input); !errors.Is(err, ErrLiveAgentPlanNotFound) || state.writes != 1 {
		t.Fatal("tenant scope escaped", err)
	}
	oversized := input
	oversized.ExpectedRevision = after.Revision
	oversized.InteractionRequirementsMD = strings.Repeat("字", 20000)
	if _, err = store.SaveLiveAgentPlanRuntimePrompts(ctx, 14, 4, 8, oversized); !errors.Is(err, ErrRuntimePromptInvalid) || state.writes != 1 {
		t.Fatal("oversized compiled prompt changed storage", err)
	}
	input.ExpectedRevision = after.Revision
	input.RewritePlanningMD = ""
	input.SpeechRequirementsMD = ""
	input.InteractionRequirementsMD = ""
	state.failWrite = true
	if _, err = store.SaveLiveAgentPlanRuntimePrompts(ctx, 14, 4, 8, input); err == nil || state.writes != 1 {
		t.Fatal("failed update committed", err)
	}
	state.failWrite = false
	cleared, err := store.SaveLiveAgentPlanRuntimePrompts(ctx, 14, 4, 8, input)
	if err != nil || cleared.RewritePlanningMD == nil || *cleared.RewritePlanningMD != "" || cleared.Sections.SpeechRequirementsMD != "" || cleared.Sections.InteractionRequirementsMD != "" {
		t.Fatal("explicit empty fields were not saved", cleared, err)
	}
}
