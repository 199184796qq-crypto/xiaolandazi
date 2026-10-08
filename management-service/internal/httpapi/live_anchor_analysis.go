package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type anchorAnalysisSection struct {
	Title  string   `json:"title"`
	Points []string `json:"points"`
}

func renderAnchorAnalysisSections(sections []anchorAnalysisSection) (string, error) {
	if len(sections) == 0 || len(sections) > 30 {
		return "", errors.New("请至少保留一个分析大项，最多30项")
	}
	var out strings.Builder
	total := 0
	for _, section := range sections {
		title := strings.TrimSpace(section.Title)
		if title == "" || utf8.RuneCountInString(title) > 60 || strings.ContainsAny(title, "\r\n【】") {
			return "", errors.New("分析大项标题无效")
		}
		out.WriteString("【" + title + "】\n")
		for i, point := range section.Points {
			point = strings.TrimSpace(point)
			if point == "" || utf8.RuneCountInString(point) > 10000 {
				return "", errors.New("分析小项不能为空，最多10000字")
			}
			total++
			out.WriteString(strconv.Itoa(i+1) + ". " + point + "\n")
		}
		out.WriteString("\n")
	}
	if total == 0 || total > 300 || out.Len() > 300000 {
		return "", errors.New("请至少保留一个分析小项，总内容过长请精简")
	}
	return strings.TrimSpace(out.String()), nil
}
func writeAnchorAnalysisError(w http.ResponseWriter, err error) {
	if errors.Is(err, appdb.ErrAnchorAnalysisConflict) {
		writeError(w, 409, err.Error())
		return
	}
	if errors.Is(err, appdb.ErrAnchorAnalysisVersionNotFound) || errors.Is(err, appdb.ErrLiveAnchorStyleNotFound) {
		writeError(w, 404, "分析版本或主播不存在")
		return
	}
	writeError(w, 500, "分析未保存，原采纳版本不变，请刷新重试")
}
func (s *Server) liveAnchorAnalysisVersions(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.liveAnchorStyleTenant(w, r, false)
	if !ok {
		return
	}
	style, ok := liveAnchorStylePathID(w, r, "styleID", "主播")
	if !ok {
		return
	}
	state, err := s.store.LiveAnchorAnalysisState(r.Context(), tenant, style)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	writeJSON(w, 200, state)
}
func (s *Server) liveAnchorAnalysisUse(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	style, ok := liveAnchorStylePathID(w, r, "styleID", "主播")
	if !ok {
		return
	}
	version, ok := liveAnchorStylePathID(w, r, "versionID", "版本")
	if !ok {
		return
	}
	var input struct {
		ExpectedActiveID int64 `json:"expected_active_id"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "版本输入无效")
		return
	}
	if err := s.store.UseLiveAnchorAnalysisVersion(r.Context(), tenant, style, version, actor.UserID, input.ExpectedActiveID); err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	s.anchorAnalysisComplete(w, r, tenant, style)
}
func (s *Server) anchorAnalysisComplete(w http.ResponseWriter, r *http.Request, tenant, style int64) {
	item, err := s.store.GetLiveAnchorStyle(r.Context(), tenant, style)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	reportAnchorAnalysis(r.Context(), 100, "综合风格和对应评分基准已保存并采纳")
	writeJSON(w, 200, map[string]any{"style": item})
}
func analyzedAnchorCorpus(samples []model.LiveAnchorStyleSample) (string, []int64, []map[string]any, error) {
	samples = append([]model.LiveAnchorStyleSample(nil), samples...)
	sort.Slice(samples, func(i, j int) bool { return samples[i].ID < samples[j].ID })
	parts := []string{}
	ids := []int64{}
	analyses := []map[string]any{}
	for _, sample := range samples {
		if sample.AnalysisStatus != "analyzed" || sample.Analysis.Delivery == nil || sample.Analysis.Delivery.MetaPromptMD == "" {
			return "", nil, nil, errors.New("有尚未完成分析的样本，请先分析每份样本")
		}
		text, _ := model.TruncateAnchorSample(sample.ReadableText)
		parts = append(parts, text)
		ids = append(ids, sample.ID)
		analyses = append(analyses, map[string]any{"sample_id": sample.ID, "analysis": sample.Analysis.Delivery.MetaPromptMD})
	}
	if len(ids) < 2 {
		return "", nil, nil, errors.New("多样本分析至少需要两份已分析话术")
	}
	corpus := strings.Join(parts, "\n\n")
	raw, _ := json.Marshal(analyses)
	if len(raw) > 1000000 || utf8.RuneCountInString(corpus) > 1000000 {
		return "", nil, nil, errors.New("本次综合分析内容过多，请减少样本后重试；未忽略任何样本")
	}
	return corpus, ids, analyses, nil
}
func (s *Server) liveAnchorAnalysisFuse(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	style, ok := liveAnchorStylePathID(w, r, "styleID", "主播")
	if !ok {
		return
	}
	state, err := s.store.LiveAnchorAnalysisState(r.Context(), tenant, style)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	samples, err := s.store.ListLiveAnchorStyleSamples(r.Context(), tenant, style)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	corpus, ids, analyses, err := analyzedAnchorCorpus(samples)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	w, r = anchorAnalysisStream(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 480*time.Second)
	defer cancel()
	reportAnchorAnalysis(ctx, 5, "正在读取同一主播的全部样本分析")
	ctx = context.WithValue(ctx, fusionAnalysisKey{}, analyses)
	profile, _, err := s.compileProductionAnchorStyle(ctx, tenant, style, corpus)
	if err != nil {
		writeError(w, 502, "综合分析失败，原版本不变："+err.Error())
		return
	}
	latest, err := s.store.ListLiveAnchorStyleSamples(ctx, tenant, style)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	_, _, latestAnalyses, err := analyzedAnchorCorpus(latest)
	raw, _ := json.Marshal(analyses)
	after, _ := json.Marshal(latestAnalyses)
	if err != nil || string(raw) != string(after) {
		writeError(w, 409, "分析期间样本已改变，本次未覆盖综合版本，请重新分析")
		return
	}
	_, err = s.store.SaveLiveAnchorAnalysisVersion(ctx, tenant, style, actor.UserID, state.ActiveVersionID, "multi_sample", ids, corpus, profile)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	s.anchorAnalysisComplete(w, r, tenant, style)
}
func (s *Server) scoreEditedAnchorAnalysis(ctx context.Context, tenant, style int64, previous model.LiveAnchorAnalysisVersion, md string) (model.LiveAgentPlanAnchorStyleProfile, error) {
	var profile model.LiveAgentPlanAnchorStyleProfile
	raw, _ := json.Marshal(previous.Profile)
	_ = json.Unmarshal(raw, &profile)
	if profile.Delivery == nil || previous.SourceText == "" {
		return profile, errors.New("本版本缺少原始话术快照，请先重新分析样本")
	}
	cfg, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return profile, err
	}
	score, source, err := loadAnchorStyleScorecardPrompt()
	if err != nil {
		return profile, err
	}
	protocol, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/scorecard-extraction-integration.md", "评分分工")
	if err != nil {
		return profile, err
	}
	authority, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/scorecard-effective-style-integration.md", "综合风格评分分工")
	if err != nil {
		return profile, err
	}
	data, _ := json.Marshal(map[string]string{"effective_style_analysis": md, "sample_text": previous.SourceText})
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, StyleID: style})
	reportAnchorAnalysis(ctx, 30, "正在更新修改内容对应的评分特征")
	response, _, err := s.completeAnchorStyleWorkbench(ctx, cfg.ScorecardProfileID, "style_scorecard_extraction", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: score + "\n\n" + protocol + "\n\n" + authority}, {Role: "user", Content: string(data)}}, 6500, false)
	if err != nil {
		return profile, err
	}
	canonical, err := anchorstylescore.Calibrate(stripPolicyJSONFence(response.Text), previous.SourceText, false)
	if err != nil {
		return profile, err
	}
	return mapEditedAnchorAnalysisProfile(profile, md, canonical, source)
}

func mapEditedAnchorAnalysisProfile(profile model.LiveAgentPlanAnchorStyleProfile, md, canonical, source string) (model.LiveAgentPlanAnchorStyleProfile, error) {
	if profile.Delivery == nil {
		return profile, errors.New("风格文件缺少格式映射信息")
	}
	profile.Delivery.MetaPromptMD = md
	profile.Delivery.Rulebook = md
	profile.Delivery.ScoringBaselineJSON = canonical
	profile.Delivery.ScorecardPromptSource = source
	profile.ReusableRules = []string{md}
	var monitor anchorStyleWorkbenchMonitor
	if strings.TrimSpace(profile.Delivery.MonitoringJSON) != "" {
		if err := json.Unmarshal([]byte(profile.Delivery.MonitoringJSON), &monitor); err != nil {
			return profile, errors.New("风格监测文件格式无效，未覆盖原文件")
		}
	}
	terms := monitor.Terms[:0]
	for _, term := range monitor.Terms {
		if strings.Contains(md, term.Text) {
			terms = append(terms, term)
		}
	}
	monitor.Terms = terms
	forbidden := []string{}
	for _, term := range monitor.ForbiddenTerms {
		if strings.Contains(md, term) {
			forbidden = append(forbidden, term)
		}
	}
	monitor.ForbiddenTerms = forbidden
	raw, _ := json.Marshal(monitor)
	profile.Delivery.MonitoringJSON = string(raw)
	return profile, nil
}
func (s *Server) liveAnchorAnalysisEdit(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	style, ok := liveAnchorStylePathID(w, r, "styleID", "主播")
	if !ok {
		return
	}
	var input struct {
		ExpectedActiveID int64                   `json:"expected_active_id"`
		Sections         []anchorAnalysisSection `json:"sections"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "分析结构无效")
		return
	}
	md, err := renderAnchorAnalysisSections(input.Sections)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	previous, err := s.store.EditableLiveAnchorAnalysis(r.Context(), tenant, style)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	if input.ExpectedActiveID != previous.ID {
		writeAnchorAnalysisError(w, appdb.ErrAnchorAnalysisConflict)
		return
	}
	previousSHA := appdb.AnchorAnalysisProfileSHA(previous.Profile)
	w, r = anchorAnalysisStream(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	reportAnchorAnalysis(ctx, 5, "正在保存分析结构和修改内容")
	profile, err := s.scoreEditedAnchorAnalysis(ctx, tenant, style, previous, md)
	if err != nil {
		writeError(w, 502, "修改未生效，原版本保留："+err.Error())
		return
	}
	reportAnchorAnalysis(ctx, 85, "评分校对完成，正在保存新的综合版本")
	_, err = s.store.SaveLiveAnchorAnalysisVersion(ctx, tenant, style, actor.UserID, previous.ID, "manual_edit", previous.SampleIDs, previous.SourceText, profile, previousSHA)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	s.anchorAnalysisComplete(w, r, tenant, style)
}
func (s *Server) liveAnchorDescription(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.liveAnchorStyleTenant(w, r, true)
	if !ok {
		return
	}
	style, ok := liveAnchorStylePathID(w, r, "styleID", "主播")
	if !ok {
		return
	}
	var input struct {
		Description string `json:"description"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "简介格式无效")
		return
	}
	text := strings.TrimSpace(input.Description)
	if len([]rune(text)) > 30 {
		text = string([]rune(text)[:30])
	}
	item, err := s.store.UpdateAnchorDescription(r.Context(), tenant, style, actor.UserID, text)
	if err != nil {
		writeAnchorAnalysisError(w, err)
		return
	}
	writeJSON(w, 200, item)
}
