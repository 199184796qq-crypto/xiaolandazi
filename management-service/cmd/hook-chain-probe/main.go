// Explicit paid offline text probe. Reads model connections; never writes a
// plan, exchange, queue, cache, TTS job or live session.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
)

type fact struct {
	ID         string   `json:"id"`
	ProductIDs []string `json:"product_ids"`
	BenefitIDs []string `json:"benefit_ids"`
	Data       any      `json:"data"`
}
type hook struct {
	ID       string   `json:"hook_id"`
	Title    string   `json:"title"`
	Concern  string   `json:"audience_concern"`
	Entry    string   `json:"entry_method"`
	Action   string   `json:"explanation_action"`
	Facts    []string `json:"fact_ids"`
	Products []string `json:"product_ids"`
	Benefits []string `json:"benefit_ids"`
}
type plan struct {
	Goal  string `json:"communication_goal"`
	Hooks []hook `json:"hooks"`
}
type sample struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Source     string         `json:"source"`
	Input      map[string]any `json:"planning_input"`
	Facts      []fact         `json:"facts"`
	Background []fact         `json:"background_facts"`
	Legacy     string         `json:"legacy_angle"`
	Baseline   bool           `json:"baseline"`
	Preview    bool           `json:"preview"`
}
type input struct {
	PlanPrompt     string                 `json:"plan_prompt"`
	SpeechMessages []agentgateway.Message `json:"speech_messages"`
	Style          any                    `json:"style"`
	P0             string                 `json:"p0"`
	Cases          []sample               `json:"cases"`
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "offline probe failed:", err)
		os.Exit(1)
	}
}
func main() {
	inPath := flag.String("input", "", "explicit offline input JSON")
	out := flag.String("output", "", "JSON output")
	live := flag.Bool("live", false, "authorize paid text-only model requests")
	workers := flag.Int("workers", 3, "independent scenario concurrency")
	cycles := flag.Int("cycles", 2, "sequential visits per hook")
	maxHooks := flag.Int("max-hooks", 0, "optional pilot limit")
	baseline := flag.Bool("baseline", false, "run legacy-reference comparison")
	preview := flag.Bool("preview", false, "generate human-only previews after chain")
	briefMemory := flag.Bool("brief-memory", false, "send work records rather than previous utterances")
	flag.Parse()
	if !*live || *inPath == "" || *out == "" || *workers < 1 || *workers > 4 || *cycles < 1 || *cycles > 3 {
		must(fmt.Errorf("explicit live flag and bounded inputs required"))
	}
	raw, err := os.ReadFile(*inPath)
	must(err)
	var data input
	must(json.Unmarshal(raw, &data))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	store, err := db.Open(config.Load())
	must(err)
	defer store.Close()
	maxID, err := store.ResolveTextRole(ctx, "mainline_angle_plan")
	must(err)
	flashID, err := store.ResolveTextRole(ctx, "speech_generation")
	must(err)
	max, err := store.ResolveSpeechModel(ctx, maxID)
	must(err)
	flash, err := store.ResolveSpeechModel(ctx, flashID)
	must(err)
	if max == nil || flash == nil || max.Model != "qwen3.8-max" || flash.Model != "qwen3.7-flash" || !max.Enabled || !flash.Enabled {
		must(fmt.Errorf("expected enabled Max/Flash roles"))
	}
	rows := make([]map[string]any, len(data.Cases))
	var mu sync.Mutex
	result := map[string]any{"text_only": true, "live_sessions_started": false, "tts_generated": false, "plans_saved": false, "model_roles": map[string]string{"planner": max.Model, "generator": flash.Model}, "started_at": time.Now().UTC().Format(time.RFC3339), "input": data, "rows": rows}
	save := func() {
		mu.Lock()
		defer mu.Unlock()
		b, e := json.MarshalIndent(result, "", "  ")
		must(e)
		must(os.MkdirAll(filepath.Dir(*out), 0700))
		must(os.WriteFile(*out+".tmp", b, 0600))
		must(os.Rename(*out+".tmp", *out))
	}
	call := func(profile *agentgateway.SpeechModel, stage string, messages []agentgateway.Message, limit int, format agentgateway.ResponseFormat) map[string]any {
		msgs := append([]agentgateway.Message(nil), messages...)
		if profile.SystemPrompt != "" {
			msgs = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, msgs...)
		}
		body, _ := json.Marshal(msgs)
		req := agentgateway.Request{Model: profile.Model, Stage: stage, Messages: msgs, MaxTokens: limit, ResponseFormat: format, EnableThinking: false, Timeout: 90 * time.Second}
		// This probe explicitly supplies the foundation and model adaptation,
		// with fixed example utterances removed. Preserve exactly that contract.
		if stage == "speech_generation" {
			req.TextPromptID = "hook-chain-no-example-probe"
			req.TextPromptSHA = fmt.Sprintf("%x", sha256.Sum256(body))
		}
		r, e := agentgateway.CompleteSpeechEndpoint(ctx, *profile, req)
		r.Provider = "anchor:" + profile.ID
		r.Model = profile.Model
		row := map[string]any{"request": req, "response": r, "success": e == nil, "at": time.Now().UTC().Format(time.RFC3339)}
		if e != nil {
			row["error"] = e.Error()
		}
		return row
	}
	save()
	var wg sync.WaitGroup
	slots := make(chan struct{}, *workers)
	for index, c := range data.Cases {
		wg.Add(1)
		slots <- struct{}{}
		go func(index int, c sample) {
			defer wg.Done()
			defer func() { <-slots }()
			packet := map[string]any{}
			for k, v := range c.Input {
				packet[k] = v
			}
			packet["confirmed_facts"] = c.Facts
			packet["effective_p0"] = data.P0
			b, _ := json.Marshal(packet)
			planning := call(max, "mainline_angle_plan", []agentgateway.Message{{Role: "system", Content: data.PlanPrompt}, {Role: "user", Content: string(b)}}, 4500, agentgateway.ResponseJSON)
			row := map[string]any{"id": c.ID, "label": c.Label, "source": c.Source, "planning": planning}
			mu.Lock()
			rows[index] = row
			mu.Unlock()
			save()
			p := plan{}
			resp := planning["response"].(agentgateway.Response)
			parseErr := json.Unmarshal([]byte(resp.Text), &p)
			if parseErr == nil {
				parseErr = validatePlan(resp.Text, p, c)
			}
			if parseErr != nil || planning["success"] != true {
				mu.Lock()
				row["structure_error"] = fmt.Sprint(parseErr)
				mu.Unlock()
				save()
				fmt.Printf("case=%s planning_structure_failed\n", c.ID)
				return
			}
			mu.Lock()
			row["parsed_plan"] = p
			mu.Unlock()
			save()
			hs := p.Hooks
			if *maxHooks > 0 && len(hs) > *maxHooks {
				hs = hs[:*maxHooks]
			}
			recent := ""
			speeches := []map[string]any{}
			comparisonHistory := []string{}
			workRecords := []map[string]any{}
			generate := func(h hook, phase string, history, legacy string) map[string]any {
				refs := []fact{}
				for _, f := range c.Facts {
					for _, id := range h.Facts {
						if f.ID == id {
							refs = append(refs, f)
							break
						}
					}
				}
				// Select only fields that describe the content task. Never forward
				// a stored preview, original angle paragraph or full candidate bank.
				selected := map[string]any{"要解决的问题": h.Concern, "切入关系": h.Entry, "解释动作": h.Action}
				background := []fact{}
				for _, f := range c.Background {
					bound := []string{}
					for _, id := range f.ProductIDs {
						for _, allowed := range h.Products {
							if id == allowed {
								bound = append(bound, id)
								break
							}
						}
					}
					if len(bound) > 0 {
						f.ProductIDs = bound
						background = append(background, f)
					}
				}
				sp := map[string]any{"本次任务": map[string]any{"目的": p.Goal, "选中钩子": selected, "授权商品": h.Products, "授权福利": h.Benefits}, "本段关联事实": refs, "当前授权商品的完整事实仅用于理解和边界": background, "字数上限": 220, "建议字数范围": "100至180字，按实际信息量处理", "阶段": "body", "近期测试已生成内容仅供避重": history, "本次最终要求": "第一句从本段具体问题进入，称呼放后面；只解释当前钩子，不串讲其它主题；后台编号和工作说明不能念；不复制近期稿；只返回正文。完整事实用来保持条件准确，不要求逐条介绍，不追加本段未选择的福利。"}
				if *briefMemory {
					delete(sp, "近期测试已生成内容仅供避重")
					sp["近期工作记录仅用于避重不是台词"] = append([]map[string]any(nil), workRecords...)
					sp["明确商品与链接映射"] = c.Input["link_mapping"]
					sp["生成序号"] = phase
				}
				if legacy != "" {
					sp["旧角度参考话术仅作对照"] = legacy
				}
				b, _ := json.Marshal(sp)
				fixed, _ := json.Marshal(map[string]any{"主播表达标尺": data.Style, "有效P0": data.P0})
				var systems []string
				for _, m := range data.SpeechMessages {
					systems = append(systems, m.Content)
				}
				systems = append(systems, string(fixed))
				msgs := []agentgateway.Message{{Role: "system", Content: strings.Join(systems, "\n\n")}}
				msgs = append(msgs, agentgateway.Message{Role: "user", Content: string(b)})
				r := call(flash, "speech_generation", msgs, 900, agentgateway.ResponseText)
				r["hook_id"] = h.ID
				r["phase"] = phase
				return r
			}
			for cycle := 1; cycle <= *cycles; cycle++ {
				for _, h := range hs {
					if cycle == 1 && len(comparisonHistory) < 2 {
						comparisonHistory = append(comparisonHistory, recent)
					}
					r := generate(h, fmt.Sprintf("cycle-%d", cycle), recent, "")
					speeches = append(speeches, r)
					if r["success"] == true {
						recent += "\n" + r["response"].(agentgateway.Response).Text
						rr := []rune(recent)
						if len(rr) > 1000 {
							recent = string(rr[len(rr)-1000:])
						}
						workRecords = append(workRecords, map[string]any{"已讲方向": h.Title, "所用解释动作": h.Action, "所用事实": h.Facts})
						if len(workRecords) > 8 {
							workRecords = workRecords[len(workRecords)-8:]
						}
					}
					mu.Lock()
					row["speeches"] = append([]map[string]any(nil), speeches...)
					mu.Unlock()
					save()
					fmt.Printf("case=%s hook=%s cycle=%d success=%v\n", c.ID, h.ID, cycle, r["success"])
				}
			}
			if *baseline && c.Baseline {
				baselines := []map[string]any{}
				for i := 0; i < 2 && i < len(hs); i++ {
					baselines = append(baselines, generate(hs[i], "legacy-reference-comparison", comparisonHistory[i], c.Legacy))
				}
				mu.Lock()
				row["baselines"] = baselines
				mu.Unlock()
				save()
			}
			if *preview && c.Preview && len(hs) > 0 {
				display := generate(hs[0], "human-only-display-preview", "", "")
				mu.Lock()
				row["human_only_preview"] = display
				mu.Unlock()
				save()
			}
		}(index, c)
	}
	wg.Wait()
	result["finished_at"] = time.Now().UTC().Format(time.RFC3339)
	save()
	fmt.Println("offline text probe complete")
}

