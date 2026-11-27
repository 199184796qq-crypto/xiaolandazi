package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechasr"
	"livecompanion/management/internal/storage"
)

const fissionJobTimeout = 28 * time.Minute

var humanFissionWake = make(chan struct{}, 1)

func (s *Server) handleHumanFission(w http.ResponseWriter, r *http.Request, tenant, plan, source, actor int64) {
	ctx := r.Context()
	asset, err := s.store.ScriptHumanVoice(ctx, tenant, plan, db.PlanHumanScriptScope, source)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	if r.Method == http.MethodDelete || strings.HasSuffix(r.Pattern, "/fission/clear") {
		s.handleHumanFissionClear(w, r, tenant, plan, source, actor)
		return
	}
	if strings.HasSuffix(r.Pattern, "/parts/{part}/split") {
		s.queueHumanFissionSplit(w, r, tenant, plan, source)
		return
	}
	if strings.HasSuffix(r.Pattern, "/fission/parts/merge") {
		s.queueHumanFissionMerge(w, r, tenant, plan, source)
		return
	}
	if strings.HasSuffix(r.Pattern, "/fission/names") {
		s.handleHumanFissionNames(w, r, tenant, plan, source)
		return
	}
	if strings.Contains(r.Pattern, "/fission/parts") {
		s.handleHumanFissionParts(w, r, tenant, plan, source, db.HumanFissionInput(asset))
		return
	}
	action := "plan"
	if strings.HasSuffix(r.Pattern, "/render") {
		action = "render"
	}
	if r.Method == http.MethodPost {
		if err = s.store.QueueHumanFission(ctx, tenant, plan, source, action); err != nil {
			if errors.Is(err, db.ErrFissionUpdated) || strings.HasPrefix(err.Error(), "录音") || strings.HasPrefix(err.Error(), "请先") {
				writeError(w, 409, err.Error())
			} else {
				writeScriptHumanVoiceError(w, err)
			}
			return
		}
		select {
		case humanFissionWake <- struct{}{}:
		default:
		}
	}
	job, err := s.store.HumanFission(ctx, tenant, plan, source)
	if errors.Is(err, sql.ErrNoRows) || err == nil && job.State.Input != db.HumanFissionInput(asset) {
		writeJSON(w, 200, map[string]any{"status": "idle"})
		return
	}
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	out := map[string]any{"status": job.Status, "action": job.Action, "detail": job.Detail,
		"segments": len(job.State.Parts), "parts_ready": job.State.ReadyParts(), "plans": len(job.State.Recipes), "remaining": job.State.Unused(),
		"exhausted": job.State.Exhausted, "missing_names": job.State.NeedsNames()}
	type recipeRow struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Duration int64  `json:"duration_ms"`
		Version  string `json:"version"`
	}
	recipes := make([]recipeRow, 0, len(job.State.Recipes))
	for i, recipe := range job.State.Recipes {
		recipes = append(recipes, recipeRow{recipe.ID, job.State.RecipeName(recipe, i+1), recipe.Duration, job.State.RecipeVersion()})
	}
	out["recipes"] = recipes
	if last := job.State.Render; last != nil && job.Status == "ready" {
		stamp, _, e := s.humanFissionSnapshot(ctx, job)
		if e == nil && stamp == last.Stamp {
			if oss, e := s.directAudioStore(); e == nil {
				if url, e := oss.SignedURL(ctx, last.Object, 10*time.Minute); e == nil {
					out["playback"] = map[string]any{"url": url, "recipe_id": last.RecipeID, "duration_ms": last.Duration, "srt": last.SRT}
				}
			}
		}
	}
	code := 200
	if r.Method == http.MethodPost && (job.Status == "queued" || job.Status == "running") {
		code = 202
	}
	writeJSON(w, code, out)
}

