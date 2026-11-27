package answerflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

type Stage string

const (
	IntentStage      Stage = "intent"
	AnswerStage      Stage = "answer"
	BatchIntentStage Stage = "batch_intent"
)

type Point string

const (
	BeforeLoad      Point = "load"
	BeforeIntent    Point = "intent"
	BeforeEmbedding Point = "embedding"
	BeforeRerank    Point = "rerank"
	BeforeAnswer    Point = "answer"
	BeforeReturn    Point = "return"
)

// Scope is program-owned; it never enters a model message.
type Scope struct{ Tenant, Plan int64 }
type Turn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
type Message struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at,omitempty"`
}

func matchedWords(lib Library, input Input) *contextword.Context {
	text := input.Question
	if input.Original != "" {
		text += "\n" + input.Original
	}
	return contextword.Match(lib.Words, text)
}

type Input struct {
	Question              string    `json:"question"`
	RoomIntro             string    `json:"room_intro"`
	Queries               []string  `json:"queries"`
	Original              string    `json:"original,omitempty"`
	Clarification         string    `json:"clarification,omitempty"`
	CurrentMainline       string    `json:"current_mainline,omitempty"`
	Recent                []Turn    `json:"recent,omitempty"`
	Context               []Message `json:"context,omitempty"`
	AnswerReasoningEffort string    `json:"-"`
}
type Request struct {
	Scope Scope
	Input Input
}
type Reply struct {
	Action            string                            `json:"action"`
	Answer            string                            `json:"answer"`
	InteractionIntent *interactionplan.Intent           `json:"interaction_intent,omitempty"`
	AnswerParse       *interactionplan.ParseDiagnostics `json:"answer_parse,omitempty"`
	AnswerModelOutput string                            `json:"answer_model_output,omitempty"`
}

type answerDecodeTarget struct {
	input Input
	reply *Reply
}
type Result struct {
	Question        string               `json:"question"`
	Intent          danmakutest.Intent   `json:"intent"`
	IntentModelJSON string               `json:"intent_model_json"`
	ContextWords    *contextword.Context `json:"context_words,omitempty"`
	Recall          danmakutest.Recall   `json:"recall"`
	MatchedQA       []qapair.Group       `json:"matched_qa,omitempty"`
	Reply
}
type RecallResult struct {
	Recall    danmakutest.Recall
	MatchedQA []qapair.Group
}
type Library struct {
	FactCard  map[string]any
	Records   []danmakutest.Record
	Links     []danmakutest.Link
	Stores    []model.MaterialStoreObject
	Words     []contextword.Entry
	QARecords []qapair.Record
	// The source defines what it can validate. It must not hold a DB transaction
	// open during remote calls. The workbench validates all knowledge sources.
	Validate func(context.Context) error
}
type Embeddings struct {
	Model   string
	Vectors [][]float32
}

type modelCallError struct{ cause error }

func (e modelCallError) Error() string { return "模型调用失败，未自动重试" }
func (e modelCallError) Unwrap() error { return e.cause }

// Service owns orchestration only. Adapters own auth, snapshots, provider
// routing, vector validation/batching and usage receipts. No retries or TTS.
type Service struct {
	Load           func(context.Context, Scope) (Library, error)
	Embed          func(context.Context, []string) (Embeddings, error)
	Complete       func(context.Context, Stage, []agentgateway.Message) (agentgateway.Response, error)
	CompleteAnswer func(context.Context, string, []agentgateway.Message) (agentgateway.Response, error)
	ReadPrompt     func(Stage) ([]byte, error)
	DecodeJSON     func(string, any) error
	ParseAnswer    func(context.Context, Input, agentgateway.Response) (Reply, error)
	ValidateQA     func(string) error
	Guard          func(context.Context, Scope, Point) error
	Now            func() time.Time
	// AnswerSystemPrefix holds the saved plan rules for the answer stage.
	AnswerSystemPrefix string
	// Workbench keeps 1000; the live adapter may retain the old 2400 limit.
	QuestionLimit int
}

const RoomIntroLimit = 2000

