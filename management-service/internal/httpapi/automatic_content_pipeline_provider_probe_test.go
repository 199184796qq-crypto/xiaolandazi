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

func TestAutomaticGeneratedContentAuditReplacePipeline(t *testing.T) {
	if os.Getenv("AUTO_CONTENT_PIPELINE_PROBE") != "1" {
		t.Skip("explicit paid full pipeline test")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reviewID, err := store.ResolveTextRole(ctx, "mainline_p0_context")
	if err != nil || reviewID == "" {
		t.Fatal("audit role unavailable")
	}
	reviewer, err := store.ResolveSpeechModel(ctx, reviewID)
	if err != nil || reviewer == nil {
		t.Fatal("audit model unavailable")
	}
	writer, err := store.ResolveSpeechModel(ctx, models.DefaultID)
	if err != nil || writer == nil {
		t.Fatal("writer unavailable")
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
	writeMother, err := interactionpolicy.Read("fact-template-write.md")
	if err != nil {
		t.Fatal(err)
	}
	repairMother, err := interactionpolicy.Read("final-content-auto-repair.md")
	if err != nil {
		t.Fatal(err)
	}
	checkMother, err := interactionpolicy.Read("final-fact-boundary-review.md")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	call := func(profile *agentgateway.SpeechModel, stage, mother, input string, temp float64) (agentgateway.Response, agentgateway.Request, error) {
		r := agentgateway.Request{Stage: stage, Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother}, {Role: "user", Content: input}}, ResponseFormat: agentgateway.ResponseText, MaxTokens: 700, Timeout: 20 * time.Second, EnableThinking: false, Temperature: &temp}
		id, e := s.beginModelExchange(ctx, stage, profile.ID, r)
		if e != nil {
			return agentgateway.Response{}, r, e
		}
		started := time.Now()
		c, stop := context.WithTimeout(ctx, 20*time.Second)
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
		raw, e := json.MarshalIndent(map[string]any{"rows": rows, "writer_model": writer.Model, "audit_model": reviewer.Model, "audit_profile_id": reviewID, "p0_source_sha": derivedHash(p0Source), "p0_rules_count": len(policy.Data.Rules), "p0_contract": p0, "writer_mother": string(writeMother), "audit_repair_mother": string(repairMother), "audit_check_mother": string(checkMother), "offline": true, "adopted": false, "played": false, "no_human_midprocess_decision": true}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("AUTO_CONTENT_PIPELINE_OUTPUT"), raw, 0600)
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
				generated, generationRequest, e := call(writer, "offline_auto_pipeline_generate", string(writeMother), base, 0.7)
				row["generation"], row["generation_request"] = generated, generationRequest
				if e != nil {
					row["action"] = "skip_generation_incomplete"
					t.Error("generation incomplete")
					return
				}
				scope := "all"
				if f.Category == "熟食" || f.Category == "咖啡豆" {
					scope = "ordinary_food"
				}
				row["program_initial_scan"] = policy.Scan(generated.Text, scope)
				// Every returned candidate receives final content review. The program
				// owns this handoff, not a human selecting errors for the auditor.
				audited, auditRequest, e := call(reviewer, "offline_auto_pipeline_audit_repair", string(repairMother), base+"\n\n模型刚返回的原正文：\n"+generated.Text, 0.2)
				row["audit_content"], row["audit_request"] = audited, auditRequest
				if e != nil {
					row["action"] = "skip_audit_incomplete"
					t.Error("audit incomplete")
					return
				}
				target, e := anchorcycle.SelectSentenceRephraseTarget(generated.Text, 0, len([]rune(generated.Text)))
				if e != nil {
					row["action"] = "skip_empty_original"
					return
				}
				row["program_target"] = target
				body, e := anchorcycle.ApplySentenceRephrase(generated.Text, target, audited.Text)
				if e != nil || textControlLeak(body) {
					row["action"] = "skip_replacement_incomplete"
					return
				}
				row["program_assembled_text"] = body
				row["changed_by_audit"] = strings.TrimSpace(body) != strings.TrimSpace(generated.Text)
				scan := policy.Scan(body, scope)
				row["program_final_scan"] = scan
				if scan.Error != "" {
					row["action"] = "skip_scan_incomplete"
					return
				}
				for _, hit := range scan.Hits {
					if hit.ReviewMode == "hard" {
						row["action"] = "skip_hard_p0_remaining"
						return
					}
				}
				checked, checkRequest, e := call(reviewer, "offline_auto_pipeline_final_check", string(checkMother), base+"\n\n待核对程序组装稿：\n"+body, 0.2)
				row["final_audit"], row["final_audit_request"] = checked, checkRequest
				verdict := factBoundaryVerdict(checked.Text)
				row["final_verdict"] = verdict
				if e != nil {
					row["action"] = "skip_final_audit_incomplete"
					t.Error("final audit incomplete")
					return
				}
				if verdict != "通过" {
					row["action"] = "skip_final_audit"
					return
				}
				if row["changed_by_audit"] == true {
					row["action"] = "accept_replacement"
				} else {
					row["action"] = "accept_original"
				}
				row["final_text"] = body
			}()
		}
	}
	wg.Wait()
	if len(rows) != 2*len(factTemplateFixtures()) {
		t.Fatal("incomplete cross-product updated-fact pipeline")
	}
}
