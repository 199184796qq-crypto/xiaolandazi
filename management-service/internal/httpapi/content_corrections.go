package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type contentCorrectionInput struct {
	ExpectedSetID int64   `json:"expected_set_id"`
	PoolKey       string  `json:"pool_key"`
	TaskID        string  `json:"task_id"`
	SelectedText  string  `json:"selected_text"`
	Products      []int64 `json:"products"`
	Feedback      string  `json:"feedback"`
}

// UI/request context selects the unit. A model never selects an internal row.
func bindContentCorrection(set contentpools.Set, in contentCorrectionInput) (contentpools.Correction, []string, error) {
	c := contentpools.Correction{ID: anchorcycle.ID(), Feedback: strings.TrimSpace(in.Feedback), Original: strings.TrimSpace(in.SelectedText), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if in.ExpectedSetID != set.ID || c.Feedback == "" || utf8.RuneCountInString(c.Feedback) > 5000 || utf8.RuneCountInString(c.Original) > 8000 {
		return c, nil, errors.New("请选择当前内容并填写纠正要求（最多5000字）")
	}
	facts := []string{}
	if in.TaskID != "" {
		for _, p := range set.Pools {
			if p.Key != in.PoolKey {
				continue
			}
			for _, t := range p.Tasks {
				if t.ID != in.TaskID {
					continue
				}
				c.Products = append([]int64{}, p.Products...)
				c.PoolKey = p.Key
				c.TaskID = t.ID
				c.Original = t.Angle
				for _, f := range p.Facts {
					for _, id := range t.Sources {
						if f.ID == id {
							facts = append(facts, f.Text)
							c.Sources = append(c.Sources, f.Sources...)
						}
					}
				}
				return c, facts, nil
			}
		}
		return c, nil, errors.New("所选内容已改变或不属于当前方案，请刷新")
	}
	if c.Original == "" {
		return c, nil, errors.New("请选择要纠正的内容")
	}
	c.Products = contentpools.IDs(in.Products)
	if len(c.Products) == 0 && len(set.Sources.Products) == 1 {
		c.Products = []int64{set.Sources.Products[0].ID}
	}
	if len(c.Products) == 0 {
		return c, nil, errors.New("请先选择这次纠正适用的商品；系统不会猜测商品范围")
	}
	for _, id := range c.Products {
		found := false
		for _, p := range set.Sources.Products {
			found = found || p.ID == id
		}
		if !found {
			return c, nil, errors.New("纠正范围不属于当前方案")
		}
	}
	for _, p := range set.Pools {
		if scopeWithin(p.Products, c.Products) {
			for _, f := range p.Facts {
				facts = append(facts, f.Text)
				c.Sources = append(c.Sources, f.Sources...)
			}
		}
	}
	return c, facts, nil
}
func scopeWithin(a, b []int64) bool {
	if len(a) == 0 {
		return false
	}
	for _, id := range a {
		if !contentpools.Contains(b, id) {
			return false
		}
	}
	return true
}
func correctionMessages(mother, p0 string, packet map[string]any) ([]agentgateway.Message, error) {
	st := &anchorcycle.State{P0PolicyMD: p0}
	if err := requireCycleP0(st); err != nil {
		return nil, err
	}
	packet["有效P0"] = generationP0View(p0, "*")
	raw, e := json.Marshal(packet)
	if e != nil {
		return nil, e
	}
	return []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, nil
}
func (s *Server) normalizeContentCorrection(ctx context.Context, c contentpools.Correction, facts []string, source contentpools.Sources) (contentpools.CorrectionContent, error) {
	mother, _, e := loadReplaceablePromptAsset("CONTENT_CORRECTION_PROMPT_PATH", "configs/prompts/anchor-style/content-correction-normalize.md", "自然语言纠正母版")
	if e != nil {
		return contentpools.CorrectionContent{}, e
	}
	p0, _, e := loadP0Mother()
	if e != nil {
		return contentpools.CorrectionContent{}, e
	}
	links := []map[string]string{}
	for _, p := range source.Products {
		if contentpools.Contains(c.Products, p.ID) {
			links = append(links, map[string]string{"链接": p.Link, "商品": p.Name})
		}
	}
	messages, e := correctionMessages(mother, p0, map[string]any{"所选内容": c.Original, "用户原话": c.Feedback, "相关已确认资料": facts, "本次适用商品": links, "已有长期纠正规则": contentpools.CorrectionView(source, c.Products)})
	if e != nil {
		return contentpools.CorrectionContent{}, e
	}
	res, e := s.completeCorrectionContent(ctx, "content_correction_normalize", agentgateway.ResponseJSON, messages, 2200)
	if e != nil {
		return contentpools.CorrectionContent{}, e
	}
	return contentpools.ParseCorrection(res.Text)
}
func (s *Server) contentCorrectionCreate(w http.ResponseWriter, r *http.Request) {
	tenant, plan, room, actorID, ok := s.contentPoolAccess(w, r, true)
	if !ok {
		return
	}
	actor, ok := s.resolveActor(w, r)
	if !ok || !s.requireLiveSupportPlanScope(w, r, actor, tenant, plan, true) {
		return
	}
	var in contentCorrectionInput
	if readJSON(w, r, &in) != nil {
		writeError(w, 400, "纠正输入无效")
		return
	}
	original, e := s.readyContentPoolSet(r.Context(), tenant, plan)
	if e != nil {
		writeError(w, 409, e.Error())
		return
	}
	c, facts, e := bindContentCorrection(original, in)
	if e != nil {
		writeError(w, 409, e.Error())
		return
	}
	ctx := withExchangeScope(r.Context(), appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan, RoomID: room, RunID: "correction:" + c.ID})
	c.Content, e = s.normalizeContentCorrection(ctx, c, facts, original.Sources)
	if e != nil {
		writeError(w, 422, "纠正未保存，整理没有完成；用户原话和模型结果已保留，可重试")
		return
	}
	if c.Content.Question != "" {
		writeJSON(w, 200, map[string]any{"state": "clarify", "question": c.Content.Question, "summary": c.Content.Summary, "unadopted_draft": c.Content})
		return
	}
	current, e := s.captureContentSources(r.Context(), tenant, plan)
	if e != nil || current.SHA() != original.SourceSHA || current.Revision != original.Sources.Revision {
		writeError(w, 409, "整理期间资料已改变，请刷新后重试；本次没有修改内容")
		return
	}
	job, e := s.store.ClaimPoolJob(r.Context(), tenant, plan, actorID, current)
	if e != nil {
		writeError(w, 409, "当前内容正在更新，请等待后重试")
		return
	}
	go s.runContentCorrectionJob(tenant, plan, room, actorID, job, original, c)
	writeJSON(w, 202, map[string]any{"state": "updating", "summary": c.Content.Summary, "correction": c, "job": job})
}

