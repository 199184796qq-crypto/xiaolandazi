package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentpools"
)

type poolContent struct {
	Point    string `json:"selling_point"`
	Angle    string `json:"angle"`
	Audience string `json:"target_audience,omitempty"`
	Pain     string `json:"pain_point,omitempty"`
}

func taskContent(t contentpools.Task) poolContent {
	return poolContent{t.Point, t.Angle, t.TargetAudience, t.PainPoint}
}
func poolMaterial(p contentpools.Pool, products []contentpools.Product) map[string]any {
	facts := []string{}
	for _, f := range p.Facts {
		facts = append(facts, f.Text)
	}
	items := []map[string]string{}
	for _, product := range products {
		if contentpools.Contains(p.Products, product.ID) {
			items = append(items, map[string]string{"link": product.Link, "name": product.Name})
		}
	}
	return map[string]any{"facts": facts, "products": items, "common": p.Owner == 0}
}
func poolNotice(p contentpools.Pool, content, kind, reason string, retry bool) contentpools.Notice {
	return contentpools.Notice{Pool: p.Label, Content: content, Kind: kind, Reason: reason, Retryable: retry}
}

func poolMessages(mother, p0 string, packet map[string]any) []agentgateway.Message {
	data, _ := json.Marshal(packet)
	// Complete P0 is instruction, never quoted factual source material. Keep
	// the current task last, with a short content-only user packet.
	return []agentgateway.Message{{Role: "system", Content: p0 + "\n\n" + mother}, {Role: "user", Content: "本次实际输入 JSON：\n" + string(data)}}
}

