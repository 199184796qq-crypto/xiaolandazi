// Offline paid first-draft experiment. Reads model configuration only; no plan,
// runtime, queue, TTS, or model-exchange writes. Raw local artifacts are retained.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentguard"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type fixture struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Product     string `json:"product"`
	Task        string `json:"task"`
	Facts       string `json:"facts"`
	Conditions  string `json:"conditions"`
	Constraints string `json:"constraints"`
	Direction   string `json:"direction"`
	Style       string `json:"style"`
}

type experiment struct {
	mu       sync.Mutex
	Rows     []map[string]any `json:"rows"`
	Metadata map[string]any   `json:"metadata"`
	path     string
	slots    chan struct{}
}

func (e *experiment) save() error {
	b, err := json.MarshalIndent(map[string]any{"rows": e.Rows, "metadata": e.Metadata}, "", "  ")
	if err != nil {
		return err
	}
	temp := e.path + ".tmp"
	if err = os.WriteFile(temp, b, 0600); err != nil {
		return err
	}
	return os.Rename(temp, e.path)
}
func (e *experiment) append(row map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Rows = append(e.Rows, row)
	if err := e.save(); err != nil {
		panic("archive unavailable")
	}
	fmt.Printf("completed case=%s layer=%v repeat=%v\n", row["name"], row["layer"], row["repeat"])
}
func hash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func load(name string) string {
	b, e := interactionpolicy.Read(name)
	if e != nil {
		panic("prompt unavailable: " + name)
	}
	return string(b)
}
func (e *experiment) call(ctx context.Context, p agentgateway.SpeechModel, stage string, messages []agentgateway.Message, temp float64) map[string]any {
	e.slots <- struct{}{}
	defer func() { <-e.slots }()
	req := agentgateway.Request{Stage: stage, Model: p.Model, Messages: messages, ResponseFormat: agentgateway.ResponseText, MaxTokens: 550, EnableThinking: false, Temperature: &temp, Timeout: 35 * time.Second}
	prepared, err := agentgateway.PrepareTextRequest(p, req)
	if err != nil || len(prepared.Messages) != len(messages) {
		panic("unexpected production prompt stack")
	}
	start := time.Now()
	response, err := agentgateway.CompleteSpeechEndpoint(ctx, p, req)
	out := map[string]any{"request": req, "response": response, "completed": err == nil, "wall_ms": time.Since(start).Milliseconds()}
	if err != nil {
		out["error"] = err.Error()
	}
	return out
}
func verdict(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimLeft(s, "#*【[ ")
	if strings.HasPrefix(s, "通过") {
		return "pass"
	}
	if strings.HasPrefix(s, "有问题") {
		return "problem"
	}
	if strings.HasPrefix(s, "不确定") {
		return "uncertain"
	}
	return "undetermined"
}

