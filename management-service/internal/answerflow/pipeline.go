package answerflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/danmakutest"
)

// factGroup is the wire shape of one object's facts. It is a struct so the
// key order is fixed and identical for every request on the same revision.
type factGroup struct {
	Scope string   `json:"scope"`
	Facts []string `json:"facts"`
}

// modelFacts projects the stored fact card into the request shape: one entry
// per object, carrying that object's original sentences. Group order follows
// the card, so the same revision always produces byte-identical input.
func modelFacts(value any) []factGroup {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var groups []struct {
		Object string `json:"object"`
		Facts  []struct {
			Text string `json:"text"`
		} `json:"facts"`
	}
	if json.Unmarshal(raw, &groups) != nil {
		return nil
	}
	out := []factGroup{}
	for _, group := range groups {
		scope := strings.TrimSpace(group.Object)
		if scope == "" {
			continue
		}
		texts := []string{}
		seen := map[string]bool{}
		for _, fact := range group.Facts {
			text := strings.TrimSpace(fact.Text)
			if text == "" || seen[text] {
				continue
			}
			seen[text] = true
			texts = append(texts, text)
		}
		if len(texts) == 0 {
			continue
		}
		out = append(out, factGroup{Scope: scope, Facts: texts})
	}
	return out
}

func (s Service) analyze(ctx context.Context, scope Scope, input Input, lib Library) (Result, error) {
	out := Result{Question: input.Question, ContextWords: matchedWords(lib, input)}
	raw, err := s.modelJSON(ctx, scope, IntentStage, intentPayload(lib, input, out.ContextWords), &out.Intent)
	if err != nil {
		return out, fmt.Errorf("意图分析：%w", err)
	}
	out.IntentModelJSON = formattedIntent(raw)
	out.Intent, err = danmakutest.NormalizeIntent(out.Intent)
	if err != nil {
		return out, err
	}
	return s.finishIntent(ctx, scope, input, lib, out)
}