// Single-item calls: the caller keeps all IDs/references/product associations.
func (s *Server) repairContentItem(ctx context.Context, mother, p0, kind, text string, c contentpools.Correction, backgrounds ...map[string]any) (string, error) {
	// Each new request reads current mothers; exchanges preserve their actual bytes.
	var loadErr error
	mother, _, loadErr = loadReplaceablePromptAsset("CONTENT_CORRECTION_REPAIR_PROMPT_PATH", "configs/prompts/anchor-style/content-correction-repair.md", "纠正单项修订母版")
	if loadErr != nil {
		return "", loadErr
	}
	p0, _, loadErr = loadP0Mother()
	if loadErr != nil {
		return "", loadErr
	}
	packet := map[string]any{"内容类别": kind, "当前这一项": text, "用户确认的纠正": c.Content}
	if len(backgrounds) > 0 {
		packet["仅用于理解的背景"] = backgrounds[0]
	}
	messages, e := correctionMessages(mother, p0, packet)
	if e != nil {
		return "", e
	}
	res, e := s.completeCorrectionContent(ctx, "content_correction_repair", agentgateway.ResponseText, messages, 3500)
	if e != nil {
		return "", e
	}
	out := strings.TrimSpace(res.Text)
	if utf8.RuneCountInString(out) > 6000 {
		return "", errors.New("单项修订返回过长")
	}
	return out, nil
}

