// Explicitly authorized text-only probe of the real 1..10-person batch contract.
// All database reads use a read-only transaction. No queue, TTS or ledger writes.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/policy"
)

type probeCase struct {
	Name  string                      `json:"name"`
	Items []interactionplan.BatchItem `json:"items"`
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }

func main() {
	input := flag.String("input", "", "authorized text-only cases")
	output := flag.String("output", "", "isolated local evidence directory")
	tenant := flag.Int64("tenant", 0, "explicit tenant")
	room := flag.Int64("room", 0, "explicit room")
	flag.Parse()
	if *input == "" || *output == "" || *tenant <= 0 || *room <= 0 {
		fail("explicit probe arguments required")
	}
	raw, err := os.ReadFile(*input)
	var cases []probeCase
	if err != nil || json.Unmarshal(raw, &cases) != nil || len(cases) != 2 || len(cases[0].Items) != 1 || len(cases[1].Items) != 10 {
		fail("exactly one 1-person and one 10-person case required")
	}
	for _, c := range cases {
		if interactionplan.ValidateBatch(c.Items) != nil {
			fail("invalid probe cases")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	mc := mysql.NewConfig()
	mc.User, mc.Passwd, mc.DBName = os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME")
	if mc.User == "" || mc.DBName == "" {
		fail("server read-only configuration unavailable")
	}
	host, port := os.Getenv("DB_HOST"), os.Getenv("DB_PORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "3306"
	}
	mc.Net, mc.Addr, mc.ParseTime, mc.Loc = "tcp", net.JoinHostPort(host, port), true, time.UTC
	mc.Timeout, mc.ReadTimeout, mc.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	mc.Params = map[string]string{"charset": "utf8mb4"}
	database, err := sql.Open("mysql", mc.FormatDSN())
	if err != nil {
		fail("probe database unavailable")
	}
	defer database.Close()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		fail("read-only transaction unavailable")
	}
	defer tx.Rollback()
	var configRaw, keysRaw []byte
	if tx.QueryRowContext(ctx, `SELECT config_json,keys_json FROM mgmt_anchor_model_settings WHERE id=1`).Scan(&configRaw, &keysRaw) != nil {
		fail("operator model configuration unavailable")
	}
	var models agentgateway.SpeechModels
	var keys map[string][]byte
	if json.Unmarshal(configRaw, &models) != nil || json.Unmarshal(keysRaw, &keys) != nil {
		fail("operator model configuration invalid")
	}
	id := models.ReviewDefaultID
	if id == "" {
		id = models.DefaultID
	}
	var profile agentgateway.SpeechModel
	for _, p := range models.Profiles {
		if p.ID == id {
			profile = p
		}
	}
	profile.APIKey, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), id, keys[id])
	if err != nil || !profile.Enabled || profile.APIKey == "" || agentgateway.ValidateSpeechModel(profile) != nil {
		fail("operator interaction credential unavailable")
	}
	var planID, versionID int64
	var name, prompt string
	var updated time.Time
	err = tx.QueryRowContext(ctx, `SELECT p.id,p.name,pr.prompt_md,pr.updated_at FROM live_agent_room_plan_selections s JOIN live_agent_plans p ON p.tenant_id=s.tenant_id AND p.id=s.plan_id AND p.status='active' JOIN live_agent_plan_prompts pr ON pr.tenant_id=p.tenant_id AND pr.plan_id=p.id WHERE s.tenant_id=? AND s.room_id=?`, *tenant, *room).Scan(&planID, &name, &prompt, &updated)
	if err != nil {
		fail("selected saved plan prompt unavailable")
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM live_agent_plan_versions WHERE tenant_id=? AND plan_id=? AND lifecycle_status IN ('draft','published','superseded')`, *tenant, planID).Scan(&versionID)
	if err != nil {
		fail("selected plan version unavailable")
	}
	if tx.Rollback() != nil {
		fail("read-only transaction close failed")
	}
	p0, err := policy.LoadGeneration()
	if err != nil {
		fail("effective P0 unavailable")
	}
	md, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if p := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_P0_POLICY_PATH")); p != "" {
		md, err = os.ReadFile(p)
	}
	if err != nil {
		fail("P0 source unavailable")
	}
	snapshot, err := interactionplan.NewSnapshot(interactionplan.Scope{Tenant: *tenant, Room: *room, Plan: planID, Version: versionID}, prompt, p0.PromptText, string(md))
	if err != nil {
		fail("saved plan snapshot invalid")
	}
	if os.MkdirAll(*output, 0700) != nil {
		fail("probe evidence directory unavailable")
	}
	metadata := map[string]any{"tenant_id": *tenant, "room_id": *room, "plan_id": planID, "plan_name": name, "version_id": versionID, "plan_prompt_updated_at": updated, "plan_prompt_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))), "p0_sha256": fmt.Sprintf("%x", sha256.Sum256(md)), "model": profile.Model, "profile_name": profile.Name, "protocol": profile.Protocol, "profile_max_tokens": profile.MaxTokens, "profile_timeout_ms": profile.TimeoutMS, "text_only": true, "database_read_only": true, "test_api_budget": 2, "snapshot": snapshot}
	write(*output, "context.json", metadata)
	for _, c := range cases {
		calls := 0
		row := map[string]any{"name": c.Name, "inputs": c.Items, "model": profile.Model, "profile_name": profile.Name, "text_only": true, "started_at_utc": time.Now().UTC()}
		// The deployed Core adapter has a 28-second HTTP budget; preserve it here.
		callCtx, stop := context.WithTimeout(context.Background(), 28*time.Second)
		start := time.Now()
		generated, genErr := interactionplan.GenerateBatch(callCtx, snapshot, c.Items, func(ctx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
			if len(c.Items) > 1 && profile.MaxTokens < req.MaxTokens {
				return agentgateway.Response{}, fmt.Errorf("configured output cap %d below required %d", profile.MaxTokens, req.MaxTokens)
			}
			if calls != 0 {
				return agentgateway.Response{}, errors.New("probe refuses a second provider call")
			}
			req.Model = profile.Model
			req.MaxTokens = min(req.MaxTokens, profile.MaxTokens)
			req.Timeout = time.Duration(profile.TimeoutMS) * time.Millisecond
			row["request"] = req
			raw, _ := json.Marshal(req)
			row["request_sha256"] = fmt.Sprintf("%x", sha256.Sum256(raw))
			calls++
			response, e := agentgateway.CompleteSpeechEndpoint(ctx, profile, req)
			row["response"] = response
			if e != nil {
				row["provider_error"] = safeError(e, profile.APIKey, profile.BaseURL)
			}
			return response, e
		})
		stop()
		row["latency_ms"] = time.Since(start).Milliseconds()
		row["provider_calls"] = calls
		row["completed_at_utc"] = time.Now().UTC()
		row["issues"] = generated.Issues
		parsed := []map[string]any{}
		valid := 0
		for _, item := range generated.Items {
			r := map[string]any{"item_id": item.ID, "intent": item.Intent, "body": item.Body, "valid": item.Err == nil}
			if item.Err != nil {
				r["error"] = item.Err.Error()
			} else {
				valid++
			}
			parsed = append(parsed, r)
		}
		row["parsed"] = parsed
		row["valid_count"] = valid
		row["model_call_succeeded"] = genErr == nil
		if genErr != nil {
			row["error"] = safeError(genErr, profile.APIKey, profile.BaseURL)
		}
		write(*output, c.Name+".json", row)
		fmt.Printf("case=%s calls=%d model_success=%v valid=%d/%d latency_ms=%v\n", c.Name, calls, genErr == nil, valid, len(c.Items), row["latency_ms"])
	}
}

func safeError(err error, key, base string) string {
	s := err.Error()
	if key != "" {
		s = strings.ReplaceAll(s, key, "[credential omitted]")
	}
	if base != "" {
		s = strings.ReplaceAll(s, base, "[endpoint omitted]")
	}
	return s
}
func write(dir, name string, value any) {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(dir, name), b, 0600) != nil {
		fail("probe evidence save failed")
	}
}
