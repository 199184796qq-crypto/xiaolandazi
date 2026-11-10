package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
	"time"
)

type danmakuInput struct {
	Question  string   `json:"question"`
	RoomIntro string   `json:"room_intro"`
	Queries   []string `json:"queries"`
}
type danmakuAnswer struct {
	Action string `json:"action"`
	Answer string `json:"answer"`
}
type danmakuResult struct {
	Question     string               `json:"question"`
	Intent       danmakutest.Intent   `json:"intent"`
	ContextWords *contextword.Context `json:"context_words,omitempty"`
	Recall       danmakutest.Recall   `json:"recall"`
	danmakuAnswer
}
type danmakuEmbedder interface {
	Enabled() bool
	Model() string
	EmbedTexts(context.Context, []string) ([][]float32, error)
}
type danmakuComplete func(context.Context, string, []agentgateway.Message) (agentgateway.Response, error)
type danmakuLibrary struct {
	Records []danmakutest.Record
	Links   []danmakutest.Link
	Stores  []model.MaterialStoreObject
	Words   []contextword.Entry
}

func (s *Server) loadDanmakuLibrary(r *http.Request, tenant, plan int64) (danmakuLibrary, error) {
	var out danmakuLibrary
	records, err := s.store.PlanMaterialRecallSnapshot(r.Context(), tenant, plan)
	if err != nil {
		return out, err
	}
	links, err := s.materialLinkObjects(r, tenant, plan)
	if err != nil {
		return out, err
	}
	objects, err := s.store.PlanMaterialObjects(r.Context(), tenant, plan)
	if err != nil {
		return out, err
	}
	words, err := s.store.PlanContextWords(r.Context(), tenant, plan)
	if err != nil {
		return out, err
	}
	for _, r := range records {
		out.Records = append(out.Records, danmakutest.Record{Claim: r.Claim, Vector: r.Vector})
	}
	for _, l := range links {
		out.Links = append(out.Links, danmakutest.Link{ID: l.ID, Name: l.LinkKey, Product: l.ProductName})
	}
	out.Stores = objects.Stores
	out.Words = words.Items
	return out, nil
}
func validateDanmakuInput(in *danmakuInput, manual bool) error {
	in.Question = strings.TrimSpace(in.Question)
	in.RoomIntro = strings.TrimSpace(in.RoomIntro)
	if manual {
		if len(in.Queries) == 0 || len(in.Queries) > 10 {
			return errors.New("请输入1至10条查询，每行一条")
		}
		for _, q := range in.Queries {
			if strings.TrimSpace(q) == "" || len([]rune(q)) > 1000 {
				return errors.New("每条查询限1至1000字")
			}
		}
		return nil
	}
	if in.Question == "" || len([]rune(in.Question)) > 1000 || len([]rune(in.RoomIntro)) > 2000 {
		return errors.New("弹幕限1至1000字，直播间介绍限2000字")
	}
	return nil
}
func (s *Server) planDanmakuTest(w http.ResponseWriter, r *http.Request) {
	_, plan, tenant, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	manual := strings.HasSuffix(r.URL.Path, "/recall")
	var input danmakuInput
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	if err := validateDanmakuInput(&input, manual); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	library, err := s.loadDanmakuLibrary(r, tenant, plan)
	if err != nil {
		writeError(w, 500, "读取本方案资料、对象或语境词失败，本次尚未调用模型")
		return
	}
	embedder, _ := s.semanticMetrics.(danmakuEmbedder)
	if manual {
		recall, err := recallDanmaku(ctx, library, nil, input.Queries, embedder, true)
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, recall)
		return
	}
	complete := func(ctx context.Context, stage string, messages []agentgateway.Message) (agentgateway.Response, error) {
		result, _, err := s.completeAnchorStyleWorkbench(ctx, "", stage, agentgateway.ResponseJSON, messages, 4000, false)
		return result, err
	}
	out, err := runDanmakuTest(ctx, input, library, embedder, complete)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, out)
}
func danmakuModelJSON(ctx context.Context, stage, mother string, input any, complete danmakuComplete, out any) error {
	prompt, err := interactionpolicy.Read(mother)
	if err != nil {
		return errors.New("读取弹幕测试规则失败")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	result, err := complete(ctx, stage, []agentgateway.Message{{Role: "system", Content: string(prompt)}, {Role: "user", Content: string(raw)}})
	if err != nil {
		return errors.New("模型调用失败，请重试；本次未进入播放队列")
	}
	if strings.EqualFold(result.FinishReason, "length") {
		return errors.New("模型结果被截断，请缩短弹幕或分批测试")
	}
	value, err := parseLooseJSONValue(result.Text)
	if err != nil {
		return errors.New("模型返回结果不完整，请重试")
	}
	raw, err = json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func runDanmakuTest(ctx context.Context, input danmakuInput, lib danmakuLibrary, embedder danmakuEmbedder, complete danmakuComplete) (danmakuResult, error) {
	out := danmakuResult{Question: input.Question, ContextWords: contextword.Match(lib.Words, input.Question)}
	attributes := []string{}
	seen := map[string]bool{}
	for _, r := range lib.Records {
		c := r.Claim
		attribute := danmakutest.AttributeName(c.Topic, lib.Links, lib.Stores)
		if (c.Destination == "" || c.Destination == "fact" || c.Destination == "qa") && attribute != "" && !seen[attribute] {
			seen[attribute] = true
			attributes = append(attributes, attribute)
		}
	}
	// Catalogs contain human names only. Internal IDs and relationships are
	// program-owned; the model never maintains claim IDs or vector/index keys.
	if len(attributes) > 150 {
		attributes = attributes[:150]
	}
	stores := []string{}
	for _, s := range lib.Stores {
		stores = append(stores, s.Name)
	}
	err := danmakuModelJSON(ctx, "danmaku_test_intent", "danmaku-test-intent.md", map[string]any{"question": input.Question, "room_intro": input.RoomIntro, "context_words": out.ContextWords, "objects": lib.Links, "stores": stores, "attributes": attributes}, complete, &out.Intent)
	if err != nil {
		return out, errors.New("意图分析：" + err.Error())
	}
	out.Intent, err = danmakutest.NormalizeIntent(out.Intent)
	if err != nil {
		return out, err
	}
	queries := danmakutest.Queries(input.Question, out.Intent.Asks)
	out.Recall, err = recallDanmaku(ctx, lib, out.Intent.Asks, queries, embedder, false)
	if err != nil {
		return out, err
	}
	// Do not send internal row IDs or numerical rankings to the answer model.
	groups := []map[string]any{}
	for _, g := range out.Recall.Groups {
		facts := []map[string]any{}
		focused, resolved := danmakutest.FocusFacts(g, lib.Links, lib.Stores)
		for _, h := range focused {
			if h.ConditionStatus == "不适用" {
				continue
			}
			facts = append(facts, map[string]any{"attribute": h.Attribute, "content": h.Content, "scope": h.Scope, "conditions": h.Conditions, "exceptions": h.Exceptions, "condition_status": h.ConditionStatus, "original": h.Original, "question": h.Question})
		}
		groups = append(groups, map[string]any{"ask": g.Ask, "facts": facts, "focus_resolved": resolved, "notice": g.Notice})
	}
	err = danmakuModelJSON(ctx, "danmaku_test_answer", "danmaku-test-answer.md", map[string]any{"question": input.Question, "room_intro": input.RoomIntro, "intent": out.Intent, "context_words": out.ContextWords, "asks": groups, "recall_notices": out.Recall.Notices}, complete, &out.danmakuAnswer)
	if err != nil {
		return out, errors.New("回答生成：" + err.Error())
	}
	if out.Action != "answer" && out.Action != "clarify" && out.Action != "insufficient" || strings.TrimSpace(out.Answer) == "" || len([]rune(out.Answer)) > 2000 {
		return out, errors.New("回答结果不完整，请重试")
	}
	return out, nil
}
func recallDanmaku(ctx context.Context, lib danmakuLibrary, asks []danmakutest.Ask, queries []string, e danmakuEmbedder, manual bool) (danmakutest.Recall, error) {
	if manual {
		for _, q := range queries {
			asks = append(asks, danmakutest.Ask{Text: q})
		}
	}
	empty := danmakutest.Recall{Groups: []danmakutest.Group{}, Hits: []danmakutest.Hit{}, Notices: []string{}}
	if len(lib.Records) == 0 {
		empty.Notices = append(empty.Notices, "本方案资料库为空，尚无事实依据。")
		return empty, nil
	}
	if e == nil || !e.Enabled() {
		return empty, errors.New("语义召回：尚未配置可用的查询向量服务")
	}
	vectors, err := embedMaterialTexts(ctx, e, queries)
	if err != nil {
		return empty, errors.New("语义召回：查询向量调用失败，不能确认召回结果")
	}
	if err = db.ValidateMaterialVectors(vectors, len(queries)); err != nil {
		return empty, errors.New("语义召回：查询向量返回不完整")
	}
	return danmakutest.Retrieve(lib.Records, lib.Links, lib.Stores, asks, queries, vectors, e.Model(), manual), nil
}
