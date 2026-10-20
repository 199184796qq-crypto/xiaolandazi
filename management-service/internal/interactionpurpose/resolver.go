package interactionpurpose

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/semantic"
)

const ContentType = "interaction_purpose"

type Result struct {
	Question   bool                  `json:"question"`
	Relevant   bool                  `json:"relevant"`
	Confidence float64               `json:"confidence"`
	PurposeID  string                `json:"purpose_id"`
	Purpose    string                `json:"purpose"`
	Scope      string                `json:"scope"`
	Angles     []contentstrategy.Row `json:"task_angles"`
	Topic      string                `json:"topic,omitempty"`
	Source     string                `json:"source,omitempty"`
	FactsSHA   string                `json:"facts_sha256,omitempty"`
	Input      string                `json:"input,omitempty"`
}
type Repository interface {
	GetInteractionPurpose(context.Context, int64, int64, int64, string) (string, error)
	UpsertSemanticDocument(context.Context, semantic.StoredDocument) error
	ListPurposeBuckets(context.Context, int64, int64, int64, int) ([]semantic.PurposeBucket, error)
	UpsertPurposeBucket(context.Context, int64, int64, int64, string, string, string) error
	GetPurposeBucketConclusion(context.Context, int64, int64, int64, string) (string, error)
}

type Complete func(context.Context, string) (string, error)
type Resolver struct {
	Repo     Repository
	Semantic *semantic.Service
	Complete Complete
	// Judge, when non-nil, classifies whether a new question is semantically the
	// same as one of the room's existing purpose buckets, so the resolution model
	// can be skipped for repeats. It consumes only question text (no fact bundle).
	Judge Complete
}
type Input struct {
	Tenant, Room, Plan  int64
	Text, Facts, Prompt string
	Purposes            []contentstrategy.Purpose
}

func Normalize(text string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}
func SourceID(input Input) string {
	return contentstrategy.SHA(input.Facts + input.Prompt)[:24] + ":" + contentstrategy.SHA(Normalize(input.Text))
}

func preciseFamily(text string) string {
	for _, family := range []struct {
		name  string
		parts []string
	}{
		{"运费", []string{"邮费", "运费", "包邮"}}, {"快递公司", []string{"什么快递", "哪个快递", "哪家快递", "快递公司", "发哪个快递"}},
		{"发货地", []string{"哪里发", "哪儿发", "哪发货", "发货地", "从哪发"}}, {"时效", []string{"几天", "多久", "好久", "什么时候发", "什么时候到"}},
		{"生熟", []string{"生油", "熟油", "生的还是熟", "熟的还是生"}}, {"优惠", []string{"优惠", "券", "折扣"}}, {"价值", []string{"贵不贵", "太贵", "贵吗", "划算"}},
		{"价格", []string{"多少钱", "多钱", "价格", "价钱", "几多钱"}},
	} {
		for _, part := range family.parts {
			if strings.Contains(text, part) {
				return family.name
			}
		}
	}
	return ""
}

