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
}
type Complete func(context.Context, string) (string, error)
type Resolver struct {
	Repo     Repository
	Semantic *semantic.Service
	Complete Complete
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
	if result.PurposeID == "" && result.Purpose != "" {
		result.PurposeID = "L" + contentstrategy.SHA(result.Purpose)[:12]
	}
	for i := range result.Angles {
		result.Angles[i].PurposeID = result.PurposeID
	}
	if len(result.Angles) > 0 {
		p := contentstrategy.Purpose{ID: result.PurposeID, Name: result.Purpose, Concern: result.Purpose, FactIDs: result.Angles[0].FactIDs}
		if err = contentstrategy.Validate(contentstrategy.Facts(input.Facts), []contentstrategy.Purpose{p}, result.Angles, true); err != nil {
			return Result{}, err
		}
		for _, row := range result.Angles {
			if err = contentstrategy.ValidateRowPolicy(row, true); err != nil {
				return Result{}, err
			}
		}
	}
	result.FactsSHA, result.Input, result.Source = factsSHA, input.Text, "model"
	result.Topic = "PURPOSE:" + contentstrategy.SHA(result.PurposeID + "/" + preciseFamily(input.Text) + "/" + result.Scope)[:24]
	if result.Confidence >= .85 {
		if err = r.save(ctx, input, id, result); err != nil {
			return Result{}, fmt.Errorf("保存目的知识失败：%w", err)
		}
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
