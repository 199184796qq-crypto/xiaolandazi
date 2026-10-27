// Read-only probe for the interaction intent step: resolves one viewer question
// against a room's published facts and purpose set, without starting a live
// session, writing semantic caches or charging a room ledger.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/factbound"
	"livecompanion/management/internal/fallbackline"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/interactionpurpose"
	"livecompanion/management/internal/semantic"
)

type readOnlyRepo struct{ store *db.Store }

// softenTerms replaces an unconfirmed value with a vague reference.
func softenTerms(text string, terms []string) string {
	out := strings.TrimSpace(text)
	for _, term := range terms {
		if strings.TrimSpace(term) == "" {
			continue
		}
		out = strings.ReplaceAll(out, term, "这个范围")
	}
	return strings.TrimSpace(out)
}

var coveredHedgeWords = []string{"没核实", "没确认", "未确认", "未核实", "没有核实", "没有确认", "还没核实", "还没确认", "尚未核实", "尚未确认", "说不好", "不确定", "需要再确认", "无法确认"}

func stripHedges(text string) string {
	var kept []string
	for _, sentence := range regexp.MustCompile(`[^。！？!?；;\n]+[。！？!?；;\n]?`).FindAllString(text, -1) {
		hedged := false
		for _, word := range coveredHedgeWords {
			if strings.Contains(sentence, word) {
				hedged = true
				break
			}
		}
		if !hedged {
			kept = append(kept, sentence)
		}
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

func probeBoundaryLine(ctx context.Context, profile *agentgateway.SpeechModel, class, question string, uncovered []string) (string, []fallbackline.Candidate) {
	var verdicts []fallbackline.Candidate
	for attempt := 0; attempt < 2; attempt++ {
		request := fallbackline.Request{Class: class, Question: question, Uncovered: uncovered}
		for _, previous := range verdicts {
			request.Recent = append(request.Recent, previous.Text)
		}
		system, user := fallbackline.CandidatesPrompt(request, 4)
		spoken, err := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
			Stage:          "interaction_boundary_line",
			Model:          profile.Model,
			Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
			MaxTokens:      240,
			ResponseFormat: agentgateway.ResponseText,
		})
		if err != nil {
			break
		}
		candidates := fallbackline.ParseCandidates(spoken.Text)
		verdicts = append(verdicts, fallbackline.Evaluate(request, candidates)...)
		if line, pickErr := fallbackline.Pick(request, candidates); pickErr == nil {
			return line, verdicts
		}
	}
	line := factbound.BoundaryTemplateFor(class, uncovered)
	if line == "" {
		line = "这个我还没确认，先不跟你说死。"
	}
	verdicts = append(verdicts, fallbackline.Candidate{Text: line, OK: true, Reason: "template", Score: 0})
	return line, verdicts
}

// welcomeFacts splits the approved facts into room-intro facts and benefit facts.
func welcomeFacts(facts string) (string, string) {
	var benefit []string
	for _, line := range strings.Split(facts, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- [") {
			continue
		}
		if strings.Contains(line, "福利") || strings.Contains(line, "活动") || strings.Contains(line, "包邮") ||
			strings.Contains(line, "赠品") || strings.Contains(line, "优惠") {
			if len(benefit) < 3 {
				benefit = append(benefit, line)
			}
		}
	}
	return factbound.SpeakableFacts(facts), factbound.SpeakableFacts(strings.Join(benefit, "\n"))
}

func (r readOnlyRepo) GetInteractionPurpose(context.Context, int64, int64, int64, string) (string, error) {
	return "", semantic.ErrDocumentNotFound
}
func (r readOnlyRepo) UpsertSemanticDocument(context.Context, semantic.StoredDocument) error {
	return nil
}
func (r readOnlyRepo) ListPurposeBuckets(context.Context, int64, int64, int64, int) ([]semantic.PurposeBucket, error) {
	return nil, nil
}
func (r readOnlyRepo) UpsertPurposeBucket(context.Context, int64, int64, int64, string, string, string) error {
	return nil
}
func (r readOnlyRepo) GetPurposeBucketConclusion(context.Context, int64, int64, int64, string) (string, error) {
	return "", semantic.ErrDocumentNotFound
}