// Decode each content row independently. Legacy metadata is ignored, not trusted
// or repaired by the model. No decoded row is executable until its own review.
func decodePoolContent(raw string) ([]poolContent, []contentpools.Notice) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &top); err != nil {
		return nil, []contentpools.Notice{{Kind: "format", Reason: "返回格式未完成解析，本批内容未入池", Retryable: true}}
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(poolProtocolField(top, "tasks", "任务", "卖点角度任务"), &rows); err != nil || rows == nil {
		return nil, []contentpools.Notice{{Kind: "format", Reason: "返回没有有效内容列表，本批内容未入池", Retryable: true}}
	}
	result := []poolContent{}
	notes := []contentpools.Notice{}
	seen := map[string]bool{}
	for i, row := range rows {
		if i >= 40 {
			notes = append(notes, contentpools.Notice{Kind: "format", Reason: "多余内容未处理，可另次生成", Retryable: true})
			break
		}
		var item poolContent
		if json.Unmarshal(row, &item) != nil || strings.TrimSpace(item.Point) == "" || strings.TrimSpace(item.Angle) == "" {
			notes = append(notes, contentpools.Notice{Kind: "format", Content: string(row), Reason: "本条内容为空或格式不完整，已跳过，其他内容继续", Retryable: true})
			continue
		}
		item.Point, item.Angle = strings.TrimSpace(item.Point), strings.TrimSpace(item.Angle)
		key := contentpools.Digest(item)
		if !seen[key] {
			result = append(result, item)
			seen[key] = true
		}
	}
	return result, notes
}
func bindPoolContent(p contentpools.Pool, item poolContent, previous []contentpools.Task) contentpools.Task {
	refs := []string{}
	for _, f := range p.Facts {
		refs = append(refs, f.ID)
	}
	// The entire reviewed request material is the provenance boundary. These are
	// pool-level references, not a claim that every sentence used every fact.
	t := contentpools.Task{ID: "task-" + contentpools.Digest(struct {
		Pool    string
		Content poolContent
	}{p.Key, item})[:32], Point: item.Point, Angle: item.Angle, TargetAudience: item.Audience, PainPoint: item.Pain, Sources: refs}
	for _, old := range previous {
		if taskContent(old) == item {
			t.ID = old.ID
			t.Disabled = old.Disabled
			break
		}
	}
	return t
}
func (s *Server) reviewSimplePoolContent(ctx context.Context, profile, mother, p0 string, p contentpools.Pool, products []contentpools.Product, corrections []map[string]any, item poolContent) (poolContent, string, error) {
	packet := poolMaterial(p, products)
	packet["candidate"], packet["user_confirmed_corrections"] = item, corrections
	call, stop := context.WithTimeout(ctx, 45*time.Second)
	defer stop()
	response, _, err := s.completeAnchorStyleWorkbench(call, profile, "content_pool_task_review", agentgateway.ResponseJSON, poolMessages(mother, p0, packet), 2400, false)
	if err != nil {
		return item, "", err
	}
	var result struct {
		Passed  *bool       `json:"passed"`
		Reason  string      `json:"reason"`
		Content poolContent `json:"content"`
	}
	if json.Unmarshal([]byte(stripPolicyJSONFence(response.Text)), &result) != nil || result.Passed == nil {
		return item, "", errors.New("本条审核格式不完整，未入池")
	}
	if !*result.Passed {
		if strings.TrimSpace(result.Reason) == "" {
			return item, "", errors.New("本条审核没有完成判断，未入池")
		}
		return item, result.Reason, nil
	}
	if strings.TrimSpace(result.Content.Point) == "" || strings.TrimSpace(result.Content.Angle) == "" {
		return item, "", errors.New("本条审核返回空内容，未入池")
	}
	// The reviewer may repair wording, but its rewrite is a new candidate. It
	// has no approval for its own added claims. Require one content-only verdict
	// before it can replace the original in the pool.
	if result.Content != item {
		packet["candidate"]=result.Content
		packet["review_mode"]="judge_only"
		response,_,err=s.completeAnchorStyleWorkbench(call,profile,"content_pool_task_review",agentgateway.ResponseJSON,poolMessages(mother,p0,packet),1200,false)
		if err!=nil{return item,"",err}
		var verdict struct{Passed *bool `json:"passed"`;Reason string `json:"reason"`}
		if json.Unmarshal([]byte(stripPolicyJSONFence(response.Text)),&verdict)!=nil||verdict.Passed==nil{return item,"",errors.New("修订后单条核对未完成，未入池")}
		if !*verdict.Passed {if strings.TrimSpace(verdict.Reason)==""{return item,"",errors.New("修订后核对缺少判断理由，未入池")};return item,verdict.Reason,nil}
	}
	return result.Content, "", nil
}
func (s *Server) generateSimplePool(ctx context.Context, profile, gen, review, p0 string, p *contentpools.Pool, previous []contentpools.Task, source contentpools.Sources, beforeCall func() error) ([]contentpools.Notice, error) {
	packet := poolMaterial(*p, source.Products)
	old := []poolContent{}
	notes := []contentpools.Notice{}
	for _, t := range previous {
		if !t.Disabled && strings.TrimSpace(t.Angle) == "" {
			notes = append(notes, poolNotice(*p, t.Point, "excluded", "旧条目没有有效内容，未入池；当前合格资料继续生成", false))
		}
		if !t.Disabled && strings.TrimSpace(t.Angle) != "" {
			old = append(old, taskContent(t))
		}
	}
	packet["existing_content"], packet["user_confirmed_corrections"] = old, contentpools.CorrectionView(source, p.Products)
	if err := beforeCall(); err != nil {
		return nil, err
	}
	call, stop := context.WithTimeout(ctx, 60*time.Second)
	response, _, err := s.completeAnchorStyleWorkbench(call, profile, "content_pool_task_generation", agentgateway.ResponseJSON, poolMessages(gen, p0, packet), 6000, false)
	stop()
	drafts := []poolContent{}
	if err != nil {
		notes = append(notes, poolNotice(*p, "", "unavailable", "本池生成暂未完成；已有内容逐条重新核对，其余池继续", true))
		drafts = old
	} else {
		var decoded []contentpools.Notice
		drafts, decoded = decodePoolContent(response.Text)
		for _, n := range decoded {
			n.Pool = p.Label
			notes = append(notes, n)
		}
	}
	// Preserve user's still-supported edits even if generation omits them.
	// They are candidates only, and get the same independent current review.
	drafts = append(drafts, old...)
	p.Tasks = []contentpools.Task{}
	seen := map[string]bool{}
	for _, draft := range drafts {
		if err := beforeCall(); err != nil {
			return notes, err
		}
		checked, reason, err := s.reviewSimplePoolContent(ctx, profile, review, p0, *p, source.Products, contentpools.CorrectionView(source, p.Products), draft)
		if err != nil {
			notes = append(notes, poolNotice(*p, draft.Angle, "unavailable", "本条审核未完成，已留在池外；可重试", true))
			continue
		}
		if reason != "" {
			notes = append(notes, poolNotice(*p, draft.Angle, "excluded", reason, false))
			continue
		}
		t := bindPoolContent(*p, checked, previous)
		if !seen[t.ID] {
			p.Tasks = append(p.Tasks, t)
			seen[t.ID] = true
		}
	}
	if len(p.Tasks) == 0 {
		notes = append(notes, poolNotice(*p, "", "empty", "这个池暂无审核通过内容，其他池可继续核对与保存", false))
	}
	return notes, nil
}