// One bounded retry for transport failures only. Format/meaning problems are
// recorded separately; failed work never partially adopts a corrected library.
func (s *Server) completeCorrectionContent(ctx context.Context, stage string, format agentgateway.ResponseFormat, messages []agentgateway.Message, maxTokens int) (agentgateway.Response, error) {
	var response agentgateway.Response
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			path, env, label := "configs/prompts/anchor-style/content-correction-normalize.md", "CONTENT_CORRECTION_PROMPT_PATH", "自然语言纠正母版"
			if stage == "content_correction_repair" {
				path, env, label = "configs/prompts/anchor-style/content-correction-repair.md", "CONTENT_CORRECTION_REPAIR_PROMPT_PATH", "纠正单项修订母版"
			}
			mother, _, e := loadReplaceablePromptAsset(env, path, label)
			if e != nil {
				return response, e
			}
			p0, _, e := loadP0Mother()
			if e != nil {
				return response, e
			}
			var packet map[string]any
			if e = json.Unmarshal([]byte(messages[len(messages)-1].Content), &packet); e != nil {
				return response, e
			}
			messages, e = correctionMessages(mother, p0, packet)
			if e != nil {
				return response, e
			}
		}
		response, _, err = s.completeAnchorStyleWorkbench(ctx, "", stage, format, messages, maxTokens, false)
		if err == nil || ctx.Err() != nil {
			return response, err
		}
	}
	return response, err
}
func (s *Server) repairCorrectionSet(ctx context.Context, original contentpools.Set, c contentpools.Correction, progress func(int, int)) (contentpools.Set, error) {
	if c.Content.Question != "" {
		return original, errors.New("纠正需要澄清，本次不修改内容")
	}
	next, e := contentpools.PartitionCorrectionPools(original, c.Products)
	if e != nil {
		return next, e
	}
	mother, _, e := loadReplaceablePromptAsset("CONTENT_CORRECTION_REPAIR_PROMPT_PATH", "configs/prompts/anchor-style/content-correction-repair.md", "纠正单项修订母版")
	if e != nil {
		return next, e
	}
	p0, _, e := loadP0Mother()
	if e != nil {
		return next, e
	}
	found := false
	for _, old := range next.Sources.Corrections {
		found = found || old.ID == c.ID
	}
	if !found {
		next.Sources.Corrections = append(next.Sources.Corrections, c)
	}
	type unit struct {
		pool, fact, task int
		kind, text       string
	}
	units := []unit{}
	for pi, p := range next.Pools {
		if !scopeWithin(p.Products, c.Products) {
			continue
		}
		for ti, t := range p.Tasks {
			if c.Content.Cancellation != "" && ((c.TaskID != "" && t.ID == c.TaskID) || t.Angle == c.Original) {
				next.Pools[pi].Tasks[ti].Disabled = true
			}
		}
		if !c.Content.Persistent() {
			continue
		}
		for fi, f := range p.Facts {
			units = append(units, unit{pi, fi, -1, "事实材料", f.Text})
		}
		for ti, t := range p.Tasks {
			if t.Disabled {
				continue
			}
			units = append(units, unit{pi, -1, ti, "内容方向", t.Angle})
		}
	}
	// Facts first, then tasks: each repaired task sees its corrected material.
	done := 0
	for _, u := range units {
		if ctx.Err() != nil {
			return next, ctx.Err()
		}
		text := u.text
		background := map[string]any{}
		if u.task >= 0 {
			t := next.Pools[u.pool].Tasks[u.task]
			material := []string{}
			for _, f := range next.Pools[u.pool].Facts {
				for _, id := range t.Sources {
					if id == f.ID {
						material = append(material, f.Text)
					}
				}
			}
			background = map[string]any{"沟通目的": t.Point, "观众顾虑": t.PainPoint, "参考人群": t.TargetAudience, "当前相关事实": material}
		}
		revised, err := s.repairContentItem(ctx, mother, p0, u.kind, text, c, background)
		if err != nil {
			return next, err
		}
		if u.fact >= 0 {
			if revised == "" {
				return next, errors.New("事实修订缺少可确认内容；未覆盖旧资料，请补充纠正要求")
			}
			next.Pools[u.pool].Facts[u.fact].Text = revised
		} else {
			if revised == "" {
				next.Pools[u.pool].Tasks[u.task].Disabled = true
			} else {
				next.Pools[u.pool].Tasks[u.task].Angle = revised
			}
		}
		done++
		if progress != nil {
			progress(done, len(units))
		}
	}
	for i := range next.Pools {
		if scopeWithin(next.Pools[i].Products, c.Products) {
			next.Pools[i].Fingerprint = contentpools.Digest(struct {
				Previous, Correction string
				Facts                []contentpools.Fact
				Tasks                []contentpools.Task
			}{next.Pools[i].Fingerprint, c.ID, next.Pools[i].Facts, next.Pools[i].Tasks})
			next.Pools[i].Reused = false
		}
	}
	next.SourceSHA = next.Sources.SHA()
	next.SafeFacts = contentpools.RenderCorrectedFacts(next)
	return next, contentpools.ValidateSet(next)
}
func (s *Server) runContentCorrectionJob(tenant, plan, room, actor int64, job appdb.PoolJob, original contentpools.Set, c contentpools.Correction) {
	parent := s.anchorProducerContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan, RoomID: room, RunID: "correction:" + c.ID})
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				ok, e := s.store.RenewPoolJob(ctx, tenant, plan, job.ID)
				if e != nil || !ok {
					cancel()
					return
				}
			}
		}
	}()
	fail := func() {
		s.store.FailPoolJob(context.WithoutCancel(ctx), tenant, plan, job.ID, "纠正未完整保存，旧方案保持可用；详情已保留，可重试")
	}
	defer func() {
		if recover() != nil {
			fail()
		}
	}()
	next, e := s.repairCorrectionSet(ctx, original, c, func(n, total int) {
		_ = s.store.PoolJobStage(ctx, tenant, plan, job.ID, "正在按纠正修订关联内容 "+strconv.Itoa(n)+"/"+strconv.Itoa(total))
	})
	if e != nil {
		fail()
		return
	}
	// A standalone safety asset is immutable and only becomes effective through
	// the newly saved set/version; an interrupted commit leaves it unreferenced.
	next, e = s.saveCorrectedSafety(ctx, tenant, plan, actor, next, original.SafetyHash)
	if e != nil {
		fail()
		return
	}
	if _, e = s.store.SaveContentCorrection(ctx, tenant, plan, actor, original, next, c, job); e != nil {
		fail()
	}
}
func (s *Server) saveCorrectedSafety(ctx context.Context, tenant, plan, actor int64, next contentpools.Set, oldHash string) (contentpools.Set, error) {
	old, e := s.store.GetLiveDerivedSafety(ctx, tenant, plan, oldHash)
	if e != nil {
		return next, e
	}
	old.ID = 0
	old.OriginalFacts = old.SafeFacts
	old.SafeFacts = next.SafeFacts
	old.SourceSHA256 = derivedHash(oldHash + next.SourceSHA + next.SafeFacts)
	old.Notice = "用户自然语言纠正及关联内容已保存，下次保存方案使用"
	old.ChangesJSON = "[]"
	safety, e := s.store.SaveLiveDerivedSafety(ctx, actor, old)
	if e != nil {
		return next, e
	}
	next.SafetyID, next.SafetyHash = safety.ID, safety.SourceSHA256
	return next, nil
}
