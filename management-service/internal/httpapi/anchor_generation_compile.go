package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"strings"
	"unicode/utf8"
)

func (s *Server) compileProductionAnchorStyle(ctx context.Context, tenant, style int64, sample string) (model.LiveAgentPlanAnchorStyleProfile, agentgateway.Response, error) {
	profile := model.LiveAgentPlanAnchorStyleProfile{Dimensions: []model.LiveAgentPlanAnchorStyleDimension{}, ReusableRules: []string{}, CandidatePatterns: []string{}, ExcludedFromStyle: []string{}}
	limit := model.LiveAnchorSampleLimit
	if ctx.Value(fusionAnalysisKey{}) != nil {
		limit = 1000000
	}
	if n := utf8.RuneCountInString(sample); n < 100 || n > limit {
		return profile, agentgateway.Response{}, errors.New("单份主播话术须为100到10000字，多样本总量须在100万字以内")
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return profile, agentgateway.Response{}, err
	}
	if settings.StyleProfileID == "" || settings.ScorecardProfileID == "" {
		return profile, agentgateway.Response{}, errors.New("请先在系统设置配置风格提取与评分基准模型")
	}
	analysis, analysisSource, err := loadAnchorStyleAnalysisPrompt()
	if err != nil {
		return profile, agentgateway.Response{}, err
	}
	if ctx.Value(fusionAnalysisKey{}) != nil {
		analysis, analysisSource, err = loadReplaceablePromptAsset("", "configs/prompts/anchor-style/style-multi-sample-fusion.md", "综合风格融合")
		if err != nil {
			return profile, agentgateway.Response{}, err
		}
	}
	protocol, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/style-extraction-integration.md", "风格集成协议")
	if err != nil {
		return profile, agentgateway.Response{}, err
	}
	data, _ := json.Marshal(map[string]string{"scene": "带货直播", "sample_text": sample})
	if fusion := ctx.Value(fusionAnalysisKey{}); fusion != nil {
		data, _ = json.Marshal(map[string]any{"analysis_results": fusion})
	}
	reportAnchorAnalysis(ctx, 20, "正在分析口语、称呼、句式和情绪特征")
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, StyleID: style})
	result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.StyleProfileID, "style_analysis", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: analysis + "\n\n" + protocol}, {Role: "user", Content: string(data)}}, 6500, true)
	if err != nil {
		return profile, result, err
	}
	var compiled anchorStyleWorkbenchMetaOutput
	if json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &compiled) != nil || strings.TrimSpace(compiled.MetaPromptMD) == "" {
		return profile, result, errors.New("风格返回未符合元提示词JSON协议，原始返回已保留")
	}
	for _, section := range []string{"【口语风格】", "【情绪】", "【禁用词】", "【范文锚定】"} {
		if !strings.Contains(compiled.MetaPromptMD, section) {
			return profile, result, errors.New("元提示词缺少必要风格章节，原始返回已保留")
		}
	}
	monitor := buildAnchorStyleWorkbenchMonitor(normalizeAnchorStyleWorkbenchLexicon(compiled.Monitoring, sample), sample)
	monitorJSON, _ := json.Marshal(monitor)
	reportAnchorAnalysis(ctx, 55, "语言特征已提取，正在建立对应评分基准")
	scoreMD, scoreSource, err := loadAnchorStyleScorecardPrompt()
	if err != nil {
		return profile, result, err
	}
	scoreProtocol, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/scorecard-extraction-integration.md", "评分字段分工协议")
	if err != nil {
		return profile, result, err
	}
	authority, _, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/scorecard-effective-style-integration.md", "综合风格评分分工")
	if err != nil {
		return profile, result, err
	}
	scoreData, _ := json.Marshal(map[string]string{"effective_style_analysis": compiled.MetaPromptMD, "sample_text": sample})
	baseline, _, err := s.completeAnchorStyleWorkbench(ctx, settings.ScorecardProfileID, "style_scorecard_extraction", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: scoreMD + "\n\n" + scoreProtocol + "\n\n" + authority}, {Role: "user", Content: string(scoreData)}}, 6500, false)
	if err != nil {
		return profile, result, err
	}
	canonical, err := anchorstylescore.Calibrate(stripPolicyJSONFence(baseline.Text), sample, false)
	if err != nil {
		return profile, result, err
	}
	reportAnchorAnalysis(ctx, 85, "正在按原始话术校对频次与权重，并保存分析结果")
	result.LatencyMS += baseline.LatencyMS
	profile.Summary = "主播元提示词已提取，评分基准已按原始样本程序校对并自动保存。"
	profile.Delivery = &model.LiveAnchorDeliverySpec{Version: "compiled-style-v1", MetaPromptMD: strings.TrimSpace(compiled.MetaPromptMD), Rulebook: strings.TrimSpace(compiled.MetaPromptMD), SourceSHA256: derivedHash(sample), SampleChars: utf8.RuneCountInString(sample), ScoringBaselineJSON: canonical, MonitoringJSON: string(monitorJSON), AnalysisPromptSource: analysisSource, ScorecardPromptSource: scoreSource}
	profile.ReusableRules = []string{compiled.MetaPromptMD}
	return profile, result, nil
}