func main() {
	tenant := flag.Int64("tenant", 0, "tenant id")
	room := flag.Int64("room", 0, "room id")
	text := flag.String("text", "", "viewer utterance")
	answer := flag.Bool("answer", false, "also generate the spoken answer with the review model")
	welcome := flag.Bool("welcome", false, "generate the welcome package (greeting + intro + benefit + human rewrite)")
	summary := flag.Bool("summary", false, "ask the review model to summarise what the room sells from the full facts")
	flag.Parse()
	if *tenant <= 0 || *room <= 0 || *text == "" {
		fmt.Println("usage: -tenant N -room N -text 观众原话")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	store, err := db.Open(config.Load())
	if err != nil {
		panic(err)
	}
	defer store.Close()

	plan, err := store.GetLiveAgentPlanForRoom(ctx, *tenant, *room)
	if err != nil || plan.ID <= 0 {
		panic(fmt.Sprintf("plan unavailable: %v", err))
	}
	published, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, *tenant, *room)
	if err != nil || published.PlanID != plan.ID {
		panic(fmt.Sprintf("published version unavailable: %v", err))
	}
	ref, _ := published.GenerationContext["derived_safety"].(map[string]any)
	hash, _ := ref["source_sha256"].(string)
	asset, err := store.GetLiveDerivedSafety(ctx, *tenant, plan.ID, hash)
	if err != nil || asset.SafeFacts == "" {
		panic(fmt.Sprintf("safe facts unavailable: %v", err))
	}
	prompt, err := interactionpolicy.Read("interaction-purpose-resolution.md")
	if err != nil {
		panic(err)
	}
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		panic(err)
	}
	_ = settings
	roleID, err := store.ResolveTextRole(ctx, "interaction_purpose_resolution")
	if err != nil {
		panic(err)
	}
	profile, err := store.ResolveOperatorSpeechModel(ctx, roleID)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		panic("intent model connection unavailable")
	}

	query := interactionpurpose.Input{Tenant: *tenant, Room: *room, Plan: plan.ID, Text: *text, Facts: asset.SafeFacts}
	if raw, err := store.GetLatestPlanContentStrategy(ctx, *tenant, plan.ID, contentstrategy.SHA(asset.SafeFacts), published.CreatedAt); err == nil {
		if snapshot, err := contentstrategy.Parse(raw, asset.SafeFacts); err == nil {
			query.Purposes = snapshot.Purposes
		}
	}

	resolver := interactionpurpose.Resolver{Repo: readOnlyRepo{store: store}, Complete: func(ctx context.Context, payload string) (string, error) {
		result, err := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
			Stage:          "interaction_purpose_resolution",
			Model:          profile.Model,
			Messages:       []agentgateway.Message{{Role: "system", Content: string(prompt)}, {Role: "user", Content: payload}},
			MaxTokens:      1800,
			ResponseFormat: agentgateway.ResponseJSON,
		})
		return result.Text, err
	}}
	if judgePrompt, err := interactionpolicy.Read("interaction-purpose-judge.md"); err == nil && len(judgePrompt) > 0 {
		resolver.Judge = func(ctx context.Context, payload string) (string, error) {
			result, err := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
				Stage:          "interaction_purpose_judge",
				Model:          profile.Model,
				Messages:       []agentgateway.Message{{Role: "system", Content: string(judgePrompt)}, {Role: "user", Content: payload}},
				MaxTokens:      128,
				ResponseFormat: agentgateway.ResponseJSON,
			})
			return result.Text, err
		}
	}

	result, err := resolver.Resolve(ctx, query)
	if err != nil {
		fmt.Printf("RESOLVE ERROR: %v\n", err)
		os.Exit(1)
	}
	if *summary {
		system := "你是直播间的真人主播。根据下面全部已确认事实，用一句话高度概括这个直播间是干什么的：" +
			"卖什么、给谁、有什么特点。要求：不超过40字；口语；只能使用事实，不编造；不要罗列商品和价格；不要称呼；不要客服腔。" +
			"\n必须说成“卖XX（产品名）+ 品类”，例如“卖四川特色美食绸都杨鸭子，卤味熟食”；" +
			"原料（土麻鸭、鸭坯）只能作为工艺补充放在后面，绝不能当商品说。" +
			"\n只输出这句话本身。"
		user := "已确认事实：\n" + factbound.SpeakableFacts(asset.SafeFacts)
		var samples []string
		for i := 0; i < 3; i++ {
			out, callErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
				Stage:          "interaction_welcome_intro",
				Model:          profile.Model,
				Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
				MaxTokens:      100,
				ResponseFormat: agentgateway.ResponseText,
			})
			if callErr != nil {
				fmt.Printf("SUMMARY ERROR: %v\n", callErr)
				os.Exit(1)
			}
			samples = append(samples, strings.TrimSpace(out.Text))
		}
		out, _ := json.MarshalIndent(map[string]any{"model": profile.Model, "samples": samples}, "", "  ")
		fmt.Println(string(out))
		return
	}
	if *welcome {
		introFacts, benefitFacts := welcomeFacts(asset.SafeFacts)
		call := func(stage, system, user string, max int) string {
			out, callErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
				Stage:          stage,
				Model:          profile.Model,
				Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
				MaxTokens:      max,
				ResponseFormat: agentgateway.ResponseText,
			})
			if callErr != nil {
				return ""
			}
			return strings.TrimSpace(out.Text)
		}
		greeting := call("interaction_welcome_greeting", "你是直播间主播，用一句话欢迎刚进来的朋友，带一个称呼，不要讲福利。只输出这句话。", "称呼用“朋友们”。", 60)
		intro := ""
		if introFacts != "" {
			for attempt := 0; attempt < 2 && intro == ""; attempt++ {
				system := "你是直播间的真人主播。用一句到两句话告诉刚进来的朋友：这个直播间是干什么的、卖什么，20到45字。只能使用下面的事实，不得编造；不要称呼，不要讲福利和价格，不要客服腔。先说卖的产品（品牌/产品名，例如“四川特色美食绸都杨鸭子”），再说品类（卤味熟食/开袋即食）；原料只能放在后面补充，绝不能说成在卖原料。禁止出现“活鸭、活禽、卖鸭”这类说法。只输出这句话本身。"
				if attempt > 0 {
					system += "\n上一次没有说清品类；这次必须出现“四川特色美食/卤味/熟食/特产”这类词。"
				}
				candidate := call("interaction_welcome_intro", system, "已确认事实：\n"+introFacts, 100)
				if factbound.ValidateIntroFor(candidate, introFacts) == nil {
					intro = candidate
				}
			}
		}
		hook := ""
		if benefitFacts != "" {
			hook = call("interaction_welcome_hook", "你是直播间的真人主播，用一句话介绍当前福利，只能使用给定事实，不要称呼，不要编造。只输出这句话。", "当前有效福利：\n"+benefitFacts, 80)
		}
		assembled := strings.TrimSpace(strings.Join([]string{greeting, intro, hook}, "\n"))
		polished := ""
		if system, user := fallbackline.HumanizePrompt(assembled); system != "" {
			if candidate := call("interaction_welcome_humanize", system, user, 240); candidate != "" && fallbackline.ValidateHumanized(assembled, candidate) == nil {
				polished = candidate
			}
		}
		out, _ := json.MarshalIndent(map[string]any{
			"model": profile.Model, "greeting": greeting, "intro": intro, "hook": hook,
			"assembled": assembled, "humanized": polished,
		}, "", "  ")
		fmt.Println(string(out))
		return
	}
	if *answer {
		factMap := map[string]string{}
		for _, f := range contentstrategy.Facts(asset.SafeFacts) {
			factMap[f.ID] = f.Text
		}
		var ids []string
		for _, row := range result.Angles {
			ids = append(ids, row.FactIDs...)
		}
		var b strings.Builder
		for _, id := range ids {
			if body, ok := factMap[id]; ok {
				fmt.Fprintf(&b, "%s %s\n", id, body)
			}
		}
		facts := strings.TrimSpace(b.String())
		hasFacts := facts != ""
		coverage := factbound.Evaluate(*text, facts, hasFacts)
		if !hasFacts {
			class := "no_dimension"
			for _, marker := range []string{"名字", "叫什么", "你是谁", "怎么称呼", "贵姓", "哪位"} {
				if strings.Contains(*text, marker) {
					class = "identity"
				}
			}
			var attempts []string
			accepted := ""
			for attempt := 0; attempt < 3 && accepted == ""; attempt++ {
				request := fallbackline.Request{Class: class, Question: *text, Recent: attempts}
				_, system, user := fallbackline.Prompt(request)
				spoken, callErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
					Stage:          "interaction_boundary_line",
					Model:          profile.Model,
					Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
					MaxTokens:      80,
					ResponseFormat: agentgateway.ResponseText,
				})
				if callErr != nil {
					fmt.Printf("ANSWER ERROR: %v\n", callErr)
					os.Exit(1)
				}
				candidate := strings.Trim(strings.TrimSpace(spoken.Text), "\"“”'")
				if fallbackline.Validate(candidate, request) == nil {
					accepted = candidate
					attempts = append(attempts, candidate)
					break
				}
				attempts = append(attempts, "[rejected] "+candidate)
			}
			if accepted == "" {
				accepted = factbound.BoundaryTemplate(class)
			}
			outAnswer, _ := json.MarshalIndent(map[string]any{
				"model":       profile.Model,
				"question":    *text,
				"intent":      result,
				"coverage":    coverage,
				"facts_used":  "",
				"attempts":    attempts,
				"spoken_text": "朋友们，" + accepted,
			}, "", "  ")
			fmt.Println(string(outAnswer))
			return
		}
		system := "你是直播口播合成器。根据答案事实直接回答观众，第一句必须给出答案，不要先评价问题，也不要宣布“我这就讲/确认”。" +
			"\n\n【必说】开头用称呼“朋友们”，随后直接回应用户实际问的点；不要重复称呼，不要先评价问题。" +
			"\n只选与问题直接相关的事实，不要罗列无关事实，不要输出事实编号。" +
			"\n如果给出的答案事实只覆盖一部分，必须明确说出哪一点还没核实，不能把相关事实说成已经确认。" +
			"\n禁止出现“问得好、我这就、马上、稍等”等评价问题或让观众等待的说法。" +
			"\n【可说】只使用下方答案事实，不编造事实里没有的信息。" +
			"\n【不提】不暗示已经核实，不假装查过，不承诺稍后回复；不要输出内部标题、字段名或分析过程。" +
			"\n【改写】答案用口语自然表达，通常一两句；事实里的价格、规格、时效不得改动；回答到位就结束。"
		user := "观众问题：" + *text + "\n【当前答案事实】\n" + factbound.SpeakableFacts(facts) +
			"\n\n请直接输出一句自然、简短、可播出的回答（称呼 + 答案，不要再称呼第二次）。"
		coverage = factbound.Evaluate(*text, facts, true)
		uncoveredTexts := make([]string, 0, len(coverage.Uncovered))
		for _, value := range coverage.Uncovered {
			uncoveredTexts = append(uncoveredTexts, value.Text)
		}
		if len(uncoveredTexts) > 0 {
			system += "\n【只答已确认】只回答答案事实里已确认的部分，不要提及、不要确认：" + strings.Join(uncoveredTexts, "、") + "。"
		}
		spoken, callErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
			Stage:          "interaction_single_direct_answer",
			Model:          profile.Model,
			Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
			MaxTokens:      100,
			ResponseFormat: agentgateway.ResponseText,
		})
		if callErr != nil {
			fmt.Printf("ANSWER ERROR: %v\n", callErr)
			os.Exit(1)
		}
		spokenText := strings.TrimSpace(spoken.Text)
		var boundaryVerdicts []fallbackline.Candidate
		if coverage.NeedsBoundary() {
			spokenText = softenTerms(spokenText, uncoveredTexts)
			spokenText = stripHedges(spokenText)
			if strings.TrimSpace(spokenText) == "" {
				factsOnly, factsErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{
					Stage: "interaction_covered_facts",
					Model: profile.Model,
					Messages: []agentgateway.Message{
						{Role: "system", Content: "只陈述下面事实里已确认的信息，用一两句口语，不要评价问题。禁止出现“没核实、没确认、说不好、不确定”这些词，也不要提到：" + strings.Join(uncoveredTexts, "、") + "。"},
						{Role: "user", Content: "已确认事实：\n" + factbound.SpeakableFacts(facts)},
					},
					MaxTokens:      80,
					ResponseFormat: agentgateway.ResponseText,
				})
				if factsErr == nil {
					spokenText = strings.TrimSpace(factsOnly.Text)
				}
			}
			line, verdicts := probeBoundaryLine(ctx, profile, "partial_value", *text, uncoveredTexts)
			boundaryVerdicts = verdicts
			if strings.TrimSpace(spokenText) == "" {
				spokenText = line
			} else {
				spokenText = strings.TrimRight(spokenText, "。！？!?；; ") + "。" + line
			}
		}
		outAnswer, _ := json.MarshalIndent(map[string]any{
			"model":             profile.Model,
			"question":          *text,
			"intent":            result,
			"coverage":          coverage,
			"facts_used":        facts,
			"model_text":        spoken.Text,
			"candidates":        boundaryVerdicts,
			"spoken_text":       spokenText,
		}, "", "  ")
		fmt.Println(string(outAnswer))
		return
	}
	out, _ := json.MarshalIndent(map[string]any{
		"room":         *room,
		"plan":         plan.ID,
		"plan_name":    plan.Name,
		"text":         *text,
		"model":        profile.Model,
		"facts_sha":    contentstrategy.SHA(asset.SafeFacts),
		"result":       result,
		"purpose_list": query.Purposes,
	}, "", "  ")
	fmt.Println(string(out))
}
