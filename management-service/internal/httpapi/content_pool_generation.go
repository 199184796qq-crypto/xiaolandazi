package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
)

func poolProtocolField(m map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, k := range keys {
		if v := m[k]; v != nil {
			return v
		}
	}
	return nil
}
func decodePoolTaskResponse(raw string, pool contentpools.Pool, previous []contentpools.Task) ([]contentpools.Task, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &top); err != nil {
		return nil, err
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(poolProtocolField(top, "tasks", "任务", "卖点角度任务"), &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows) > 200 {
		return nil, errors.New("内容池任务须为1到200条")
	}
	old := map[string]contentpools.Task{}
	for _, t := range previous {
		old[t.ID] = t
	}
	facts := map[string]contentpools.Fact{}
	for _, f := range pool.Facts {
		facts[f.ID] = f
	}
	result := []contentpools.Task{}
	seen := map[string]bool{}
	for _, r := range rows {
		var t contentpools.Task
		_ = json.Unmarshal(poolProtocolField(r, "id", "编号"), &t.ID)
		_ = json.Unmarshal(poolProtocolField(r, "selling_point", "卖点", "卖点名"), &t.Point)
		_ = json.Unmarshal(poolProtocolField(r, "angle", "角度"), &t.Angle)
		_ = json.Unmarshal(poolProtocolField(r, "target_audience", "目标人群"), &t.TargetAudience)
		_ = json.Unmarshal(poolProtocolField(r, "pain_point", "痛点"), &t.PainPoint)
		if err := json.Unmarshal(poolProtocolField(r, "source_ids", "事实编号", "依据编号"), &t.Sources); err != nil {
			return nil, errors.New("任务缺少事实引用编号")
		}
		t.Point, t.Angle = strings.TrimSpace(t.Point), strings.TrimSpace(t.Angle)
		if t.Point == "" || t.Angle == "" || len(t.Sources) == 0 {
			return nil, errors.New("任务卖点、角度或引用无效")
		}
		sort.Strings(t.Sources)
		refs := map[string]bool{}
		for _, id := range t.Sources {
			_, ok := facts[id]
			if !ok || refs[id] {
				return nil, errors.New("任务引用了其他池的事实或重复编号")
			}
			refs[id] = true
		}
		if _, ok := old[t.ID]; !ok {
			t.ID = "task-" + contentpools.Digest(struct {
				Pool, Angle string
				Refs        []string
			}{pool.Key, t.Angle, t.Sources})[:32]
		}
		if seen[t.ID] {
			return nil, errors.New("内容池任务编号重复")
		}
		seen[t.ID] = true
		result = append(result, t)
	}
	return result, nil
}

// The program validates task shape and referential integrity only. Numeric,
// product and wording judgments belong to the review model.
func (s *Server) reviewContentPoolTaskDraft(ctx context.Context, profile, mother string, pool *contentpools.Pool, previous []contentpools.Task, products []contentpools.Product, draft string, beforeCall func() error) error {
	tasks, structureErr := decodePoolTaskResponse(draft, *pool, previous)
	if structureErr == nil {
		pool.Tasks = tasks
	}
	for attempt := 0; attempt < 2; attempt++ {
		if beforeCall != nil {
			if err := beforeCall(); err != nil {
				return err
			}
		}
		input := map[string]any{"pool": pool, "products": products}
		if structureErr != nil {
			input["draft_response"] = draft
			input["structure_issue"] = structureErr.Error()
		}
		data, _ := json.Marshal(input)
		result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "content_pool_task_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(data)}}, 12000, false)
		if err != nil {
			return err
		}
		draft = result.Text
		known := previous
		if len(pool.Tasks) > 0 {
			known = pool.Tasks
		}
		tasks, structureErr = decodePoolTaskResponse(draft, *pool, known)
		if structureErr == nil {
			pool.Tasks = tasks
			return nil
		}
	}
	return fmt.Errorf("模型返回结构仍不完整，已自动复核两次：%w", structureErr)
}