// Only protocol and references. No business semantic approval or live filter.
func validatePlan(raw string, p plan, c sample) error {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &obj) != nil || len(obj) != 2 || obj["communication_goal"] == nil || obj["hooks"] == nil || strings.TrimSpace(p.Goal) == "" || len(p.Hooks) == 0 {
		return fmt.Errorf("invalid planning structure")
	}
	limit, _ := c.Input["hook_limit"].(float64)
	if limit > 0 && len(p.Hooks) > int(limit) {
		return fmt.Errorf("hook count exceeds request")
	}
	known := map[string]fact{}
	authorized := map[string]bool{}
	if items, ok := c.Input["product_ids"].([]any); ok {
		for _, id := range items {
			authorized[fmt.Sprint(id)] = true
		}
	}
	for _, f := range c.Facts {
		known[f.ID] = f
	}
	ids := map[string]bool{}
	var rawHooks []map[string]json.RawMessage
	_ = json.Unmarshal(obj["hooks"], &rawHooks)
	for i, h := range p.Hooks {
		if len(rawHooks[i]) != 8 || ids[h.ID] || strings.TrimSpace(h.ID) == "" || h.Title == "" || h.Concern == "" || h.Entry == "" || h.Action == "" || len(h.Facts) == 0 || len(h.Products) == 0 || h.Benefits == nil {
			return fmt.Errorf("incomplete hook structure")
		}
		ids[h.ID] = true
		products := map[string]bool{}
		benefits := map[string]bool{}
		for _, id := range h.Facts {
			f, ok := known[id]
			if !ok {
				return fmt.Errorf("unknown fact reference %s", id)
			}
			for _, p := range f.ProductIDs {
				products[p] = true
			}
			for _, b := range f.BenefitIDs {
				benefits[b] = true
			}
		}
		for _, id := range h.Products {
			if !products[id] || !authorized[id] {
				return fmt.Errorf("product reference not supported by facts")
			}
		}
		for _, id := range h.Benefits {
			if !benefits[id] {
				return fmt.Errorf("benefit reference not supported by facts")
			}
		}
	}
	return nil
}