func (r *Resolver) Resolve(ctx context.Context, input Input) (Result, error) {
	if input.Tenant <= 0 || input.Room <= 0 || input.Plan <= 0 || Normalize(input.Text) == "" {
		return Result{}, fmt.Errorf("目的解析作用域或原话缺失")
	}
	id := SourceID(input)
	factsSHA := contentstrategy.SHA(input.Facts)
	if raw, err := r.Repo.GetInteractionPurpose(ctx, input.Tenant, input.Room, input.Plan, id); err == nil && raw != "" {
		var cached Result
		if json.Unmarshal([]byte(raw), &cached) == nil && cached.FactsSHA == factsSHA && Normalize(cached.Input) == Normalize(input.Text) {
			cached.Source = "exact"
			return cached, nil
		}
	}
	// 判断模型层：拿新问题跟本房间的语义桶比对，命中同类就复用结论，省掉完整解意图。
	// 只喂问题原文（不带事实包），判断模型远比解意图便宜。宁可 miss 也不误命中。
	if r.Judge != nil {
		if result, ok := r.judgeReuse(ctx, input, factsSHA); ok {
			return result, nil
		}
	}
	if r.Semantic != nil && r.Semantic.Enabled() {
		matches, err := r.Semantic.Search(ctx, input.Text, semantic.Query{TenantID: input.Tenant, RoomID: input.Room, PlanID: input.Plan, ContentType: ContentType, Limit: 12, MinScore: .80})
		if err == nil {
			var best Result
			bestScore, competitor := 0.0, 0.0
			for _, match := range matches {
				var cached Result
				if json.Unmarshal([]byte(match.Document.Text), &cached) != nil || cached.FactsSHA != factsSHA || cached.Confidence < .85 || preciseFamily(input.Text) != preciseFamily(cached.Input) {
					continue
				}
				if bestScore == 0 {
					best, bestScore = cached, match.Score
				} else if cached.Topic != best.Topic && match.Score > competitor {
					competitor = match.Score
				}
			}
			if bestScore >= .94 && bestScore-competitor >= .08 {
				best.Input = input.Text
				best.Source = "semantic"
				r.save(ctx, input, id, best)
				return best, nil
			}
		}
	}
	payload, _ := json.Marshal(map[string]any{"观众原话": input.Text, "完整已发布事实": input.Facts, "事实编号": contentstrategy.Facts(input.Facts), "已有目的": input.Purposes})
	raw, err := r.Complete(ctx, string(payload))
	if err != nil {
		return Result{}, err
	}
	var result Result
	var fields map[string]json.RawMessage
	if err = contentstrategy.Decode(raw, &fields); err != nil {
		return Result{}, err
	}
	allowed := map[string]bool{"question": true, "relevant": true, "confidence": true, "purpose_id": true, "purpose": true, "scope": true, "task_angles": true}
	if len(fields) != len(allowed) {
		return Result{}, fmt.Errorf("目的返回字段缺失或多余")
	}
	for name, value := range fields {
		if !allowed[name] || strings.TrimSpace(string(value)) == "null" {
			return Result{}, fmt.Errorf("目的返回字段无效：%s", name)
		}
	}
	if err = contentstrategy.Decode(raw, &result); err != nil {
		return Result{}, fmt.Errorf("目的返回协议无效：%w", err)
	}
	if result.Confidence < 0 || result.Confidence > 1 || len([]rune(result.Purpose)) > 80 || len([]rune(result.Scope)) > 120 || len(result.Angles) > 3 {
		return Result{}, fmt.Errorf("目的返回字段越界")
	}
	known := false
	for _, p := range input.Purposes {
		if p.ID == result.PurposeID {
			known = true
		}
	}
	if result.PurposeID != "" && !known {
		return Result{}, fmt.Errorf("目的编号未来自当前方案")
	}
	if result.Question && result.Relevant && (result.Purpose == "" || result.Confidence < .75) {
		return Result{}, fmt.Errorf("问题目的不确定，保留确定性入口")
	}
	// relevant=false（超出事实包范围/与当前直播无关）时，目的编号没有意义：
	// 不该有 purpose_id，也不该触发 L 兜底生成假 ID。直接清空，让下游走「缺事实兜底」。
	if !result.Relevant {
		result.PurposeID = ""
		result.Purpose = ""
		result.Angles = nil
	}
	if result.PurposeID == "" && result.Purpose != "" {
		result.PurposeID = "L" + contentstrategy.SHA(result.Purpose)[:12]
	}
	for i := range result.Angles {
		result.Angles[i].PurposeID = result.PurposeID
	}
	if len(result.Angles) > 0 {
		// 解意图的 task_angles 是「轻量角度」，模型只输出「角度/讲述任务/关联事实」，
		// 不输出快照专属的「优先级/任务类型/适用条件」。这里只校验解意图真正要求的：
		//   1) 关联事实必须引用已存在的事实编号（防模型编造）
		//   2) 角度、讲述任务非空且不超长
		//   3) 采用条件（若给出）合法
		// 因此用 ValidateRowPolicy(required=false)（跳过优先级/任务类型/采用条件的非空要求），
		// 并单独校验关联事实引用，不再套用快照级 Validate（它强制适用条件/优先级等）。
		knownFacts := map[string]bool{}
		for _, f := range contentstrategy.Facts(input.Facts) {
			knownFacts[f.ID] = true
		}
		for _, row := range result.Angles {
			if err = contentstrategy.ValidateRowPolicy(row, false); err != nil {
				return Result{}, err
			}
			if err = contentstrategy.ValidateFactRefs(row.FactIDs, knownFacts); err != nil {
				return Result{}, err
			}
			if len([]rune(strings.TrimSpace(row.Angle))) == 0 || len([]rune(row.Angle)) > 60 ||
				len([]rune(strings.TrimSpace(row.Task))) == 0 || len([]rune(row.Task)) > 240 {
				return Result{}, fmt.Errorf("解意图角度字段缺失或过长")
			}
		}
	}
	result.FactsSHA, result.Input, result.Source = factsSHA, input.Text, "model"
	result.Topic = "PURPOSE:" + contentstrategy.SHA(result.PurposeID + "/" + preciseFamily(input.Text) + "/" + result.Scope)[:24]
	if result.Confidence >= .85 {
		if err = r.save(ctx, input, id, result); err != nil {
			return Result{}, fmt.Errorf("保存目的知识失败：%w", err)
		}
		r.storeBucket(ctx, input, result)
	}
	return result, nil
}
func (r *Resolver) save(ctx context.Context, input Input, id string, result Result) error {
	raw, _ := json.Marshal(result)
	doc := semantic.Document{TenantID: input.Tenant, RoomID: input.Room, PlanID: input.Plan, ContentType: ContentType, SourceID: id, SourceVersion: 1, Status: "active", Text: string(raw)}
	if r.Semantic != nil && r.Semantic.Enabled() {
		_, err := r.Semantic.Resolve(ctx, []semantic.Document{doc})
		return err
	}
	return r.Repo.UpsertSemanticDocument(ctx, semantic.StoredDocument{Document: doc, TextHash: semantic.HashText(doc.Text), Model: "purpose-exact-v1"})
}