// One request owns one product's applicability. The model selects verbatim
// factual content; IDs and associations never enter its protocol.
func (s *Server) classifySimplePoolFacts(ctx context.Context, profile string, source contentpools.Sources, facts []contentpools.Fact, p0 string) ([]contentpools.Fact, []contentpools.Notice, error) {
	original := facts
	facts = mergeEqualProductFacts(facts)
	mother, _, err := loadReplaceablePromptAsset("CONTENT_POOL_SIMPLE_SCOPE_PATH", "configs/prompts/anchor-style/content-pool-simple-scope.md", "内容池单商品范围母版")
	if err != nil {
		return nil, nil, err
	}
	pending := []string{}
	known := map[string]bool{}
	for i, f := range facts {
		if f.Owner == 0 && !f.Private {
			facts[i].Products = nil
			if !known[f.Text] {
				pending = append(pending, f.Text)
				known[f.Text] = true
			}
		}
	}
	if len(pending) == 0 {
		return facts, nil, nil
	}
	notes := []contentpools.Notice{}
	for _, product := range source.Products {
		if ctx.Err() != nil {
			return facts, notes, ctx.Err()
		}
		attributes := []string{}
		for _, f := range original {
			if f.Owner == product.ID {
				attributes = append(attributes, f.Text)
			}
		}
		packet := map[string]any{"product": map[string]string{"link": product.Link, "name": product.Name}, "product_attributes": attributes, "facts": pending, "user_confirmed_corrections": contentpools.CorrectionView(source, []int64{product.ID})}
		call, stop := context.WithTimeout(ctx, 45*time.Second)
		result, _, e := s.completeAnchorStyleWorkbench(call, profile, "content_pool_fact_scope", agentgateway.ResponseJSON, poolMessages(mother, p0, packet), 6000, false)
		stop()
		var response struct {
			Applicable *[]string `json:"applicable_facts"`
			Excluded   []struct {
				Fact   string `json:"fact"`
				Reason string `json:"reason"`
			} `json:"excluded"`
		}
		if e != nil || json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &response) != nil || response.Applicable == nil {
			empty := []string{}
			response.Applicable = &empty
			response.Excluded = nil
		}
		judged := map[string]bool{}
		for _, text := range *response.Applicable {
			if known[text] {
				judged[text] = true
			}
		}
		for _, x := range response.Excluded {
			if known[x.Fact] && strings.TrimSpace(x.Reason) != "" {
				judged[x.Fact] = true
			}
		}
		missing := []string{}
		for _, text := range pending {
			if !judged[text] {
				missing = append(missing, text)
			}
		}
		if len(missing) > 0 && ctx.Err() == nil {
			// Only unfinished items go to one independent bounded review; valid
			// siblings from the first response stay associated with this product.
			packet["facts"] = missing
			call, stop := context.WithTimeout(ctx, 30*time.Second)
			retry, _, e := s.completeAnchorStyleWorkbench(call, profile, "content_pool_fact_scope_review", agentgateway.ResponseJSON, poolMessages(mother, p0, packet), 6000, false)
			stop()
			var checked struct {
				Applicable []string `json:"applicable_facts"`
				Excluded   []struct {
					Fact   string `json:"fact"`
					Reason string `json:"reason"`
				} `json:"excluded"`
			}
			if e == nil && json.Unmarshal([]byte(stripPolicyJSONFence(retry.Text)), &checked) == nil {
				*response.Applicable = append(*response.Applicable, checked.Applicable...)
				response.Excluded = append(response.Excluded, checked.Excluded...)
			}
		}
		accepted := map[string]bool{}
		for _, text := range *response.Applicable {
			if !known[text] {
				notes = append(notes, contentpools.Notice{Pool: product.Link + "号 · " + product.Name, Content: text, Kind: "format", Reason: "适用资料未逐字对应输入内容，未建立关联", Retryable: true})
				continue
			}
			accepted[text] = true
		}
		for i, f := range facts {
			if f.Owner == 0 && !f.Private && accepted[f.Text] {
				facts[i].Products = append(facts[i].Products, product.ID)
			}
		}
		excluded := map[string]bool{}
		for _, x := range response.Excluded {
			if known[x.Fact] && !accepted[x.Fact] && strings.TrimSpace(x.Reason) != "" {
				excluded[x.Fact] = true
				notes = append(notes, contentpools.Notice{Pool: product.Link + "号 · " + product.Name, Content: x.Fact, Kind: "excluded", Reason: x.Reason})
			}
		}
		for _, text := range pending {
			if !accepted[text] && !excluded[text] {
				notes = append(notes, contentpools.Notice{Pool: product.Link + "号 · " + product.Name, Content: text, Kind: "unavailable", Reason: "本条适用范围没有明确判断，未建立关联；其他资料继续", Retryable: true})
			}
		}
	}
	return facts, notes, nil
}
