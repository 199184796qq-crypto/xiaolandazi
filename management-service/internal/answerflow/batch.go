package answerflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/danmakutest"
)

type Candidate struct {
	ID        string
	Input     Input
	ExpiresAt time.Time
}
type CandidateResult struct {
	ID       string
	Intent   danmakutest.Intent
	Priority string
	Reason   string
	Reply    *Result
}
type BatchResult struct {
	Selected int // -1 means no applicable, unexpired candidate.
	Items    []CandidateResult
}
type classified struct {
	ID       string             `json:"item_id"`
	Intent   danmakutest.Intent `json:"intent"`
	Relevant *bool              `json:"relevant"`
	Priority string             `json:"priority"`
	Reason   string             `json:"reason"`
}

type classificationResponse struct {
	Items []classified `json:"items"`
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// AnswerBatch loads one library. A singleton uses the ordinary full pipeline.
// Multiple viewers use one classification request and at most one answer;
// classification is never repeated for the selected viewer.
func (s Service) AnswerBatch(ctx context.Context, scope Scope, candidates []Candidate) (BatchResult, error) {
	return s.answerBatch(ctx, scope, candidates, nil)
}

// AnswerRetrievedBatch keeps the live batch priority selector and runs the
// evidence-based workflow only for the candidate chosen for playback.
func (s Service) AnswerRetrievedBatch(ctx context.Context, scope Scope, candidates []Candidate, retrieval RetrievalConfig) (BatchResult, error) {
	return s.answerBatch(ctx, scope, candidates, &retrieval)
}

func (s Service) answerBatch(ctx context.Context, scope Scope, candidates []Candidate, retrieval *RetrievalConfig) (BatchResult, error) {
	candidates = append([]Candidate(nil), candidates...)
	out := BatchResult{Selected: -1, Items: make([]CandidateResult, len(candidates))}
	if len(candidates) < 1 || len(candidates) > 10 {
		return out, errors.New("回答批次须包含1至10人")
	}
	for i, c := range candidates {
		if c.ID != fmt.Sprintf("U%02d", i+1) {
			return out, errors.New("回答批次编号无效")
		}
		if err := s.validateInput(&candidates[i].Input, false); err != nil {
			return out, err
		}
		if candidates[i].Input.RoomIntro != candidates[0].Input.RoomIntro || candidates[i].Input.CurrentMainline != candidates[0].Input.CurrentMainline {
			return out, errors.New("回答批次简介不一致")
		}
		out.Items[i].ID = c.ID
	}
	lib, err := s.load(ctx, Request{Scope: scope, Input: candidates[0].Input}, false)
	if err != nil {
		return out, err
	}
	if len(candidates) == 1 {
		if !candidates[0].ExpiresAt.IsZero() && !s.now().Before(candidates[0].ExpiresAt) {
			return out, context.DeadlineExceeded
		}
		callCtx, cancel := candidateContext(ctx, candidates[0].ExpiresAt)
		defer cancel()
		r, err := s.answerSelected(callCtx, scope, candidates[0].Input, lib, retrieval, nil)
		if err != nil {
			return out, err
		}
		out.Selected = 0
		out.Items[0].Intent = r.Intent
		out.Items[0].Reply = &r
		out.Items[0].Priority = "normal"
		return out, nil
	}
	items := make([]map[string]any, len(candidates))
	for i, c := range candidates {
		items[i] = map[string]any{"item_id": c.ID, "input": batchItemPayload(lib, c.Input)}
	}
	var response classificationResponse
	_, err = s.modelJSON(ctx, scope, BatchIntentStage, batchIntentPayload(lib, candidates[0].Input, items), &response)
	if err != nil {
		return out, err
	}
	if len(response.Items) != len(candidates) {
		return out, errors.New("批量意图返回人数不完整，未购买回答")
	}
	byID := map[string]classified{}
	for _, item := range response.Items {
		if _, exists := byID[item.ID]; exists {
			return out, errors.New("批量意图编号重复，未购买回答")
		}
		byID[item.ID] = item
	}
	ranks := map[string]int{"low": 1, "normal": 2, "high": 3}
	best := 0
	for i, c := range candidates {
		item, ok := byID[c.ID]
		if !ok || item.Relevant == nil || ranks[item.Priority] == 0 || len([]rune(item.Reason)) > 500 || item.Priority == "high" && strings.TrimSpace(item.Reason) == "" {
			return out, errors.New("批量意图身份或优先级不完整，未购买回答")
		}
		intent, err := danmakutest.NormalizeIntent(item.Intent)
		if err != nil {
			return out, err
		}
		out.Items[i].Intent = intent
		out.Items[i].Priority = item.Priority
		out.Items[i].Reason = item.Reason
		if !*item.Relevant || (!c.ExpiresAt.IsZero() && !s.now().Before(c.ExpiresAt)) {
			continue
		}
		if ranks[item.Priority] > best {
			best = ranks[item.Priority]
			out.Selected = i
		}
	}
	if out.Selected < 0 {
		return out, s.check(ctx, scope, BeforeReturn)
	}
	selected := candidates[out.Selected]
	callCtx, cancel := candidateContext(ctx, selected.ExpiresAt)
	defer cancel()
	// Full batch JSON stays in the adapter's model receipt, never in one
	// viewer's response. Only their normalized intent enters the answer call.
	r, err := s.answerSelected(callCtx, scope, selected.Input, lib, retrieval, &out.Items[out.Selected].Intent)
	if err != nil {
		return out, err
	}
	if retrieval != nil {
		out.Items[out.Selected].Intent = r.Intent
	}
	out.Items[out.Selected].Reply = &r
	return out, nil
}

func (s Service) answerSelected(ctx context.Context, scope Scope, input Input, lib Library, retrieval *RetrievalConfig, ranked *danmakutest.Intent) (Result, error) {
	if retrieval != nil {
		out, err := s.analyzeRetrieved(ctx, scope, input, lib, *retrieval)
		return out.Result, err
	}
	if ranked == nil {
		return s.analyze(ctx, scope, input, lib)
	}
	r := Result{Question: input.Question, Intent: *ranked, ContextWords: matchedWords(lib, input)}
	return s.finishIntent(ctx, scope, input, lib, r)
}

func batchItemPayload(lib Library, input Input) map[string]any {
	// Shared catalogs and introduction are transmitted once. Histories stay
	// under the viewer whose input they belong to.
	payload := intentPayload(lib, input, matchedWords(lib, input))
	for _, key := range []string{"objects", "stores", "attributes", "attribute_catalog", "room_intro", "current_mainline"} {
		delete(payload, key)
	}
	return payload
}

func batchIntentPayload(lib Library, input Input, items []map[string]any) map[string]any {
	shared := intentPayload(lib, Input{RoomIntro: input.RoomIntro, CurrentMainline: input.CurrentMainline}, nil)
	delete(shared, "question")
	delete(shared, "context_words")
	shared["items"] = items
	return shared
}

func candidateContext(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	if deadline.IsZero() {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, deadline)
}
