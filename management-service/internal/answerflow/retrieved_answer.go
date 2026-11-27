package answerflow

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/interactionplan"
)

type AttributeConcept struct {
	Hash   string
	Model  string
	Vector []float32
}

type RetrievalConfig struct {
	GlobalName       string
	AttributeAliases map[string]string
	Concepts         map[string]AttributeConcept
	Rerank           func(context.Context, string, []string) ([]float64, error)
}

type retrievedCondition struct {
	Field  string   `json:"field"`
	Value  string   `json:"value"`
	Scope  string   `json:"scope"`
	AskIDs []string `json:"ask_ids,omitempty"`
}

type retrievedAsk struct {
	ID             string   `json:"id"`
	Text           string   `json:"text"`
	Query          string   `json:"query"`
	Subjects       []string `json:"subjects"`
	Attribute      string   `json:"attribute,omitempty"`
	Attributes     []string `json:"attributes"`
	AttributeScope string   `json:"attribute_scope"`
	CompareGroup   string   `json:"compare_group,omitempty"`
	Region         string   `json:"region,omitempty"`
	Notes          []string `json:"notes,omitempty"`
	FactIDs        []int64  `json:"fact_ids,omitempty"`
	Reranked       bool     `json:"reranked,omitempty"`
	Unresolved     bool     `json:"-"`
}

type retrievedIntent struct {
	Meaning       string               `json:"meaning"`
	Category      string               `json:"category"`
	SpeechAct     string               `json:"speech_act"`
	Uncertainties []string             `json:"uncertainties"`
	Conditions    []retrievedCondition `json:"conditions"`
	Asks          []retrievedAsk       `json:"asks"`
}

type RetrievalTrace struct {
	CorrectedQuestion   string           `json:"corrected_question"`
	Edits               []correctionEdit `json:"edits,omitempty"`
	Asks                []retrievedAsk   `json:"asks"`
	FactCount           int              `json:"fact_count"`
	EstimatedFactTokens int              `json:"estimated_fact_tokens"`
	FactBudgetTrimmed   bool             `json:"fact_budget_trimmed,omitempty"`
	RecallElapsedMS     int64            `json:"recall_elapsed_ms"`
}

type RetrievedResult struct {
	Result
	Retrieval RetrievalTrace `json:"retrieval"`
}

// AnalyzeRetrieved runs the evidence-based, single-question workflow.
func (s Service) AnalyzeRetrieved(ctx context.Context, request Request, config RetrievalConfig) (RetrievedResult, error) {
	if err := s.validateInput(&request.Input, false); err != nil {
		return RetrievedResult{}, err
	}
	lib, err := s.load(ctx, request, false)
	if err != nil {
		return RetrievedResult{}, err
	}
	return s.analyzeRetrieved(ctx, request.Scope, request.Input, lib, config)
}

