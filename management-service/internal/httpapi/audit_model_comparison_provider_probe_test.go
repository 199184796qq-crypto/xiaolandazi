package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentguard"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuditComparisonProfileInventory(t *testing.T) {
	if os.Getenv("AUDIT_COMPARISON_INVENTORY") != "1" {
		t.Skip("explicit read-only profile inventory")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	models, err := store.SpeechModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{"writer": models.DefaultID, "system": models.SystemDefaultID, "review": models.ReviewDefaultID}
	review, err := store.ResolveTextRole(context.Background(), "mainline_p0_context")
	if err != nil {
		t.Fatal(err)
	}
	roles["resolved_review"] = review
	profiles := []map[string]any{}
	for _, p := range models.Profiles {
		profiles = append(profiles, map[string]any{"id": p.ID, "name": p.Name, "model": p.Model, "enabled": p.Enabled, "protocol": p.Protocol, "has_api_key": p.HasAPIKey})
	}
	raw, err := json.MarshalIndent(map[string]any{"roles": roles, "profiles": profiles}, "", "  ")
	if err == nil {
		err = os.WriteFile(os.Getenv("AUDIT_COMPARISON_INVENTORY_OUTPUT"), raw, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

type auditCompareCase struct {
	Fixture             factTemplateFixture
	Original, Kind, Key string
}

func TestAuditModelsOnIdenticalGeneratedContent(t *testing.T) {
	if os.Getenv("AUDIT_MODEL_COMPARISON_PROBE") != "1" {
		t.Skip("explicit paid comparison")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.ResolveTextRole(ctx, "mainline_p0_context")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{"current": reviewID}
	for _, p := range models.Profiles {
		if p.Enabled && p.Model == "qwen3.8-max" {
			ids["qwen_max"] = p.ID
		}
		if p.Enabled && p.Model == "deepseek-v4-pro" {
			ids["ds4p"] = p.ID
		}
	}
	profiles := map[string]*agentgateway.SpeechModel{}
	profileInfo := map[string]any{}
	for _, label := range []string{"current", "qwen_max", "ds4p"} {
		if ids[label] == "" {
			t.Fatal("requested comparison model unavailable")
		}
		p, e := store.ResolveSpeechModel(ctx, ids[label])
		if e != nil || p == nil || !p.Enabled || p.APIKey == "" {
			t.Fatal("requested comparison connection unavailable")
		}
		profiles[label] = p
		u, _ := url.Parse(p.BaseURL)
		profileInfo[label] = map[string]any{"profile_id": p.ID, "model": p.Model, "endpoint_host": u.Hostname()}
	}
	var input struct {
		Rows []struct {
			Fixture factTemplateFixture `json:"fixture"`
			Outputs []struct {
				Mode        string                `json:"mode"`
				Temperature float64               `json:"temperature"`
				Repeat      int                   `json:"repeat"`
				Response    agentgateway.Response `json:"response"`
			} `json:"outputs"`
		} `json:"rows"`
	}
	raw, err := os.ReadFile(os.Getenv("AUDIT_MODEL_COMPARISON_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	cases := []auditCompareCase{}
	for _, row := range input.Rows {
		for _, o := range row.Outputs {
			if o.Mode == "direct" && o.Temperature == 0.7 {
				key := anchorcycle.ProductDigest(fmt.Sprintf("%s/%d/%s", anchorcycle.ProductDigest(row.Fixture), o.Repeat, o.Response.Text))
				cases = append(cases, auditCompareCase{row.Fixture, o.Response.Text, "actual_model_return", key})
			}
		}
	}
	if len(cases) != 32 {
		t.Fatal("actual generated comparison set incomplete")
	}
	controls := sentenceRephraseFixtures()
	for _, i := range []int{0, 2, 4, 5} {
		f := controls[i]
		fixture := factTemplateFixture{Product: "语境对照", Category: f.Category, Task: f.Name, Identity: f.Facts, Facts: f.Facts}
		cases = append(cases, auditCompareCase{fixture, f.Before, "declared_control", anchorcycle.ProductDigest(f)})
	}
	p0Source, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(p0Source)
	if err != nil {
		t.Fatal(err)
	}
	p0Mother, err := interactionpolicy.Read("model-content-p0-contract.md")
	if err != nil {
		t.Fatal(err)
	}
	p0 := strings.ReplaceAll(string(p0Mother), "{{p0_rules}}", policy.JSON())
	repairMother, err := interactionpolicy.Read("final-content-auto-repair.md")
	if err != nil {
		t.Fatal(err)
	}
	checkMother, err := interactionpolicy.Read("final-fact-boundary-review.md")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	call := func(profile *agentgateway.SpeechModel, stage, mother, content string) (agentgateway.Response, agentgateway.Request, error) {
		temp := 0.2
		r := agentgateway.Request{Stage: stage, Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother}, {Role: "user", Content: content}}, ResponseFormat: agentgateway.ResponseText, MaxTokens: 850, Timeout: 30 * time.Second, EnableThinking: false, Temperature: &temp}
		id, e := s.beginModelExchange(ctx, stage, profile.ID, r)
		if e != nil {
			return agentgateway.Response{}, r, e
		}
		started := time.Now()
		c, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		res, e := agentgateway.CompleteSpeechEndpoint(c, *profile, r)
		res.Provider, res.Model = "anchor:"+profile.ID, profile.Model
		e = s.finishModelExchange(ctx, id, res, res.Text, started, res.InputTokens, res.OutputTokens, e)
		return res, r, e
	}
	var mu sync.Mutex
	rows := []map[string]any{}
	archive := func() {
		mu.Lock()
		defer mu.Unlock()
		b, e := json.MarshalIndent(map[string]any{"rows": rows, "profiles": profileInfo, "source_file_sha": anchorcycle.ProductDigest(string(raw)), "actual_cases": 32, "control_cases": 4, "p0_rules_count": len(policy.Data.Rules), "p0_source_sha": derivedHash(p0Source), "p0_contract": p0, "repair_mother": string(repairMother), "check_mother": string(checkMother), "offline": true, "adopted": false, "played": false, "human_midprocess_changes": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("AUDIT_MODEL_COMPARISON_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}
	defer archive()
	slots := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, c := range cases {
		c := c
		for _, label := range []string{"current", "qwen_max", "ds4p"} {
			label := label
			wg.Add(1)
			go func() {
				defer wg.Done()
				slots <- struct{}{}
				defer func() { <-slots }()
				started := time.Now()
				profile := profiles[label]
				row := map[string]any{"case_key": c.Key, "kind": c.Kind, "fixture": c.Fixture, "original": c.Original, "role": label, "model": profile.Model}
				defer func() {
					row["pipeline_wall_ms"] = time.Since(started).Milliseconds()
					mu.Lock()
					rows = append(rows, row)
					mu.Unlock()
					archive()
					t.Logf("completed %s %s action=%v", label, c.Key[:8], row["action"])
				}()
				base := fmt.Sprintf("商品类别：%s\n当前问题：%s\n商品必要事实：%s\n本次完整确认事实：%s", c.Fixture.Category, c.Fixture.Task, c.Fixture.Identity, c.Fixture.Facts)
				content := base + "\n\n模型刚返回的原正文：\n" + c.Original
				row["identical_audit_input_sha"] = anchorcycle.ProductDigest(p0 + string(repairMother) + content)
				scope := "all"
				if c.Fixture.Category == "熟食" || c.Fixture.Category == "咖啡豆" {
					scope = "ordinary_food"
				}
				row["initial_program_scan"] = policy.Scan(c.Original, scope)
				res, request, e := call(profile, "offline_audit_model_comparison_repair", string(repairMother), content)
				row["audit_content"], row["audit_request"], row["audit_completed"] = res, request, e == nil
				if e != nil {
					row["action"] = "skip_audit_incomplete"
					t.Errorf("%s audit incomplete", label)
					return
				}
				target, e := anchorcycle.SelectSentenceRephraseTarget(c.Original, 0, len([]rune(c.Original)))
				if e != nil {
					row["action"] = "skip_empty_source"
					return
				}
				body, e := anchorcycle.ApplySentenceRephrase(c.Original, target, res.Text)
				if e != nil || textControlLeak(body) {
					row["action"] = "skip_replacement_incomplete"
					return
				}
				row["program_target"], row["assembled_text"], row["changed"] = target, body, strings.TrimSpace(body) != strings.TrimSpace(c.Original)
				scan := policy.Scan(body, scope)
				row["final_program_scan"] = scan
				if scan.Error != "" {
					row["action"] = "skip_scan_incomplete"
					return
				}
				for _, h := range scan.Hits {
					if h.ReviewMode == "hard" {
						row["action"] = "skip_hard_p0_remaining"
						return
					}
				}
				checked, checkRequest, e := call(profile, "offline_audit_model_comparison_check", string(checkMother), base+"\n\n待核对程序组装稿：\n"+body)
				row["final_check"], row["final_request"], row["check_completed"] = checked, checkRequest, e == nil
				row["model_verdict"] = factBoundaryVerdict(checked.Text)
				if e != nil {
					row["action"] = "skip_check_incomplete"
					t.Errorf("%s check incomplete", label)
					return
				}
				if row["model_verdict"] != "通过" {
					row["action"] = "skip_final_check"
					return
				}
				row["action"], row["final_text"] = "accept", body
			}()
		}
	}
	wg.Wait()
	if len(rows) != 3*len(cases) {
		t.Fatal("comparison incomplete")
	}
}
