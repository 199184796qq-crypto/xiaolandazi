package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/answerflow"
	"livecompanion/management/internal/danmakutest"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/planprompt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The workbench uses the shared input and library contracts.
type danmakuInput = answerflow.Input
type danmakuLibrary = answerflow.Library
type danmakuEmbedder interface {
	Enabled() bool
	Model() string
	EmbedTexts(context.Context, []string) ([][]float32, error)
}
type danmakuComplete func(context.Context, string, []agentgateway.Message) (agentgateway.Response, error)

type danmakuModelStage struct {
	Thinking  string `json:"thinking"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

type danmakuModelStages struct {
	Intent *danmakuModelStage `json:"intent,omitempty"`
	Answer *danmakuModelStage `json:"answer,omitempty"`
}

func (stages *danmakuModelStages) record(profile agentgateway.SpeechModel, request agentgateway.Request, elapsed time.Duration) {
	switch request.Stage {
	case "danmaku_test_intent", "danmaku_test_batch_intent", "danmaku_test_intent_qa":
		stages.Intent = danmakuStageTiming(profile, request, elapsed)
	case "danmaku_test_answer":
		stages.Answer = danmakuStageTiming(profile, request, elapsed)
	}
}

func danmakuStageTiming(profile agentgateway.SpeechModel, request agentgateway.Request, elapsed time.Duration) *danmakuModelStage {
	thinking := "off"
	if request.ReasoningEffort == "low" {
		if agentgateway.SupportsLowReasoning(profile) {
			thinking = "low"
		} else {
			thinking = "low_unavailable"
		}
	} else if request.EnableThinking {
		thinking = "on"
	}
	return &danmakuModelStage{Thinking: thinking, ElapsedMS: elapsed.Milliseconds()}
}

type danmakuTestResponse struct {
	answerflow.RetrievedResult
	ModelStages danmakuModelStages `json:"model_stages"`
}

func (s *Server) loadDanmakuLibrary(ctx context.Context, tenant, plan int64) (danmakuLibrary, error) {
	snapshot, err := s.store.PlanAnswerSnapshot(ctx, tenant, plan)
	if err != nil {
		return danmakuLibrary{}, err
	}
	out := danmakuLibraryFromSnapshot(snapshot)
	if card, ok, cardErr := s.store.GetPlanFactCard(ctx, tenant, plan, snapshot.Revision); cardErr == nil && ok {
		out.FactCard = card
	} else {
		card := danmakutest.BuildFactCard(out.Records, out.Links, out.Stores)
		out.FactCard = card
		if cardErr == nil {
			_ = s.store.PutPlanFactCard(ctx, tenant, plan, snapshot.Revision, card)
		}
	}
	out.Validate = func(ctx context.Context) error {
		current, err := s.store.PlanAnswerSnapshot(ctx, tenant, plan)
		if err != nil {
			return errors.New("读取回答资料状态失败，本次不能确认依据")
		}
		if current.Revision != snapshot.Revision {
			return errors.New("资料、对象、语境词或问答已变化，请重新分析，未自动重试模型")
		}
		return nil
	}
	return out, nil
}

func danmakuLibraryFromSnapshot(snapshot appdb.AnswerSnapshot) danmakuLibrary {
	var out danmakuLibrary
	for _, r := range snapshot.Records {
		out.Records = append(out.Records, danmakutest.Record{Claim: r.Claim, Vector: r.Vector})
	}
	for _, l := range snapshot.Links {
		name := strings.TrimSpace(l.LinkKey)
		if n, err := strconv.ParseInt(name, 10, 64); err == nil {
			name = strconv.FormatInt(n, 10) + "号链接"
		}
		display := snapshot.Objects.Directory.LinkDisplayNames[l.ID]
		if display == "" {
			display = strings.TrimSpace(l.FieldLabels["display_name"])
		}
		out.Links = append(out.Links, danmakutest.Link{ID: l.ID, Name: name, DisplayName: display, Product: l.ProductName, MaterialHidden: slices.Contains(snapshot.Objects.Directory.RemovedLinkIDs, l.ID)})
	}
	for _, object := range snapshot.Objects.Directory.Objects {
		out.Links = append(out.Links, danmakutest.Link{ObjectID: object.ID, Name: object.DisplayName})
	}
	out.Stores = snapshot.Objects.Stores
	out.Words = snapshot.Words.Items
	out.QARecords = snapshot.QA
	return out
}

// rebuildPlanFactCard materializes the answer card for the current revision.
// Claims without a resolvable object never enter a bucket; they are logged so
// the gap is visible, but they must not freeze the card at an older revision.
func (s *Server) rebuildPlanFactCard(ctx context.Context, tenant, plan int64) error {
	snapshot, err := s.store.PlanAnswerSnapshot(ctx, tenant, plan)
	if err != nil {
		return err
	}
	lib := danmakuLibraryFromSnapshot(snapshot)
	card := danmakutest.BuildFactCard(lib.Records, lib.Links, lib.Stores)
	if missing := factCardMissingIDs(lib.Records, card); len(missing) > 0 {
		log.Printf("fact card coverage tenant=%d plan=%d revision=%s missing=%v", tenant, plan, snapshot.Revision, missing)
	}
	return s.store.PutPlanFactCard(ctx, tenant, plan, snapshot.Revision, card)
}

func factCardMissingIDs(records []danmakutest.Record, card map[string]any) []int64 {
	covered := map[int64]bool{}
	if groups, ok := card["facts_by_object"].([]map[string]any); ok {
		for _, group := range groups {
			facts, _ := group["facts"].([]map[string]any)
			for _, fact := range facts {
				if ids, ok := fact["ids"].([]int64); ok {
					for _, id := range ids {
						covered[id] = true
					}
				}
			}
		}
	}
	missing := []int64{}
	for _, r := range records {
		if r.Claim.ID > 0 && !covered[r.Claim.ID] {
			missing = append(missing, r.Claim.ID)
		}
	}
	slices.Sort(missing)
	return missing
}

// scheduleFactCardRebuild rebuilds after a committed write without blocking the
// response. Failures keep the previous card in place.
func (s *Server) scheduleFactCardRebuild(tenant, plan int64) {
	if tenant <= 0 || plan <= 0 {
		return
	}
	go s.rebuildRewriteFacts(tenant, plan)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.rebuildPlanFactCard(ctx, tenant, plan); err != nil {
			log.Printf("fact card rebuild failed tenant=%d plan=%d: %v", tenant, plan, err)
		}
	}()
}
func validateDanmakuInput(in *danmakuInput, manual bool) error {
	return answerflow.ValidateInput(in, manual)
}
func (s *Server) planDanmakuTest(w http.ResponseWriter, r *http.Request) {
	_, plan, tenant, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	manual := strings.HasSuffix(r.URL.Path, "/recall")
	var requestInput struct {
		danmakuInput
		AnswerReasoningEffort string `json:"answer_reasoning_effort"`
	}
	if err := readJSON(w, r, &requestInput); err != nil {
		return
	}
	if requestInput.AnswerReasoningEffort != "" && requestInput.AnswerReasoningEffort != "low" {
		writeError(w, 400, "answer_reasoning_effort 仅支持 low，省略时关闭思考")
		return
	}
	input := requestInput.danmakuInput
	if !manual {
		// The workbench tests the same saved plan as the live Q&A flow.
		input.RoomIntro = ""
	}
	if err := validateDanmakuInput(&input, manual); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	embedder, _ := s.semanticMetrics.(danmakuEmbedder)
	scope := answerflow.Scope{Tenant: tenant, Plan: plan}
	if manual {
		library, err := s.loadDanmakuLibrary(ctx, tenant, plan)
		if err != nil {
			writeError(w, 500, "读取本方案资料、对象或语境词失败，本次尚未调用模型")
			return
		}
		result, err := newDanmakuAnswerService(library, embedder, nil).Recall(ctx, answerflow.Request{Scope: scope, Input: input})
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"groups": result.Recall.Groups, "hits": result.Recall.Hits, "notices": result.Recall.Notices, "matched_qa": result.MatchedQA})
		return
	}
	library, retrieval, err := s.loadDanmakuRetrieval(ctx, tenant, plan)
	if err != nil {
		writeError(w, 500, "读取本方案事实或属性资料失败，本次尚未调用模型")
		return
	}
	roomIntro, systemPrefix, err := s.loadAnswerPlanContext(ctx, tenant, plan)
	if err != nil {
		writeError(w, 500, "读取本方案直播简介或互动提示词失败，本次尚未调用模型")
		return
	}
	input.RoomIntro = roomIntro
	profile, err := s.danmakuPoolProfile(ctx)
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	var stages danmakuModelStages
	complete := func(ctx context.Context, stage string, messages []agentgateway.Message) (agentgateway.Response, error) {
		format := agentgateway.ResponseJSON
		effort := ""
		if stage == "danmaku_test_answer" {
			format = agentgateway.ResponseText
			// 弹幕测试与直播互动一致，回答阶段默认使用低档思考。
			effort = "low"
		}
		request := agentgateway.Request{
			Stage: stage, Model: profile.Model, Messages: messages, MaxTokens: 4000,
			EnableThinking: false, ReasoningEffort: effort, ResponseFormat: format, CacheSystemPrompt: true,
			Timeout: 120 * time.Second,
		}
		if profile.MaxTokens > 0 && profile.MaxTokens < request.MaxTokens {
			request.MaxTokens = profile.MaxTokens
		}
		if profile.TimeoutMS > 0 {
			request.Timeout = time.Duration(profile.TimeoutMS) * time.Millisecond
		}
		request, err := agentgateway.PrepareTextRequest(*profile, request)
		if err != nil {
			return agentgateway.Response{}, err
		}
		id, err := s.beginModelExchange(ctx, stage, profile.ID, request)
		if err != nil {
			return agentgateway.Response{}, err
		}
		started := time.Now()
		result, err := agentgateway.CompleteSpeechEndpoint(ctx, *profile, request)
		stages.record(*profile, request, time.Since(started))
		result.Provider = "pool:" + profile.ID
		result.Model = profile.Model
		err = s.finishModelExchange(ctx, id, result, result.Text, started, result.InputTokens, result.OutputTokens, err)
		return result, err
	}
	service := newDanmakuAnswerService(library, embedder, complete)
	service.AnswerSystemPrefix = systemPrefix
	configureRetrievedDanmakuPrompts(&service)
	out, err := service.AnalyzeRetrieved(ctx, answerflow.Request{Scope: scope, Input: input}, retrieval)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, danmakuTestResponse{RetrievedResult: out, ModelStages: stages})
}

func configureRetrievedDanmakuPrompts(service *answerflow.Service) {
	readPrompt := service.ReadPrompt
	service.ReadPrompt = func(stage answerflow.Stage) ([]byte, error) {
		switch stage {
		case answerflow.IntentStage:
			return interactionpolicy.Read("danmaku-test-intent-v2.md")
		case answerflow.AnswerStage:
			rules, err := interactionpolicy.Read("danmaku-test-answer-v2.md")
			if err != nil {
				return nil, err
			}
			return []byte(string(rules) + "\n\n" + planprompt.InteractionOutputContract), nil
		case answerflow.RetrievalRewriteStage:
			return []byte("改写成一个更容易从知识库检索的问句。保持对象、属性、否定、数量和条件，不回答、不添加事实。只输出 JSON：{\"query\":\"问句\"}。"), nil
		default:
			return readPrompt(stage)
		}
	}
}

func (s *Server) danmakuPoolProfile(ctx context.Context) (*agentgateway.SpeechModel, error) {
	entries, err := s.store.ListModelPoolEntries(ctx)
	if err != nil {
		return nil, errors.New("读取模型池失败，本次尚未调用模型")
	}
	for _, entry := range entries {
		if !entry.Enabled || entry.AvailableRooms <= 0 {
			continue
		}
		profile, err := s.store.ModelPoolCatalogProfile(ctx, entry.ID)
		if err != nil || profile == nil {
			continue
		}
		return profile, nil
	}
	return nil, errors.New("模型池没有可用模型，请先在模型池启用模型并保留名额")
}

// The adapter owns provider selection, usage accounting, prompt locations and
// legacy JSON repair. The answer service has no HTTP, database or live queue dependency.
func newDanmakuAnswerService(lib danmakuLibrary, e danmakuEmbedder, complete danmakuComplete) answerflow.Service {
	return answerflow.Service{
		Load: func(ctx context.Context, _ answerflow.Scope) (answerflow.Library, error) {
			return lib, ctx.Err()
		},
		ReadPrompt: func(stage answerflow.Stage) ([]byte, error) {
			switch stage {
			case answerflow.IntentStage:
				return interactionpolicy.Read("danmaku-test-intent.md")
			case answerflow.AnswerStage:
				rules, err := interactionpolicy.Read("danmaku-test-answer.md")
				if err != nil {
					return nil, err
				}
				return []byte(string(rules) + "\n\n" + planprompt.InteractionOutputContract), nil
			case answerflow.BatchIntentStage:
				return interactionpolicy.Read("danmaku-batch-intent.md")
			default:
				return nil, errors.New("未知回答模型阶段")
			}
		},
		Complete: func(ctx context.Context, stage answerflow.Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
			if complete == nil {
				return agentgateway.Response{}, errors.New("模型未配置")
			}
			return complete(ctx, "danmaku_test_"+string(stage), messages)
		},
		DecodeJSON: func(text string, out any) error {
			value, err := parseLooseJSONValue(text)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, out)
		},
		ValidateQA: func(answer string) error {
			p0, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
			if err != nil {
				return err
			}
			return interactionplan.Validate(interactionplan.Snapshot{P0Markdown: string(p0)}, answer)
		},
		ParseAnswer: func(ctx context.Context, input answerflow.Input, response agentgateway.Response) (answerflow.Reply, error) {
			if err := ctx.Err(); err != nil {
				return answerflow.Reply{}, err
			}
			p0, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
			if err != nil {
				return answerflow.Reply{}, err
			}
			parsed, err := interactionplan.ParseReply(interactionplan.Snapshot{P0Markdown: string(p0)}, interactionplan.Input{Kind: "semantic", Text: input.Question, AllowClarify: true}, response)
			if err != nil {
				return answerflow.Reply{}, err
			}
			action := "answer"
			if parsed.Intent.NeedClarify {
				action = "clarify"
			}
			if parsed.Body == "" {
				action = "skip"
				parsed.Intent.Relevant = false
			}
			return answerflow.Reply{Action: action, Answer: parsed.Body, InteractionIntent: &parsed.Intent, AnswerParse: &parsed.Parse, AnswerModelOutput: response.Text}, nil
		},
		Embed: func(ctx context.Context, queries []string) (answerflow.Embeddings, error) {
			if e == nil || !e.Enabled() {
				return answerflow.Embeddings{}, errors.New("语义召回：尚未配置可用的查询向量服务")
			}
			model := e.Model()
			vectors, err := embedMaterialTexts(ctx, e, queries)
			if err != nil {
				if ctx.Err() != nil {
					return answerflow.Embeddings{}, ctx.Err()
				}
				return answerflow.Embeddings{}, errors.New("语义召回：查询向量调用失败或返回不完整，不能确认召回结果")
			}
			return answerflow.Embeddings{Model: model, Vectors: vectors}, nil
		},
	}
}