// The live batch already validated the input and loaded its plan library.
func (s Service) analyzeRetrieved(ctx context.Context, scope Scope, input Input, lib Library, config RetrievalConfig) (RetrievedResult, error) {
	out := RetrievedResult{Result: Result{Question: input.Question, ContextWords: matchedWords(lib, input)}}
	retrieval := newFactRetrieval(lib, config)
	corrected, edits := correctSearchText(input.Question, retrieval.terms)
	out.Retrieval.CorrectedQuestion, out.Retrieval.Edits = corrected, edits

	searchInput := input
	searchInput.Question = corrected
	payload := intentPayload(lib, searchInput, out.ContextWords)
	payload["objects"] = retrieval.objects
	payload["attributes"] = retrieval.attributes
	history := make([]map[string]string, 0, len(input.Context)+len(input.Recent)*2)
	for _, turn := range input.Recent {
		history = append(history, map[string]string{"role": "user", "text": turn.Question},
			map[string]string{"role": "assistant", "text": turn.Answer})
	}
	for _, message := range input.Context {
		history = append(history, map[string]string{"role": message.Role, "text": message.Text})
	}
	if len(history) > 0 {
		payload["history"] = history
		delete(payload, "context")
		delete(payload, "recent")
	}

	var intent retrievedIntent
	raw, err := s.modelJSON(ctx, scope, IntentStage, payload, &intent)
	if errors.Is(err, interactionplan.ErrInvalidResult) {
		intent = retrievedIntent{}
		raw, err = s.modelJSON(ctx, scope, IntentStage, payload, &intent)
	}
	if err != nil {
		return out, fmt.Errorf("意图分析：%w", err)
	}
	if err := retrieval.normalizeIntent(&intent, searchInput); err != nil {
		return out, err
	}
	out.IntentModelJSON = formattedIntent(raw)
	out.Intent = danmakutest.Intent{
		Meaning: intent.Meaning, Category: intent.Category, SpeechAct: intent.SpeechAct,
		Uncertainties: intent.Uncertainties,
	}
	if corrected != input.Question {
		out.Intent.Corrected = corrected
	}

	facts, err := s.recallForAnswer(ctx, scope, retrieval, &intent, &out)
	if err != nil {
		return out, err
	}
	qaAsks := make([]danmakutest.Ask, 0, len(out.Intent.Asks))
	for i, group := range out.Recall.Groups {
		if !intent.Asks[i].Unresolved {
			qaAsks = append(qaAsks, group.Ask)
		}
	}
	qa, err := s.recallAnswerQA(ctx, scope, lib, qaAsks)
	if err != nil {
		return out, err
	}
	var references []answerQA
	out.MatchedQA, references, _ = answerQAReferences(qa, s.ValidateQA)

	if lib.Validate != nil {
		if err := lib.Validate(ctx); err != nil {
			return out, err
		}
	}
	customer := map[string]any{
		"speaker": "顾客", "source": "直播间弹幕", "original": input.Question,
		"meaning": intent.Meaning, "uncertainties": intent.Uncertainties,
	}
	if len(intent.Asks) > 0 {
		asks := make([]map[string]any, 0, len(intent.Asks))
		for _, ask := range intent.Asks {
			asks = append(asks, map[string]any{"text": ask.Text, "subjects": ask.Subjects,
				"attributes": ask.Attributes, "compare_group": ask.CompareGroup, "region": ask.Region})
		}
		customer["asks"] = asks
	}
	packet := conversation(input, map[string]any{
		"facts": facts, "qa_pairs": references, "room_intro": input.RoomIntro,
		"context_words": out.ContextWords, "customer_question": customer,
	})
	_, err = s.modelJSON(ctx, scope, AnswerStage, packet, &answerDecodeTarget{input: input, reply: &out.Reply})
	if err != nil {
		return out, fmt.Errorf("回答生成：%w", err)
	}
	if lib.Validate != nil {
		if err := lib.Validate(ctx); err != nil {
			return out, err
		}
	}
	return out, s.check(ctx, scope, BeforeReturn)
}

// RetrievalRewriteStage is used only after a scoped search finds no evidence.
const RetrievalRewriteStage Stage = "recall_query"

func (s Service) stepBackRecall(ctx context.Context, scope Scope, r factRetrieval, ask retrievedAsk, vectors retrievalVectors) ([]rankedFact, error) {
	if len(r.facts) == 0 || vectors.model == "" {
		return nil, nil
	}
	var rewritten struct {
		Query string `json:"query"`
	}
	_, err := s.modelJSON(ctx, scope, RetrievalRewriteStage, map[string]any{
		"query": ask.Query, "original": ask.Text, "subjects": ask.Subjects, "attributes": ask.Attributes,
	}, &rewritten)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := strings.TrimSpace(rewritten.Query)
	if err != nil || query == "" || len([]rune(query)) > 1000 || query == ask.Query {
		return nil, nil
	}
	embeddings, err := s.embedRetrievalQueries(ctx, scope, []string{query})
	if err != nil || embeddings.Model != vectors.model {
		return nil, err
	}
	ask.Query = query
	vectors.byText[query] = embeddings.Vectors[0]
	// Query rewriting never bypasses the object, requested attribute or region.
	return r.candidates(ask, vectors, false), nil
}