func mergeEqualProductFacts(input []contentpools.Fact) []contentpools.Fact {
	out := []contentpools.Fact{}
	groups := map[string][]contentpools.Fact{}
	for _, f := range input {
		if f.Owner > 0 && !f.Private {
			groups[f.Text] = append(groups[f.Text], f)
		} else {
			out = append(out, f)
		}
	}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		if len(group) < 2 {
			out = append(out, group...)
			continue
		}
		products := []int64{}
		sources := []string{}
		for _, f := range group {
			products = append(products, f.Products...)
			sources = append(sources, f.Sources...)
		}
		products = contentpools.IDs(products)
		sort.Strings(sources)
		if len(products) < 2 {
			out = append(out, group...)
			continue
		}
		out = append(out, contentpools.Fact{ID: "shared-" + contentpools.Digest(sources)[:24], Text: key, Sources: sources, Products: products, Private: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *Server) classifyContentFacts(ctx context.Context, profile string, source contentpools.Sources, facts []contentpools.Fact) ([]contentpools.Fact, error) {
	originalFacts := facts
	facts = mergeEqualProductFacts(facts)
	pending := []contentpools.Fact{}
	attributes := []contentpools.Fact{}
	for _, f := range facts {
		if !f.Private && f.Owner == 0 {
			pending = append(pending, f)
		}
	}
	for _, f := range originalFacts {
		if f.Owner > 0 {
			attributes = append(attributes, f)
		}
	}
	if len(pending) == 0 {
		return facts, nil
	}
	mother, _, err := loadReplaceablePromptAsset("CONTENT_POOL_FACT_SCOPE_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-fact-scope.md", "商品资料适用范围母版")
	if err != nil {
		return nil, err
	}
	assignments := map[string][]int64{}
	known := map[int64]bool{}
	for _, p := range source.Products {
		known[p.ID] = true
	}
	for start := 0; start < len(pending); start += 100 {
		end := min(start+100, len(pending))
		data, _ := json.Marshal(map[string]any{"products": source.Products, "facts": pending[start:end], "product_attributes": attributes})
		result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "content_pool_fact_scope", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(data)}}, 8000, false)
		if err != nil {
			return nil, err
		}
		var top map[string]json.RawMessage
		if err = json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &top); err != nil {
			return nil, err
		}
		var rows []map[string]json.RawMessage
		if err = json.Unmarshal(poolProtocolField(top, "assignments", "归属", "适用范围"), &rows); err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, f := range pending[start:end] {
			allowed[f.ID] = true
		}
		if len(rows) != len(allowed) {
			return nil, errors.New("事实适用范围没有覆盖全部输入")
		}
		for _, row := range rows {
			var id string
			var ids []int64
			_ = json.Unmarshal(poolProtocolField(row, "fact_id", "事实编号"), &id)
			if err = json.Unmarshal(poolProtocolField(row, "products", "商品编号"), &ids); err != nil {
				return nil, err
			}
			if !allowed[id] {
				return nil, errors.New("事实适用范围编号无效")
			}
			delete(allowed, id)
			ids = contentpools.IDs(ids)
			for _, product := range ids {
				if !known[product] {
					return nil, errors.New("事实适用范围引用其他方案商品")
				}
			}
			assignments[id] = ids
		}
	}
	for i, f := range facts {
		if ids, ok := assignments[f.ID]; ok {
			facts[i].Products = ids
		}
	}
	return facts, nil
}