func main() {
	if os.Getenv("FIRST_DRAFT_GRADUAL_PROBE") != "1" {
		panic("explicit offline probe required")
	}
	raw, err := os.ReadFile(os.Getenv("FIRST_DRAFT_GRADUAL_INPUT"))
	if err != nil {
		panic("input unavailable")
	}
	var fixtures []fixture
	if json.Unmarshal(raw, &fixtures) != nil || len(fixtures) == 0 || len(fixtures) > 12 {
		panic("invalid fixtures")
	}
	output := os.Getenv("FIRST_DRAFT_GRADUAL_OUTPUT")
	if output == "" {
		panic("output required")
	}
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		panic("archive directory unavailable")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		panic("model configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		panic("model inventory unavailable")
	}
	var profile *agentgateway.SpeechModel
	for _, p := range models.Profiles {
		if p.Enabled && p.Model == "qwen3.7-flash" {
			profile, err = store.ResolveSpeechModel(ctx, p.ID)
			break
		}
	}
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		panic("configured qwen3.7-flash unavailable")
	}
	p := *profile
	base := load("first-draft-gradual-base-probe.md")
	boundary := load("first-draft-gradual-boundary-probe.md")
	task := load("first-draft-gradual-task-probe.md")
	recent := load("first-draft-gradual-recent-probe.md")
	review := load("first-draft-gradual-review-probe.md")
	effective, err := policy.LoadGeneration()
	if err != nil {
		panic("full P0 unavailable")
	}
	p0 := effective.PromptText
	if !strings.Contains(p0, "完整有效P0") {
		panic("full P0 not bound")
	}
	p0Source := load("compliance/L1/p0-hard-block.md")
	scanner, err := contentguard.Compile(p0Source)
	if err != nil {
		panic("P0 compiler unavailable")
	}
	e := &experiment{path: output, slots: make(chan struct{}, 3), Metadata: map[string]any{"started_at": time.Now().UTC().Format(time.RFC3339Nano), "model": p.Model, "profile_id": p.ID, "fixtures": fixtures, "fixture_sha": hash(string(raw)), "p0": p0, "p0_source_sha": hash(p0Source), "temperature": 0.25, "audit_temperature": 0.1, "layers": []string{"最少任务加P0", "加基础事实", "加条件和关系及用户要求", "加通用表达边界", "加当前沟通方向", "加纯表达风格", "加近期原文避重"}, "prompts": map[string]string{"base": base, "boundary": boundary, "task": task, "recent": recent, "review": review}, "raw_first_draft_only": true, "repair": false, "synthetic_fixtures": true, "business_assets_changed": false, "database_writes": false, "tts": false, "played": false, "production_role_routing_used": false}}
	e.mu.Lock()
	err = e.save()
	e.mu.Unlock()
	if err != nil {
		panic("archive unavailable")
	}
	var wg sync.WaitGroup
	for _, f := range fixtures {
		f := f
		wg.Add(1)
		go func() {
			defer wg.Done()
			prior := []string{}
			for layer := 0; layer < 7; layer++ {
				for repeat := 1; repeat <= 3; repeat++ {
					sys := p0 + "\n\n" + base
					input := "商品类别：" + f.Category + "\n当前商品：" + f.Product + "\n本次内容任务：" + f.Task
					if layer >= 1 {
						input += "\n\n已确认基础事实：\n" + f.Facts
					}
					if layer >= 2 {
						input += "\n\n已确认条件与对象关系：\n" + f.Conditions + "\n\n用户明确表达要求：\n" + f.Constraints
					}
					if layer >= 3 {
						sys += "\n\n" + boundary
					}
					if layer >= 4 {
						sys += "\n\n" + task
						input += "\n\n本段选中的沟通方向：\n" + f.Direction
					}
					if layer >= 5 {
						input += "\n\n主播表达特点：\n" + f.Style
					}
					if layer >= 6 {
						sys += "\n\n" + recent
						input += "\n\n近期正文仅供避重：\n" + strings.Join(prior, "\n")
					}
					call := e.call(ctx, p, "offline_first_draft_gradual_generation", []agentgateway.Message{{Role: "system", Content: sys}, {Role: "user", Content: input}}, 0.25)
					row := map[string]any{"name": f.Name, "layer": layer, "repeat": repeat, "generation": call, "generation_input_sha": hash(sys + input), "generation_material_chars": len([]rune(sys + input)), "known_conditions_provided": layer >= 2, "expression_boundary_provided": layer >= 3}
					if call["completed"] == true {
						response := call["response"].(agentgateway.Response)
						body := response.Text
						scope := "all"
						if f.Category == "熟食" || f.Category == "咖啡豆" {
							scope = "ordinary_food"
						}
						scan := scanner.Scan(body, scope)
						row["program_lexical_scan"] = scan
						row["raw_first_draft"] = body
						row["characters"] = len([]rune(body))
						auditInput := "商品类别：" + f.Category + "\n商品：" + f.Product + "\n本次任务：" + f.Task + "\n完整已确认事实：\n" + f.Facts + "\n" + f.Conditions + "\n完整明确表达要求：\n" + f.Constraints + "\n\n原始首稿（只核对，不修改）：\n" + body
						audit := e.call(ctx, p, "offline_first_draft_gradual_semantic_review", []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + review}, {Role: "user", Content: auditInput}}, 0.1)
						row["model_review"] = audit
						if audit["completed"] == true {
							row["model_verdict"] = verdict(audit["response"].(agentgateway.Response).Text)
						} else {
							row["model_verdict"] = "incomplete"
						}
						if layer == 5 {
							prior = append(prior, body)
						}
					}
					e.append(row)
				}
			}
		}()
	}
	wg.Wait()
	e.mu.Lock()
	e.Metadata["finished_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	e.Metadata["row_count"] = len(e.Rows)
	err = e.save()
	e.mu.Unlock()
	if err != nil {
		panic("final archive unavailable")
	}
	fmt.Printf("done rows=%d model=%s; verdicts are model judgments, not independently proven correctness\n", len(e.Rows), p.Model)
}
