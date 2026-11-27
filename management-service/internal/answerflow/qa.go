package answerflow

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"slices"

	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/qapair"
)

const (
	directQAScore = 0.85
	answerQAChars = 6000
)

type answerQAAsk struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

type answerQA struct {
	Question   string        `json:"question"`
	Answer     string        `json:"answer"`
	Conditions string        `json:"conditions,omitempty"`
	Exceptions string        `json:"exceptions,omitempty"`
	Asks       []answerQAAsk `json:"asks"`
}

func (s Service) recallAnswerQA(ctx context.Context, scope Scope, lib Library, asks []danmakutest.Ask) ([]qapair.Group, error) {
	now := s.now()
	if len(asks) == 0 || !slices.ContainsFunc(lib.QARecords, func(r qapair.Record) bool { return qapair.Active(r.Entry, now) }) {
		return nil, nil
	}
	if err := s.check(ctx, scope, BeforeEmbedding); err != nil {
		return nil, err
	}
	if s.Embed == nil {
		return nil, nil
	}
	queries := make([]string, 0, len(asks))
	indices := make(map[string]int, len(asks))
	for _, ask := range asks {
		if _, exists := indices[ask.Text]; !exists {
			indices[ask.Text] = len(queries)
			queries = append(queries, ask.Text)
		}
	}
	vectors, err := s.Embed(ctx, queries)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || vectors.Model == "" || len(vectors.Vectors) != len(queries) {
		log.Printf("qa recall unavailable tenant=%d plan=%d asks=%d", scope.Tenant, scope.Plan, len(asks))
		return nil, nil
	}
	now = s.now()
	groups := make([]qapair.Group, len(asks))
	for i, ask := range asks {
		groups[i] = qapair.Group{
			Ask: ask.Text, Matches: qapair.AnswerCandidates(lib.QARecords, ask, vectors.Vectors[indices[ask.Text]], vectors.Model, now, lib.Links, lib.Stores),
		}
	}
	return groups, nil
}

// Reserve candidates in rounds across asks so one ask cannot take the whole
// budget. Encode each stored answer once, with every matching ask and score.
func answerQAReferences(groups []qapair.Group, validate func(string) error) ([]qapair.Group, []answerQA, []string) {
	selected := make(map[string]bool)
	picked, budget := 0, 0
	for rank := 0; picked < qapair.AnswerLimit; rank++ {
		hasMore := false
		for _, group := range groups {
			if rank >= len(group.Matches) {
				continue
			}
			hasMore = true
			match := group.Matches[rank]
			if _, seen := selected[match.ID]; seen {
				continue
			}
			size := len([]rune(match.Question + match.Answer + match.Conditions + match.Exceptions))
			include := budget+size <= answerQAChars && (validate == nil || validate(match.Answer) == nil)
			selected[match.ID] = include
			if include {
				picked++
				budget += size
				if picked == qapair.AnswerLimit {
					break
				}
			}
		}
		if !hasMore {
			break
		}
	}
	var evidence []qapair.Group
	var references []answerQA
	var ids []string
	index := make(map[string]int, picked)
	for _, group := range groups {
		visible := qapair.Group{Ask: group.Ask}
		for _, match := range group.Matches {
			if !selected[match.ID] {
				continue
			}
			i, exists := index[match.ID]
			if !exists {
				i = len(references)
				index[match.ID] = i
				references = append(references, answerQA{
					Question: match.Question, Answer: match.Answer, Conditions: match.Conditions, Exceptions: match.Exceptions,
				})
				ids = append(ids, match.ID)
			}
			ask := answerQAAsk{Text: group.Ask, Score: math.Round(match.Score*10000) / 10000}
			if !slices.Contains(references[i].Asks, ask) {
				references[i].Asks = append(references[i].Asks, ask)
			}
			visible.Matches = append(visible.Matches, match)
		}
		if len(visible.Matches) > 0 {
			evidence = append(evidence, visible)
		}
	}
	return evidence, references, ids
}

func (s Service) finishIntent(ctx context.Context, scope Scope, input Input, lib Library, out Result) (Result, error) {
	matches, err := s.recallAnswerQA(ctx, scope, lib, out.Intent.Asks)
	if err != nil {
		return out, err
	}
	if len(matches) == 1 && len(matches[0].Matches) > 0 && len(out.Intent.Uncertainties) == 0 {
		top := matches[0].Matches[0]
		if top.Score > directQAScore && top.Conditions == "" && top.Exceptions == "" && s.ValidateQA != nil && s.ValidateQA(top.Answer) == nil {
			if lib.Validate != nil {
				if err := lib.Validate(ctx); err != nil {
					return out, err
				}
			}
			if err := s.check(ctx, scope, BeforeReturn); err != nil {
				return out, err
			}
			for _, record := range lib.QARecords {
				if record.Entry.ID == top.ID && record.Entry.Revision == top.Revision && qapair.Active(record.Entry, s.now()) {
					top.Status = "已采用"
					out.MatchedQA = []qapair.Group{{Ask: matches[0].Ask, Matches: []qapair.Match{top}}}
					out.Recall = danmakutest.Recall{Groups: []danmakutest.Group{}, Hits: []danmakutest.Hit{}}
					out.Reply = Reply{Action: "answer", Answer: top.Answer,
						AnswerParse: &interactionplan.ParseDiagnostics{Outcome: "accepted", Reason: "qa_pair_match"}}
					log.Printf("qa recall direct tenant=%d plan=%d qa_id=%s score=%.3f", scope.Tenant, scope.Plan, top.ID, top.Score)
					return out, nil
				}
			}
			matches = nil // The top entry expired while validating the snapshot.
		}
	}
	evidence, refs, ids := answerQAReferences(matches, s.ValidateQA)
	if len(refs) > 0 && lib.Validate != nil {
		if err := lib.Validate(ctx); err != nil {
			return out, err
		}
	}
	out.MatchedQA = evidence
	if len(matches) > 0 {
		var best float64
		for _, group := range matches {
			if len(group.Matches) > 0 {
				best = max(best, group.Matches[0].Score)
			}
		}
		var size int
		if len(refs) > 0 {
			raw, _ := json.Marshal(refs)
			size = len(raw)
		}
		log.Printf("qa recall tenant=%d plan=%d asks=%d hits=%d best=%.3f ids=%v bytes=%d", scope.Tenant, scope.Plan, len(matches), len(refs), best, ids, size)
	}
	out, err = s.answer(ctx, scope, input, lib, out, refs)
	if err == nil && len(refs) > 0 && lib.Validate != nil {
		err = lib.Validate(ctx)
		if err != nil {
			out.Reply = Reply{}
		}
	}
	return out, err
}