func ValidateInput(in *Input, manual bool) error {
	return validateInput(in, manual, 1000)
}
func (s Service) validateInput(in *Input, manual bool) error {
	limit := s.QuestionLimit
	if limit <= 0 || limit > 2400 {
		limit = 1000
	}
	return validateInput(in, manual, limit)
}
func validateInput(in *Input, manual bool, questionLimit int) error {
	in.Question, in.RoomIntro = strings.TrimSpace(in.Question), strings.TrimSpace(in.RoomIntro)
	if manual {
		if len(in.Queries) < 1 || len(in.Queries) > 10 {
			return errors.New("请输入1至10条查询，每行一条")
		}
		for _, q := range in.Queries {
			if strings.TrimSpace(q) == "" || len([]rune(q)) > 1000 {
				return errors.New("每条查询限1至1000字")
			}
		}
		return nil
	}
	if in.Question == "" || len([]rune(in.Question)) > questionLimit || len([]rune(in.RoomIntro)) > RoomIntroLimit {
		return fmt.Errorf("弹幕限1至%d字，直播间介绍限%d字", questionLimit, RoomIntroLimit)
	}
	if len(in.Recent) > 5 || len(in.Context) > 5 || len([]rune(in.Original)) > 2400 || len([]rune(in.Clarification)) > 2400 || len([]rune(in.CurrentMainline)) > 8000 {
		return errors.New("互动上下文超出范围")
	}
	for _, turn := range in.Recent {
		if len([]rune(turn.Question)) > 2400 || len([]rune(turn.Answer)) > 2400 {
			return errors.New("互动上下文超出范围")
		}
	}
	for _, message := range in.Context {
		if (message.Role != "user" && message.Role != "assistant") || len([]rune(message.Text)) > 2400 {
			return errors.New("互动上下文格式错误")
		}
	}
	return nil
}
func (s Service) check(ctx context.Context, scope Scope, point Point) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if scope.Tenant <= 0 || scope.Plan <= 0 {
		return errors.New("回答流程的租户或方案身份无效")
	}
	if s.Guard != nil {
		if err := s.Guard(ctx, scope, point); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (s Service) load(ctx context.Context, request Request, manual bool) (Library, error) {
	if err := s.validateInput(&request.Input, manual); err != nil {
		return Library{}, err
	}
	if err := s.check(ctx, request.Scope, BeforeLoad); err != nil {
		return Library{}, err
	}
	if s.Load == nil {
		return Library{}, errors.New("回答资料源未配置")
	}
	lib, err := s.Load(ctx, request.Scope)
	if err != nil {
		return lib, err
	}
	return lib, ctx.Err()
}
func (s Service) Analyze(ctx context.Context, request Request) (Result, error) {
	if err := s.validateInput(&request.Input, false); err != nil {
		return Result{}, err
	}
	lib, err := s.load(ctx, request, false)
	if err != nil {
		return Result{}, err
	}
	return s.analyze(ctx, request.Scope, request.Input, lib)
}
func (s Service) Recall(ctx context.Context, request Request) (RecallResult, error) {
	lib, err := s.load(ctx, request, true)
	if err != nil {
		return RecallResult{}, err
	}
	recall, qa, err := s.recall(ctx, request.Scope, lib, nil, request.Input.Queries, true)
	if err != nil {
		return RecallResult{}, err
	}
	if len(lib.QARecords) > 0 {
		recall.Notices = append(recall.Notices, "手动核对未分析对象归属，仅显示全局问答；商品或门店问答请通过“分析意图”核对。")
	}
	if lib.Validate != nil {
		if err = lib.Validate(ctx); err != nil {
			return RecallResult{}, err
		}
	}
	if err = s.check(ctx, request.Scope, BeforeReturn); err != nil {
		return RecallResult{}, err
	}
	return RecallResult{recall, qa}, nil
}
func (s Service) recall(ctx context.Context, scope Scope, lib Library, asks []danmakutest.Ask, queries []string, manual bool) (danmakutest.Recall, []qapair.Group, error) {
	if manual {
		for _, q := range queries {
			asks = append(asks, danmakutest.Ask{Text: q})
		}
	}
	empty := danmakutest.Recall{Groups: []danmakutest.Group{}, Hits: []danmakutest.Hit{}, Notices: []string{}}
	if len(lib.Records) == 0 && len(lib.QARecords) == 0 {
		empty.Notices = append(empty.Notices, "本方案资料库为空，尚无事实依据。")
		return empty, nil, ctx.Err()
	}
	if err := s.check(ctx, scope, BeforeEmbedding); err != nil {
		return empty, nil, err
	}
	if lib.Validate != nil {
		if err := lib.Validate(ctx); err != nil {
			return empty, nil, err
		}
	}
	if s.Embed == nil {
		return empty, nil, errors.New("语义召回：尚未配置可用的查询向量服务")
	}
	v, err := s.Embed(ctx, queries)
	if err != nil {
		return empty, nil, err
	}
	if err = ctx.Err(); err != nil {
		return empty, nil, err
	}
	if v.Model == "" || len(v.Vectors) != len(queries) {
		return empty, nil, errors.New("语义召回：查询向量返回不完整")
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	qa := qapair.Retrieve(lib.QARecords, lib.Links, lib.Stores, asks, queries, v.Vectors, v.Model, now)
	return danmakutest.Retrieve(lib.Records, lib.Links, lib.Stores, asks, queries, v.Vectors, v.Model, manual), qa, nil
}
func (s Service) modelJSON(ctx context.Context, scope Scope, stage Stage, input any, out any) (string, error) {
	point := BeforeIntent
	if stage == AnswerStage {
		point = BeforeAnswer
	}
	if err := s.check(ctx, scope, point); err != nil {
		return "", err
	}
	if s.Complete == nil || s.ReadPrompt == nil || s.DecodeJSON == nil {
		return "", errors.New("回答模型或解析适配器未配置")
	}
	prompt, err := s.ReadPrompt(stage)
	if err != nil {
		return "", errors.New("读取回答规则失败")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	if err = s.check(ctx, scope, point); err != nil {
		return "", err
	}
	system := string(prompt)
	if stage == AnswerStage && s.AnswerSystemPrefix != "" {
		system = s.AnswerSystemPrefix + "\n\n" + system
	}
	messages := []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: string(raw)}}
	var result agentgateway.Response
	if target, ok := out.(*answerDecodeTarget); ok && s.CompleteAnswer != nil {
		result, err = s.CompleteAnswer(ctx, target.input.AnswerReasoningEffort, messages)
	} else {
		result, err = s.Complete(ctx, stage, messages)
	}
	if err != nil {
		return "", modelCallError{cause: err}
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if strings.EqualFold(result.FinishReason, "length") {
		return "", fmt.Errorf("%w：模型结果被截断，请缩短输入或减少批次人数", interactionplan.ErrInvalidResult)
	}
	if target, ok := out.(*answerDecodeTarget); ok {
		if s.ParseAnswer != nil {
			*target.reply, err = s.ParseAnswer(ctx, target.input, result)
			if err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				return "", fmt.Errorf("%w：回答未符合原互动输出契约，未自动重试", interactionplan.ErrInvalidResult)
			}
			return result.Text, nil
		}
		out = target.reply
	}
	if err = s.DecodeJSON(result.Text, out); err != nil {
		return "", fmt.Errorf("%w：模型返回结果无法解析", interactionplan.ErrInvalidResult)
	}
	return result.Text, nil
}
func stripJSONFence(raw string) string {
	v := strings.TrimSpace(raw)
	if strings.HasPrefix(v, "```") {
		v = strings.TrimPrefix(v, "```json")
		v = strings.TrimPrefix(v, "```JSON")
		v = strings.TrimPrefix(v, "```")
		v = strings.TrimSpace(v)
		v = strings.TrimSpace(strings.TrimSuffix(v, "```"))
	}
	return v
}
func formattedIntent(raw string) string {
	var b bytes.Buffer
	if json.Indent(&b, []byte(stripJSONFence(raw)), "", "  ") == nil {
		return b.String()
	}
	return raw
}

// Preserve old empty-history payloads byte-for-byte in meaning. Histories are
// attached only to their owner's request, never synthesized from another item.
func conversation(input Input, payload map[string]any) map[string]any {
	if input.CurrentMainline != "" {
		payload["current_mainline"] = input.CurrentMainline
	}
	if len(input.Recent) > 0 {
		payload["recent"] = input.Recent
	}
	if len(input.Context) > 0 {
		payload["context"] = input.Context
	}
	if input.Original != "" {
		payload["original"] = input.Original
	}
	if input.Clarification != "" {
		payload["clarification"] = input.Clarification
	}
	if len(input.Recent) > 0 || len(input.Context) > 0 || input.Original != "" || input.Clarification != "" || input.CurrentMainline != "" {
		payload["history_policy"] = historyPolicy
	}
	return payload
}

const historyPolicy = "历史只属于当前观众，主线只作为现场背景，用于指代、省略和连续追问；当前问题优先，历史回答和主线不是当前商品事实依据。"