// judgeReuse asks the judge model whether input.Text belongs to one of the
// room's existing purpose buckets. On a confident hit it reuses the stored
// conclusion; on a miss it returns ok=false so the caller falls through to the
// full resolution model.
func (r *Resolver) judgeReuse(ctx context.Context, input Input, factsSHA string) (Result, bool) {
	buckets, err := r.Repo.ListPurposeBuckets(ctx, input.Tenant, input.Room, input.Plan, 20)
	if err != nil || len(buckets) == 0 {
		return Result{}, false
	}
	payload, _ := json.Marshal(map[string]any{
		"新问题":     input.Text,
		"历史已分析问题": bucketQuestions(buckets),
	})
	raw, err := r.Judge(ctx, string(payload))
	if err != nil {
		return Result{}, false
	}
	var verdict struct {
		Same       bool `json:"same"`
		MatchIndex *int `json:"match_index"`
	}
	if contentstrategy.Decode(raw, &verdict) != nil || !verdict.Same {
		return Result{}, false
	}
	// same=true 但缺 match_index 时 MatchIndex 为 nil，绝不能默认命中第 0 桶。
	if verdict.MatchIndex == nil || *verdict.MatchIndex < 0 || *verdict.MatchIndex >= len(buckets) {
		return Result{}, false
	}
	hit := buckets[*verdict.MatchIndex]
	conclusion, err := r.Repo.GetPurposeBucketConclusion(ctx, input.Tenant, input.Room, input.Plan, hit.SourceID)
	if err != nil || conclusion == "" {
		return Result{}, false
	}
	var cached Result
	if json.Unmarshal([]byte(conclusion), &cached) != nil {
		return Result{}, false
	}
	if cached.FactsSHA != factsSHA {
		// 事实包变了，旧结论可能引用已失效的事实，不冒险复用。
		return Result{}, false
	}
	cached.Input = input.Text
	cached.Source = "bucket"
	return cached, true
}

func bucketQuestions(buckets []semantic.PurposeBucket) []string {
	out := make([]string, 0, len(buckets))
	for _, b := range buckets {
		if b.Question != "" {
			out = append(out, b.Question)
		}
	}
	return out
}

// storeBucket records the current question + conclusion as a semantic bucket for
// future judge-model reuse. Called after a successful resolution.
func (r *Resolver) storeBucket(ctx context.Context, input Input, result Result) {
	if result.Confidence < .85 {
		return
	}
	if result.Source != "model" {
		return
	}
	conclusion, _ := json.Marshal(result)
	sourceID := "bucket:" + contentstrategy.SHA(Normalize(input.Text))
	_ = r.Repo.UpsertPurposeBucket(ctx, input.Tenant, input.Room, input.Plan, sourceID, input.Text, string(conclusion))
}