func (s *Server) buildContentPools(ctx context.Context, tenant, plan, actor int64, job appdb.PoolJob, source contentpools.Sources) (contentpools.Set, error) {
	stage := func(text string) { _ = s.store.PoolJobStage(context.WithoutCancel(ctx), tenant, plan, job.ID, text) }
	stage("正在核对事实与福利")
	safety, err := s.prepareDerivedSafety(ctx, tenant, plan, actor)
	if err != nil {
		return contentpools.Set{}, err
	}
	cfg, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return contentpools.Set{}, err
	}
	if cfg.SellingPointProfileID == "" {
		return contentpools.Set{}, errors.New("请先配置卖点挖掘模型")
	}
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	stage("正在归纳公共资料与商品专属资料")
	facts, err := s.classifyContentFacts(ctx, cfg.SellingPointProfileID, source, contentFactsFromSafety(source, safety.SafeFacts))
	if err != nil {
		return contentpools.Set{}, err
	}
	gen, _, err := loadReplaceablePromptAsset("CONTENT_POOL_TASK_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-task-generation.md", "内容池卖点角度母版")
	if err != nil {
		return contentpools.Set{}, err
	}
	review, _, err := loadReplaceablePromptAsset("CONTENT_POOL_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-task-review.md", "内容池任务事实核对母版")
	if err != nil {
		return contentpools.Set{}, err
	}
	legacyMaterial := ""
	for _, r := range source.Rows {
		if r.ID == "wheel:active" {
			legacyMaterial = r.Fields["内容"]
		}
	}
	pools := contentpools.GroupPools(facts, source.Products, derivedHash(gen+review+legacyMaterial))
	old, err := s.store.LatestContentPoolSet(ctx, tenant, plan)
	if err != nil {
		return contentpools.Set{}, err
	}
	prior := map[string]contentpools.Pool{}
	for _, p := range old.Pools {
		prior[p.Key] = p
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	indices := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	done := 0
	for worker := 0; worker < 3; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range indices {
				if work.Err() != nil {
					return
				}
				p := &pools[index]
				previous := prior[p.Key]
				if previous.Fingerprint == p.Fingerprint {
					p.Tasks = previous.Tasks
					p.Reused = true
				} else if len(p.Facts) > 0 {
					rev, e := s.store.ContentSourceRevision(work, tenant, plan)
					if e == nil && rev != source.Revision {
						e = appdb.ErrPoolSourceChanged
					}
					if e == nil {
						data, _ := json.Marshal(map[string]any{"pool": p, "products": source.Products, "existing_tasks": previous.Tasks, "confirmed_legacy_content": legacyMaterial})
						var result agentgateway.Response
						result, _, e = s.completeAnchorStyleWorkbench(work, cfg.SellingPointProfileID, "content_pool_task_generation", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: gen}, {Role: "user", Content: string(data)}}, 12000, false)
						if e == nil {
							e = s.reviewContentPoolTaskDraft(work, cfg.SellingPointProfileID, review, p, previous.Tasks, source.Products, result.Text, func() error {
								rev, err := s.store.ContentSourceRevision(work, tenant, plan)
								if err != nil {
									return err
								}
								if rev != source.Revision {
									return appdb.ErrPoolSourceChanged
								}
								return nil
							})
						}
					}
					if e != nil {
						mu.Lock()
						if first == nil {
							first = fmt.Errorf("%s更新未完成：%w", p.Label, e)
						}
						mu.Unlock()
						cancel()
						return
					}
				}
				mu.Lock()
				done++
				stage(fmt.Sprintf("内容池已完成 %d/%d", done, len(pools)))
				mu.Unlock()
			}
		}()
	}
enqueue:
	for i := range pools {
		select {
		case indices <- i:
		case <-work.Done():
			break enqueue
		}
	}
	close(indices)
	wg.Wait()
	if first != nil {
		return contentpools.Set{}, first
	}
	if err = ctx.Err(); err != nil {
		return contentpools.Set{}, err
	}
	current, err := s.captureContentSources(ctx, tenant, plan)
	if err != nil {
		return contentpools.Set{}, err
	}
	if current.SHA() != source.SHA() || current.Revision != source.Revision {
		return contentpools.Set{}, appdb.ErrPoolSourceChanged
	}
	set := contentpools.Set{Sources: source, SourceSHA: source.SHA(), SafeFacts: safety.SafeFacts, SafetyID: safety.ID, SafetyHash: safety.SourceSHA256, Pools: pools}
	if err = contentpools.ValidateSet(set); err != nil {
		return set, err
	}
	stage("正在保存完整内容草稿")
	return s.store.CommitContentPoolSet(ctx, tenant, plan, actor, job, set)
}

func (s *Server) runContentPoolJob(tenant, plan, actor int64, job appdb.PoolJob, source contentpools.Sources) {
	parent := s.anchorProducerContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				leaseCtx, stop := context.WithTimeout(ctx, 10*time.Second)
				ok, err := s.store.RenewPoolJob(leaseCtx, tenant, plan, job.ID)
				stop()
				if err != nil || !ok {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		if value := recover(); value != nil {
			s.store.FailPoolJob(context.Background(), tenant, plan, job.ID, "更新进程发生异常，可重试")
		}
	}()
	if _, err := s.buildContentPools(ctx, tenant, plan, actor, job, source); err != nil {
		// Provider errors may contain signed URLs; keep those in model traces.
		reason := "更新未完成，请重试；详情已保留在后台模型记录。"
		if errors.Is(err, appdb.ErrPoolSourceChanged) {
			reason = appdb.ErrPoolSourceChanged.Error()
		} else if !strings.Contains(err.Error(), "http") && !strings.Contains(err.Error(), "https") {
			reason = err.Error()
		}
		s.store.FailPoolJob(context.WithoutCancel(ctx), tenant, plan, job.ID, reason)
	}
}