func (r factRetrieval) normalizeIntent(intent *retrievedIntent, input Input) error {
	intent.Meaning = strings.TrimSpace(intent.Meaning)
	if intent.Meaning == "" || len([]rune(intent.Meaning)) > 1600 || len(intent.Asks) > 8 || len(intent.Conditions) > 16 {
		return errors.New("意图结果不完整或诉求过多，请分批测试")
	}
	for i := range intent.Asks {
		intent.Asks[i].Region = ""
		intent.Asks[i].Reranked = false
		intent.Asks[i].Notes, intent.Asks[i].FactIDs = nil, nil
	}
	r.ensureComparisonObjects(intent, input.Question)
	seen := map[string]bool{}
	for i := range intent.Asks {
		ask := &intent.Asks[i]
		ask.Text = strings.TrimSpace(ask.Text)
		if ask.Text == "" || !strings.Contains(input.Question, ask.Text) {
			ask.Text = input.Question
		}
		ask.Query = strings.TrimSpace(ask.Query)
		if ask.Query == "" || len([]rune(ask.Query)) > 1000 {
			ask.Query = ask.Text
		}
		if ask.ID == "" {
			ask.ID = fmt.Sprintf("a%d", i+1)
		}
		if seen[ask.ID] {
			return errors.New("意图结果包含重复诉求编号")
		}
		seen[ask.ID] = true
		if len(ask.Subjects) > 1 {
			return errors.New("单条诉求对象不唯一，请重新试答")
		}
		for i, name := range ask.Subjects {
			ask.Subjects[i] = r.normalizeSubject(name)
		}
		if len(ask.Subjects) == 1 && ask.Subjects[0] == "" {
			ask.Subjects = nil
			ask.Unresolved = true
			ask.Notes = append(ask.Notes, "未确认所指商品或门店")
		}
		if len(ask.Attributes) == 0 && ask.Attribute != "" {
			ask.Attributes = []string{ask.Attribute}
		}
		ask.Attributes = uniqueStrings(ask.Attributes)
		if len(ask.Attributes) > 3 {
			return errors.New("单条诉求属性超过三个，请重新试答")
		}
		if len(ask.Attributes) == 0 ||
			((ask.AttributeScope == "全部" || ask.CompareGroup != "" || isOpenAsk(ask.Text)) && !r.attributeNamed(ask.Text, ask.Attributes)) {
			ask.Attributes = nil
			ask.AttributeScope = "全部"
		} else {
			ask.AttributeScope = "明确"
		}
		regions := []string{}
		for _, condition := range intent.Conditions {
			if condition.Field != "地区" && condition.Field != "region" || condition.Scope == "ask" && !slices.Contains(condition.AskIDs, ask.ID) {
				continue
			}
			region := strings.TrimSpace(condition.Value)
			if region != "" && danmakutest.IsRegion(region) && viewerMentioned(input, region) {
				regions = append(regions, region)
			}
		}
		switch regions = uniqueStrings(regions); len(regions) {
		case 1:
			ask.Region = regions[0]
		case 0:
			ask.Region = danmakutest.ViewerRegion(ask.Text)
		}
	}
	return nil
}

var mentionedLinkNumber = regexp.MustCompile("[0-9]+")

func (r factRetrieval) ensureComparisonObjects(intent *retrievedIntent, question string) {
	// Preserve the explicit sides of a comparison even if the model returns
	// one side. A number counts only next to a link marker or a list separator.
	labeled := []string{}
	indices := mentionedLinkNumber.FindAllStringIndex(question, -1)
	for _, idx := range indices {
		prefix, suffix := question[:idx[0]], question[idx[1]:]
		if strings.HasSuffix(prefix, ".") || strings.HasPrefix(suffix, ".") ||
			strings.HasPrefix(suffix, "块") || strings.HasPrefix(suffix, "元") || strings.HasPrefix(suffix, "斤") ||
			strings.HasPrefix(suffix, "件") || strings.HasPrefix(suffix, "天") || strings.HasPrefix(suffix, "月") {
			continue
		}
		if !strings.HasPrefix(suffix, "号") && !strings.HasPrefix(suffix, "链接") &&
			!strings.HasPrefix(suffix, "和") && !strings.HasPrefix(suffix, "与") &&
			!strings.HasSuffix(prefix, "和") && !strings.HasSuffix(prefix, "与") &&
			!strings.HasSuffix(prefix, "、") && !strings.HasPrefix(suffix, "、") {
			continue
		}
		if name := r.normalizeSubject(question[idx[0]:idx[1]]); name != "" {
			labeled = append(labeled, name)
		}
	}
	labeled = uniqueStrings(labeled)
	if len(labeled) < 2 {
		return
	}
	known := map[string]bool{}
	groups := map[string]bool{}
	var first retrievedAsk
	for _, ask := range intent.Asks {
		if ask.CompareGroup != "" {
			groups[ask.CompareGroup] = true
			if first.CompareGroup == "" {
				first = ask
			}
		}
		for _, subject := range ask.Subjects {
			known[r.normalizeSubject(subject)] = true
		}
	}
	if len(groups) != 1 {
		return
	}
	for i, name := range labeled {
		if known[name] || len(intent.Asks) >= 8 {
			continue
		}
		ask := first
		ask.ID = fmt.Sprintf("explicit-subject-%d", i+1)
		ask.Subjects = []string{name}
		ask.Query = name + " " + first.Text
		ask.Notes = []string{"已补齐原话中的对比对象"}
		intent.Asks = append(intent.Asks, ask)
	}
}

func viewerMentioned(input Input, value string) bool {
	if strings.Contains(input.Question, value) {
		return true
	}
	for _, message := range input.Context {
		if message.Role == "user" && strings.Contains(message.Text, value) {
			return true
		}
	}
	for _, turn := range input.Recent {
		if strings.Contains(turn.Question, value) {
			return true
		}
	}
	return false
}

func isOpenAsk(text string) bool {
	for _, word := range []string{"怎么样", "咋样", "好不好", "如何", "介绍", "有什么区别", "有啥区别", "值不值", "推荐"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func uniqueStrings(items []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range items {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out
}