func (s *Server) handleHumanFissionClear(w http.ResponseWriter, r *http.Request, tenant, plan, source, actor int64) {
	ctx := r.Context()
	if r.Method == http.MethodGet {
		job, err := s.store.HumanFission(ctx, tenant, plan, source)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"available": false})
			return
		}
		if err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
		if job.Busy() {
			writeError(w, http.StatusConflict, db.ErrFissionClearBusy.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"available": true, "revision": job.ClearRevision(),
			"segments": len(job.State.Parts), "plans": len(job.State.Recipes)})
		return
	}
	var input struct {
		Revision string `json:"revision"`
	}
	if err := readJSONWithLimit(w, r, &input, 2<<10); err != nil || len(input.Revision) != 64 {
		writeError(w, http.StatusBadRequest, "请先核对要清空的裂变结果")
		return
	}
	if err := s.store.ClearHumanFission(ctx, tenant, plan, source, actor, input.Revision); err != nil {
		if errors.Is(err, db.ErrFissionClearBusy) || errors.Is(err, db.ErrFissionClearChanged) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeScriptHumanVoiceError(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHumanFissionParts(w http.ResponseWriter, r *http.Request, tenant, plan, source int64, input string) {
	ctx := r.Context()
	job, err := s.store.HumanFission(ctx, tenant, plan, source)
	if err != nil {
		writeScriptHumanVoiceError(w, err)
		return
	}
	if job.State.Input != input || job.State.ReadyParts() == 0 {
		writeError(w, http.StatusConflict, "语音片还未就绪，请刷新后再试")
		return
	}
	if value := r.PathValue("part"); value != "" {
		index, err := strconv.Atoi(value)
		if err != nil || index < 1 || index > len(job.State.Parts) {
			writeError(w, http.StatusNotFound, "语音片不存在")
			return
		}
		part := job.State.Parts[index-1]
		if version := r.URL.Query().Get("version"); version != "" && version != part.Version() {
			writeError(w, http.StatusConflict, "语音片已更新，请重新打开列表")
			return
		}
		asset, err := s.store.ScriptHumanVoiceByObject(ctx, tenant, plan, db.HumanFissionScope(source), part.Object)
		if err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
		playback, err := s.scriptHumanVoicePlayback(ctx, asset)
		if err != nil {
			writeScriptHumanVoiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, playback)
		return
	}
	type item struct {
		Index    int    `json:"number"`
		Name     string `json:"name"`
		Duration int64  `json:"duration_ms"`
		Text     string `json:"text"`
		Version  string `json:"version"`
		Edited   bool   `json:"edited,omitempty"`
	}
	items := make([]item, 0, len(job.State.Parts))
	for i, part := range job.State.Parts {
		items = append(items, item{Index: i + 1, Name: part.DisplayName(i + 1), Duration: part.Duration, Text: part.Text, Version: part.Version(), Edited: part.Edited})
	}
	response := map[string]any{"items": items, "busy": job.Busy(), "missing_names": job.State.NeedsNames(), "name_status": ""}
	if job.Action == "names" {
		response["name_status"] = job.Status
	}
	if job.Action == "split" {
		response["split_status"] = job.Status
		response["split_detail"] = job.Detail
	}
	if job.Action == "merge" {
		response["merge_status"], response["merge_detail"] = job.Status, job.Detail
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) RunHumanFission(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for ctx.Err() == nil {
		if s.leader == nil || s.leader.IsLeader() {
			claim, cancel := context.WithTimeout(ctx, 5*time.Second)
			job, err := s.store.ClaimHumanFission(claim, rand.Text(), time.Now().Add(fissionJobTimeout+time.Minute))
			cancel()
			if err == nil {
				s.processHumanFission(ctx, job)
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
				log.Printf("claim human fission: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-humanFissionWake:
		}
	}
}

func (s *Server) processHumanFission(parent context.Context, job db.HumanFissionJob) {
	ctx, cancel := context.WithTimeout(parent, fissionJobTimeout)
	defer cancel()
	status, detail, delay := "ready", "", time.Duration(0)
	if err := s.runHumanFission(ctx, &job); err != nil {
		status, detail = "failed", err.Error()
		if errors.Is(err, db.ErrModelPoolFull) {
			status, detail, delay = "queued", "正在等待模型池名额…", 20*time.Second
		} else if parent.Err() != nil {
			status, detail, delay = "queued", "任务已排队，等待继续处理…", 20*time.Second
		}
		if utf8.RuneCountInString(detail) > 240 {
			detail = string([]rune(detail)[:240])
		}
		log.Printf("human fission plan=%d source=%d action=%s: %v", job.Plan, job.Source, job.Action, err)
	}
	save, stop := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer stop()
	if err := s.store.SaveHumanFission(save, job, status, detail, delay); err != nil && !errors.Is(err, db.ErrFissionUpdated) {
		log.Printf("record human fission result source=%d: %v", job.Source, err)
	}
}

func (s *Server) runHumanFission(ctx context.Context, job *db.HumanFissionJob) error {
	original, err := s.store.ScriptHumanVoice(ctx, job.Tenant, job.Plan, db.PlanHumanScriptScope, job.Source)
	if err != nil {
		return err
	}
	if job.State.Input != db.HumanFissionInput(original) {
		return db.ErrFissionUpdated
	}
	oss, err := s.directAudioStore()
	if err != nil || original.StorageBucket != oss.Bucket() {
		return errors.New("方案录音存储不可用")
	}
	ctx = withExchangeScope(ctx, db.ModelExchangeScope{TenantID: job.Tenant, PlanID: job.Plan, RunID: job.Token, CandidateID: strconv.FormatInt(job.Source, 10)})
	report := func(detail string) error {
		err := s.store.UpdateHumanFissionProgress(ctx, *job, detail)
		if err == nil || errors.Is(err, db.ErrFissionUpdated) || ctx.Err() != nil {
			return err
		}
		log.Printf("human fission progress source=%d: %v", job.Source, err)
		return nil
	}
	switch job.Action {
	case "names":
		return s.nameHumanFission(ctx, job, report)
	case "render_exact":
		if err := report("正在核对事实并准备点播组合…"); err != nil {
			return err
		}
		return s.renderExactHumanFission(ctx, job, oss)
	case "split", "merge":
		if job.Action == "split" {
			err = s.splitHumanFissionPart(ctx, job, oss, report)
		} else {
			err = s.mergeHumanFissionParts(ctx, job, oss, report)
		}
		if err != nil {
			return err
		}
		s.tryHumanFissionNames(ctx, job, report)
		return nil
	}
	if len(job.State.Parts) == 0 {
		if err = s.cutHumanFission(ctx, job, oss, original, report); err != nil {
			return err
		}
		if err = s.store.SaveHumanFission(ctx, *job, "running", "切分完成，正在准备逐片转写…", 0); err != nil {
			return err
		}
	}
	if job.State.ReadyParts() == 0 {
		if err = transcribeHumanFissionParts(ctx, oss, job.State.Parts, report); err != nil {
			return err
		}
		if err = s.store.SaveHumanFission(ctx, *job, "running", "全部转写完成，正在读取方案事实…", 0); err != nil {
			return err
		}
	}
	s.tryHumanFissionNames(ctx, job, report)
	facts, err := s.buildRewriteFactsPack(ctx, job.Tenant, job.Plan)
	if err != nil {
		return err
	}
	stamp := humanfission.Digest(facts)
	if job.State.ExhaustedAt != stamp {
		job.State.Exhausted = false
	}
	if !job.State.Exhausted && (job.Action == "plan" || job.State.Unused() <= humanfission.RefillAt) {
		if err = report("正在规划话术组合…"); err != nil {
			return err
		}
		if err = s.planHumanFission(ctx, job, facts, stamp); err != nil {
			if job.Action != "render" || len(job.State.Recipes) == 0 {
				return err
			}
			log.Printf("replenish human fission plans source=%d: %v", job.Source, err)
		} else if err = s.store.SaveHumanFission(ctx, *job, "running", "话术组合已保存…", 0); err != nil {
			return err
		}
	}
	if job.Action == "render" {
		if err = report("正在核对事实并准备试听…"); err != nil {
			return err
		}
		return s.renderHumanFission(ctx, job, oss, job.State.Last)
	}
	return nil
}

func (s *Server) completeHumanFission(ctx context.Context, stage string, payload any, out any) error {
	template, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/human-fission-"+stage+".md", "真人话术裂变母版")
	if err != nil {
		return err
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	profile, lease, err := s.store.AcquirePlanModelLease(call, time.Now().Add(7*time.Minute))
	if err != nil {
		return err
	}
	defer func() {
		release, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if err := s.store.ReleasePlanModelLease(release, lease); err != nil {
			log.Printf("release human fission model slot: %v", err)
		}
	}()
	mode := stage != "review" && stage != "name"
	request := agentgateway.Request{
		Stage: "human_fission_" + stage, Model: profile.Model, ResponseFormat: agentgateway.ResponseJSON,
		UseProviderOutputLimit: mode, ThinkingOverride: &mode, Timeout: 6 * time.Minute,
		Messages: []agentgateway.Message{{Role: "system", Content: template + "\n\n" + p0}, {Role: "user", Content: string(body)}},
	}
	if mode {
		request.ReasoningEffort = "high"
	} else {
		request.MaxTokens = profile.MaxTokens
		if stage == "name" {
			request.MaxTokens = 8192
		}
	}
	start := time.Now()
	id, err := s.beginModelExchange(call, request.Stage, profile.ID, request)
	if err != nil {
		return err
	}
	result, err := agentgateway.CompleteSpeechEndpoint(call, profile, request)
	if err = s.finishModelExchange(call, id, result, result.Text, start, result.InputTokens, result.OutputTokens, err); err != nil {
		return errors.New("裂变模型调用失败，请稍后重试")
	}
	if result.FinishReason == "length" {
		return errors.New("裂变模型返回未完成，请重试")
	}
	if err = json.Unmarshal([]byte(result.Text), out); err != nil {
		return errors.New("裂变模型未返回可验证的结果")
	}
	return nil
}

func humanFissionRead(ctx context.Context, oss *storage.OSS, key string) ([]int16, error) {
	file, err := oss.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		return nil, err
	}
	return humanfission.ReadWAV(raw)
}

func (s *Server) putHumanFission(ctx context.Context, job db.HumanFissionJob, oss *storage.OSS, name string, pcm []int16) (string, error) {
	raw := humanfission.WAV(pcm)
	key := job.ObjectKey(name)
	duration := uint64(humanfission.MS(len(pcm)))
	asset := model.CreateMediaAssetInput{OriginalName: name, StorageDriver: oss.Driver(), StorageBucket: oss.Bucket(), ObjectKey: key, SizeBytes: uint64(len(raw)), DurationMS: &duration}
	err := s.store.SaveHumanFissionMedia(ctx, job, asset, func() error { return oss.PutPrivate(ctx, key, bytes.NewReader(raw), "audio/wav") })
	return key, err
}

func (s *Server) cutHumanFission(ctx context.Context, job *db.HumanFissionJob, oss *storage.OSS, asset model.MediaAsset, report func(string) error) error {
	if err := report("正在读取录音和文稿…"); err != nil {
		return err
	}
	transcript, _ := asset.Metadata["script_transcript"].(string)
	cues, err := humanfission.ReadSRT(transcript)
	if err != nil {
		return err
	}
	pcm, err := humanFissionRead(ctx, oss, asset.ObjectKey)
	if err != nil {
		return err
	}
	job.State.Duration = humanfission.MS(len(pcm))
	var guide struct {
		Cuts *[]struct {
			MS     int64  `json:"ms"`
			Before string `json:"before"`
			After  string `json:"after"`
		} `json:"cuts"`
	}
	if err = report("正在分析文稿，确定语义分组…"); err != nil {
		return err
	}
	if err = s.completeHumanFission(ctx, "cut", map[string]any{"duration_ms": job.State.Duration, "transcript": cues}, &guide); err != nil {
		return err
	}
	if guide.Cuts == nil {
		return errors.New("模型没有返回语义切分坐标")
	}
	points := make([]int64, 0, len(*guide.Cuts))
	for _, cut := range *guide.Cuts {
		if !matchesFissionGuide(cues, cut.MS, cut.Before, cut.After) {
			return fmt.Errorf("第 %.3f 秒的分组坐标与原文指引不一致", float64(cut.MS)/1000)
		}
		points = append(points, cut.MS)
	}
	if err = report("正在定位音频切点…"); err != nil {
		return err
	}
	edges, err := humanfission.Partition(pcm, points)
	if err != nil {
		return err
	}
	parts := make([]humanfission.Part, len(edges)-1)
	for i := range parts {
		if err = report(fmt.Sprintf("正在切分第 %d / %d 片…", i+1, len(parts))); err != nil {
			return err
		}
		from, to := edges[i], edges[i+1]
		key, putErr := s.putHumanFission(ctx, *job, oss, fmt.Sprintf("original-%03d-%d-%d", i+1, from, to), pcm[from:to])
		if putErr != nil {
			return putErr
		}
		parts[i] = humanfission.Part{Start: humanfission.MS(from), End: humanfission.MS(to), Object: key, Duration: humanfission.MS(to - from)}
	}
	job.State.Parts = parts
	return nil
}

func matchesFissionGuide(cues []humanfission.Cue, ms int64, before, after string) bool {
	before, after = strings.TrimSpace(before), strings.TrimSpace(after)
	if before == "" || after == "" {
		return false
	}
	for i, cue := range cues {
		if !strings.Contains(cue.Text, before) {
			continue
		}
		// The two excerpts bracket a boundary between adjacent subtitles.
		if i+1 < len(cues) && cue.End <= ms+2000 && cues[i+1].Start >= ms-2000 &&
			strings.Contains(cues[i+1].Text, after) {
			return true
		}
		// A semantic boundary can also fall inside a long subtitle.
		if index := strings.Index(cue.Text, before); index >= 0 && ms > cue.Start && ms < cue.End &&
			strings.Contains(cue.Text[index+len(before):], after) {
			return true
		}
	}
	return false
}

func transcribeHumanFissionParts(ctx context.Context, oss *storage.OSS, parts []humanfission.Part, report func(string) error) error {
	var pending []int
	for i, part := range parts {
		if part.SRT == "" {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	client := speechasr.NewFromEnv()
	if err := client.Configured(); err != nil {
		return errors.New("录音转写服务未配置")
	}
	done := len(parts) - len(pending)
	reportDone := func() error {
		return report(fmt.Sprintf("正在逐片转写，已完成 %d / %d 片…", done, len(parts)))
	}
	if err := reportDone(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	slots := make(chan struct{}, 3)
	completed := make(chan error, len(pending))
	for _, i := range pending {
		go func() {
			var err error
			defer func() { completed <- err }()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				err = ctx.Err()
				return
			}
			defer func() { <-slots }()
			part := &parts[i]
			link, e := oss.SignedURL(ctx, part.Object, 20*time.Minute)
			if e != nil {
				err = e
				return
			}
			result, e := client.Transcribe(ctx, link)
			if e != nil {
				err = fmt.Errorf("第 %d 个切片转写失败", i+1)
				return
			}
			part.SRT, e = planHumanScriptSRT(result, part.Duration)
			err = e
			if e == nil {
				rows, _ := humanfission.ReadSRT(part.SRT)
				var text strings.Builder
				for _, row := range rows {
					text.WriteString(row.Text)
				}
				part.Text = text.String()
				part.Words = fissionTimedWords(result)
			}
		}()
	}
	var failures []error
	var progressErr error
	for range pending {
		if err := <-completed; err != nil {
			failures = append(failures, err)
		} else if progressErr == nil {
			done++
			if progressErr = reportDone(); progressErr != nil {
				cancel()
			}
		}
	}
	return errors.Join(progressErr, errors.Join(failures...))
}

func fissionParts(parts []humanfission.Part) []map[string]any {
	items := make([]map[string]any, len(parts))
	for i, part := range parts {
		items[i] = map[string]any{"number": i + 1, "duration_ms": part.Duration, "text": part.Text}
	}
	return items
}

func (s *Server) planHumanFission(ctx context.Context, job *db.HumanFissionJob, facts json.RawMessage, stamp string) error {
	var plan struct {
		Plans     *[]humanfission.NamedPlan `json:"plans"`
		Exhausted *bool                     `json:"exhausted"`
	}
	history := make([][]int, 0, len(job.State.Recipes))
	for _, p := range job.State.Recipes {
		history = append(history, p.Order)
	}
	err := s.completeHumanFission(ctx, "plan", map[string]any{"original_ms": job.State.Duration, "parts": fissionParts(job.State.Parts), "facts": facts, "existing": history}, &plan)
	if err != nil {
		return err
	}
	if plan.Plans == nil || plan.Exhausted == nil {
		return errors.New("缺少组合计划或素材用尽状态")
	}
	valid := make([]humanfission.NamedPlan, 0, len(*plan.Plans))
	for _, recipe := range *plan.Plans {
		recipe.Name = strings.TrimSpace(recipe.Name)
		if humanfission.ValidName(recipe.Name) {
			valid = append(valid, recipe)
		}
	}
	added := job.State.AddNamedPlans(valid)
	if added == 0 && (!*plan.Exhausted || len(*plan.Plans) != 0) {
		return errors.New("模型这批未给出有效的新组合，请重试")
	}
	job.State.Exhausted = *plan.Exhausted
	job.State.ExhaustedAt = stamp
	return nil
}