func originalQuestionClauses(question string) []string {
	parts := strings.FieldsFunc(question, func(r rune) bool {
		switch r {
		case '，', ',', '。', '！', '!', '？', '?', '；', ';', '\n':
			return true
		}
		return false
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func factObjectLabel(name string, lib Library) (string, string) {
	name = strings.TrimSpace(name)
	if name == "全局" || name == "" {
		return "全店通用", "不区分链接，适用于本直播间所有商品"
	}
	if strings.Contains(name, "、") {
		return name + "共用", "同时适用于" + name
	}
	for _, store := range lib.Stores {
		if strings.TrimSpace(store.Name) == name {
			return "线下实体门店", "线下实体门店信息，不是线上链接"
		}
	}
	return name, "仅适用于" + name
}

func compactFactEntries(hits []danmakutest.Hit) []map[string]any {
	index := map[string]int{}
	entries := []map[string]any{}
	for _, hit := range hits {
		text := strings.TrimSpace(hit.Original)
		if text == "" {
			text = strings.TrimSpace(hit.Content)
		}
		if text == "" {
			continue
		}
		if hit.Conditions != "" && !strings.Contains(text, hit.Conditions) {
			text += "；条件：" + hit.Conditions
		}
		if hit.Exceptions != "" && !strings.Contains(text, hit.Exceptions) {
			text += "；例外：" + hit.Exceptions
		}
		if i, ok := index[text]; ok {
			ids := entries[i]["ids"].([]int64)
			ids = append(ids, hit.ID)
			sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
			entries[i]["ids"] = ids
			continue
		}
		index[text] = len(entries)
		entries = append(entries, map[string]any{"ids": []int64{hit.ID}, "text": text})
	}
	return entries
}

// answerRequest fixes the wire order: the fact pack first, so it is a stable
// cacheable prefix, and the customer question last, so it stays the freshest
// instruction the model reads.
type answerRequest struct {
	Facts            []factGroup    `json:"facts"`
	QAPairs          []answerQA     `json:"qa_pairs,omitempty"`
	RoomIntro        string         `json:"room_intro"`
	ContextWords     any            `json:"context_words"`
	CurrentMainline  string         `json:"current_mainline,omitempty"`
	Recent           []Turn         `json:"recent,omitempty"`
	Context          []Message      `json:"context,omitempty"`
	Original         string         `json:"original,omitempty"`
	Clarification    string         `json:"clarification,omitempty"`
	HistoryPolicy    string         `json:"history_policy,omitempty"`
	CustomerQuestion map[string]any `json:"customer_question"`
}

func (s Service) answer(ctx context.Context, scope Scope, input Input, lib Library, out Result, qa []answerQA) (Result, error) {
	var err error
	factGroups, allHits := danmakutest.AllFactsByObject(lib.Records, lib.Links, lib.Stores)
	out.Recall = danmakutest.Recall{Groups: factGroups, Hits: allHits, Notices: []string{"使用已生成事实卡。"}}
	var factsByObject any
	if lib.FactCard != nil {
		factsByObject = lib.FactCard["facts_by_object"]
	}
	if factsByObject == nil {
		fallback := []map[string]any{}
		for _, group := range factGroups {
			object, meaning := factObjectLabel(group.Ask.Text, lib)
			fallback = append(fallback, map[string]any{"object": object, "meaning": meaning, "facts": compactFactEntries(group.Hits)})
		}
		factsByObject = fallback
	}
	customerAsks := []map[string]string{}
	for _, ask := range out.Intent.Asks {
		if text := strings.TrimSpace(ask.Text); text != "" {
			customerAsks = append(customerAsks, map[string]string{"text": text})
		}
	}
	customerQuestion := map[string]any{
		"speaker":       "顾客",
		"source":        "直播间弹幕",
		"original":      input.Question,
		"meaning":       out.Intent.Meaning,
		"uncertainties": out.Intent.Uncertainties,
	}
	if len(customerAsks) > 1 {
		customerQuestion["asks"] = customerAsks
	}
	request := answerRequest{
		Facts:            modelFacts(factsByObject),
		QAPairs:          qa,
		RoomIntro:        input.RoomIntro,
		ContextWords:     out.ContextWords,
		CurrentMainline:  input.CurrentMainline,
		Recent:           input.Recent,
		Context:          input.Context,
		Original:         input.Original,
		Clarification:    input.Clarification,
		CustomerQuestion: customerQuestion,
	}
	if len(input.Recent) > 0 || len(input.Context) > 0 || input.Original != "" || input.Clarification != "" || input.CurrentMainline != "" {
		request.HistoryPolicy = historyPolicy
	}
	_, err = s.modelJSON(ctx, scope, AnswerStage, request, &answerDecodeTarget{input: input, reply: &out.Reply})
	if err != nil {
		return out, fmt.Errorf("回答生成：%w", err)
	}
	noSpeech := out.Action == "skip" && out.InteractionIntent != nil && !out.InteractionIntent.Relevant
	if !noSpeech && (out.Action != "answer" && out.Action != "clarify" && out.Action != "insufficient" || strings.TrimSpace(out.Answer) == "" || len([]rune(out.Answer)) > 2000) {
		return out, errors.New("回答结果不完整，请重试")
	}
	if err := s.check(ctx, scope, BeforeReturn); err != nil {
		return out, err
	}
	return out, nil
}
func answerFacts(hits []danmakutest.Hit) []map[string]any {
	facts := []map[string]any{}
	for _, h := range hits {
		scope := strings.TrimSpace(h.Scope)
		if n, err := strconv.ParseInt(scope, 10, 64); err == nil {
			scope = strconv.FormatInt(n, 10) + "号链接"
		}
		original := strings.TrimSpace(h.Original)
		if original == "" {
			original = strings.TrimSpace(h.Content)
		}
		text := original
		if text != "" && scope != "" && scope != "全局" && !strings.HasPrefix(text, scope) {
			text = scope + text
		}
		facts = append(facts, map[string]any{"key": h.Key, "attribute": h.Attribute, "topic": h.Attribute, "content": h.Content, "text": text, "scope": scope, "conditions": h.Conditions, "exceptions": h.Exceptions, "condition_status": h.ConditionStatus, "original": original, "question": h.Question})
	}
	return facts
}

func intentPayload(lib Library, input Input, words *contextword.Context) map[string]any {
	resolver := danmakutest.NewIntentAttributeResolver(lib.Links, lib.Stores, "", nil)
	return intentPayloadWithAttributes(lib, input, words, resolver.Directory(lib.Records))
}

func intentPayloadWithAttributes(lib Library, input Input, words *contextword.Context, directory danmakutest.IntentAttributeDirectory) map[string]any {
	stores := []string{}
	for _, s := range lib.Stores {
		stores = append(stores, s.Name)
	}
	return conversation(input, map[string]any{"question": input.Question, "room_intro": input.RoomIntro, "context_words": words, "objects": lib.Links, "stores": stores, "attributes": directory.Names, "attribute_catalog": directory.Entries})
}
