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
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMainAnswerReadOnlyExtensionRepairProvider(t *testing.T) {
	if os.Getenv("MAIN_EXTENSION_PROBE") != "1" {
		t.Skip("explicit paid offline content-parts experiment")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.ResolveTextRole(ctx, "mainline_p0_context")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := store.ResolveSpeechModel(ctx, models.DefaultID)
	if err != nil || writer == nil {
		t.Fatal("writer unavailable")
	}
	current, err := store.ResolveSpeechModel(ctx, reviewID)
	if err != nil || current == nil {
		t.Fatal("auditor unavailable")
	}
	var maxProfile *agentgateway.SpeechModel
	for _, p := range models.Profiles {
		if p.Enabled && p.Model == "qwen3.8-max" {
			maxProfile, err = store.ResolveSpeechModel(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if maxProfile == nil {
		t.Fatal("Max unavailable")
	}
	p0Source, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(p0Source)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := interactionpolicy.Read("model-content-p0-contract.md")
	if err != nil {
		t.Fatal(err)
	}
	p0 := strings.ReplaceAll(string(contract), "{{p0_rules}}", policy.JSON())
	mothers := map[string]string{}
	for _, name := range []string{"main-extension-write.md", "main-answer-boundary-review.md", "extension-only-auto-repair.md", "final-fact-boundary-review.md"} {
		b, e := interactionpolicy.Read(name)
		if e != nil {
			t.Fatal(e)
		}
		mothers[name] = string(b)
	}
	s := &Server{store: store}
	call := func(p *agentgateway.SpeechModel, stage, mother, input string, temp float64, format agentgateway.ResponseFormat) (agentgateway.Response, agentgateway.Request, int64, error) {
		r := agentgateway.Request{Stage: stage, Model: p.Model, Messages: []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother}, {Role: "user", Content: input}}, ResponseFormat: format, MaxTokens: 850, Timeout: 30 * time.Second, EnableThinking: false, Temperature: &temp}
		id, e := s.beginModelExchange(ctx, stage, p.ID, r)
		if e != nil {
			return agentgateway.Response{}, r, 0, e
		}
		started := time.Now()
		c, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		res, e := agentgateway.CompleteSpeechEndpoint(c, *p, r)
		fullMS := time.Since(started).Milliseconds()
		res.Provider, res.Model = "anchor:"+p.ID, p.Model
		e = s.finishModelExchange(ctx, id, res, res.Text, started, res.InputTokens, res.OutputTokens, e)
		return res, r, fullMS, e
	}
	var mu sync.Mutex
	rows := []map[string]any{}
	archive := func() {
		mu.Lock()
		defer mu.Unlock()
		b, e := json.MarshalIndent(map[string]any{"rows": rows, "writer_model": writer.Model, "current_auditor": current.Model, "main_and_final_auditor": maxProfile.Model, "p0_rules_count": len(policy.Data.Rules), "p0_source_sha": derivedHash(p0Source), "p0_contract": p0, "mothers": mothers, "offline": true, "adopted": false, "played": false, "human_midprocess_changes": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("MAIN_EXTENSION_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}
	defer archive()
	slots := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, f := range factTemplateFixtures() {
		f := f
		for repeat := 0; repeat < 2; repeat++ {
			repeat := repeat
			wg.Add(1)
			go func() {
				defer wg.Done()
				slots <- struct{}{}
				defer func() { <-slots }()
				row := map[string]any{"fixture": f, "repeat": repeat, "source_sha": anchorcycle.ProductDigest(f)}
				defer func() {
					mu.Lock()
					rows = append(rows, row)
					mu.Unlock()
					archive()
					t.Logf("completed %s/%s/%d action=%v", f.Product, f.Task, repeat, row["action"])
				}()
				base := fmt.Sprintf("商品类别：%s\n当前问题：%s\n商品必要事实：%s\n本次完整确认事实：%s", f.Category, f.Task, f.Identity, f.Facts)
				generated, req, ms, e := call(writer, "offline_main_extension_generate", mothers["main-extension-write.md"], base, .7, agentgateway.ResponseJSON)
				row["generation"], row["generation_request"], row["generation_full_ms"] = generated, req, ms
				if e != nil {
					row["action"] = "skip_generation_incomplete"
					t.Error("generation incomplete")
					return
				}
				parts, e := anchorcycle.ParseContentParts(generated.Text)
				if e != nil {
					row["action"] = "skip_content_parts_incomplete"
					return
				}
				row["parts"], row["original_body"] = parts, anchorcycle.ComposeContentParts(parts, parts.Extension)
				scope := "all"
				if f.Category == "熟食" || f.Category == "咖啡豆" {
					scope = "ordinary_food"
				}
				mainScan := policy.Scan(parts.Main, scope)
				row["main_program_scan"] = mainScan
				if mainScan.Error != "" {
					row["action"] = "skip_main_scan_incomplete"
					return
				}
				for _, h := range mainScan.Hits {
					if h.ReviewMode == "hard" {
						row["action"] = "skip_main_hard_p0"
						return
					}
				}
				checked, mainReq, mainMS, e := call(maxProfile, "offline_main_answer_review", mothers["main-answer-boundary-review.md"], base+"\n\n待核对主回答：\n"+parts.Main, .2, agentgateway.ResponseText)
				row["main_check"], row["main_check_request"], row["main_check_full_ms"] = checked, mainReq, mainMS
				if e != nil {
					row["action"] = "skip_main_review_incomplete"
					t.Error("main review incomplete")
					return
				}
				if factBoundaryVerdict(checked.Text) != "通过" {
					row["action"] = "skip_main_review"
					return
				}
				arms := []map[string]any{}
				for _, auditor := range []struct {
					label string
					p     *agentgateway.SpeechModel
				}{{"current", current}, {"qwen_max", maxProfile}} {
					a := map[string]any{"role": auditor.label, "model": auditor.p.Model}
					arms = append(arms, a)
					input := base + "\n\n只读主回答（仅参考、不能改动或抄回）：\n" + parts.Main + "\n\n本次要校对的延展：\n" + parts.Extension
					a["identical_extension_input_sha"] = anchorcycle.ProductDigest(p0 + mothers["extension-only-auto-repair.md"] + input)
					res, r, repairMS, e := call(auditor.p, "offline_extension_only_repair", mothers["extension-only-auto-repair.md"], input, .2, agentgateway.ResponseJSON)
					a["repair"], a["repair_request"], a["repair_full_ms"] = res, r, repairMS
					if e != nil {
						a["action"] = "skip_extension_repair_incomplete"
						t.Error("extension repair incomplete")
						continue
					}
					ext, e := anchorcycle.ParseExtensionContent(res.Text)
					if e != nil {
						a["action"] = "skip_extension_content_incomplete"
						continue
					}
					body := anchorcycle.ComposeContentParts(parts, ext)
					a["corrected_extension"], a["assembled_text"], a["main_unchanged"], a["extension_changed"] = ext, body, strings.HasPrefix(body, parts.Main), ext != parts.Extension
					scan := policy.Scan(body, scope)
					a["final_program_scan"] = scan
					hard := false
					for _, h := range scan.Hits {
						if h.ReviewMode == "hard" {
							hard = true
						}
					}
					if hard || scan.Error != "" || textControlLeak(body) {
						a["action"] = "skip_final_program_check"
						continue
					}
					final, fr, finalMS, e := call(maxProfile, "offline_main_extension_final_review", mothers["final-fact-boundary-review.md"], base+"\n\n待核对程序组装稿：\n"+body, .2, agentgateway.ResponseText)
					a["final_check"], a["final_check_request"], a["final_check_full_ms"] = final, fr, finalMS
					if e != nil {
						a["action"] = "skip_final_review_incomplete"
						t.Error("final review incomplete")
						continue
					}
					if factBoundaryVerdict(final.Text) != "通过" {
						a["action"] = "skip_final_review"
						continue
					}
					a["action"], a["final_text"] = "accept", body
				}
				row["arms"], row["action"] = arms, "main_passed_arms_completed"
			}()
		}
	}
	wg.Wait()
	if len(rows) != 32 {
		t.Fatal("cross-category set incomplete")
	}
}
