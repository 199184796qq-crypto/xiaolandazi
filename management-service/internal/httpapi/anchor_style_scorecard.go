package httpapi

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

var anchorStyleDeepSeek4Model = regexp.MustCompile("(?i)^deepseek[-_.]?v?4(?:[-_.]|$)")

func defaultAnchorStyleScorecardCompiler(models []anchorStyleWorkbenchModel) string {
	first := ""
	for _, item := range models {
		if item.Enabled && item.HasAPIKey && anchorStyleDeepSeek4Model.MatchString(strings.TrimSpace(item.Model)) {
			if strings.Contains(strings.ToLower(item.Model), "pro") {
				return item.ID
			}
			if first == "" {
				first = item.ID
			}
		}
	}
	if first != "" {
		return first
	}
	for _, item := range models {
		if item.Enabled && item.HasAPIKey && item.IsDefault {
			return item.ID
		}
	}
	for _, item := range models {
		if item.Enabled && item.HasAPIKey {
			return item.ID
		}
	}
	return ""
}

type anchorStyleScorecardResponse struct {
	AssetID               int64                     `json:"asset_id"`
	MetaPromptAssetID     int64                     `json:"meta_prompt_asset_id"`
	SampleSHA256          string                    `json:"sample_sha256"`
	Status                string                    `json:"status"`
	ScorecardJSON         string                    `json:"scorecard_json"`
	OriginalScorecardJSON string                    `json:"original_scorecard_json"`
	ExtractionPromptMD    string                    `json:"extraction_prompt_md"`
	PromptSource          string                    `json:"prompt_source"`
	Request               anchorStyleWorkbenchTrace `json:"request"`
	RawResponse           string                    `json:"raw_response"`
	Provider              string                    `json:"provider"`
	Model                 string                    `json:"model"`
	LatencyMS             int64                     `json:"latency_ms"`
	InputTokens           int64                     `json:"input_tokens"`
	OutputTokens          int64                     `json:"output_tokens"`
	PersistedAt           time.Time                 `json:"persisted_at"`
	ConfirmedAt           *time.Time                `json:"confirmed_at,omitempty"`
	CalibrationVersion    string                    `json:"calibration_version"`
	CalibrationError      string                    `json:"calibration_error,omitempty"`
}

func anchorStyleScorecardResponseFromAsset(a model.LiveAnchorStyleScorecardAsset) anchorStyleScorecardResponse {
	return anchorStyleScorecardResponse{
		AssetID: a.ID, MetaPromptAssetID: a.MetaPromptAssetID, SampleSHA256: a.SampleSHA256,
		Status: a.Status, ScorecardJSON: a.EffectiveScorecardJSON(), OriginalScorecardJSON: a.OriginalScorecardJSON,
		ExtractionPromptMD: a.ExtractionPromptMD, PromptSource: a.PromptSource,
		Request: anchorStyleWorkbenchTrace{System: a.RequestSystem, User: a.RequestUser}, RawResponse: a.RawResponse,
		Provider: a.Provider, Model: a.Model, LatencyMS: a.LatencyMS, InputTokens: a.InputTokens, OutputTokens: a.OutputTokens,
		PersistedAt: a.CreatedAt, ConfirmedAt: a.ConfirmedAt,
		CalibrationVersion: scorecardCalibrationVersion(a.EffectiveScorecardJSON()), CalibrationError: a.CalibrationError,
	}
}

func (s *Server) anchorStyleWorkbenchScorecardCompile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID          int64  `json:"tenant_id,omitempty"`
		SampleText        string `json:"sample_text"`
		PromptMD          string `json:"prompt_md,omitempty"`
		PromptMode        string `json:"prompt_mode,omitempty"`
		CompilerProfile   string `json:"compiler_profile_id,omitempty"`
		MetaPromptAssetID int64  `json:"meta_prompt_asset_id"`
		IncludeLe         bool   `json:"include_le"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "评分基准提取请求格式错误")
		return
	}
	input.SampleText = strings.TrimSpace(input.SampleText)
	if n := utf8.RuneCountInString(input.SampleText); n < 100 || n > 60000 {
		writeError(w, 400, "主播样本须为100到60000字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAnchorStyleMetaPromptNotFound) {
		writeError(w, 409, "请先生成或保存当前主播风格元提示词")
		return
	}
	if err != nil {
		writeError(w, 500, "读取风格资产失败")
		return
	}
	if input.MetaPromptAssetID <= 0 || input.MetaPromptAssetID != meta.ID {
		writeError(w, 409, "风格资产已变化，请刷新后重新提取基准")
		return
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(input.SampleText)))
	if meta.Provider != "manual" && meta.SampleSHA256 != hash {
		writeError(w, 409, "评分样本与风格分析样本不同，请使用同一份样本")
		return
	}
	prompt, source, err := resolveRequestPrompt(input.PromptMode, input.PromptMD, loadAnchorStyleScorecardPrompt)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if n := utf8.RuneCountInString(prompt); n < 100 || n > 60000 {
		writeError(w, 400, "基准提取提示词须为100到60000字")
		return
	}
	// Baseline extraction has its own model selection. A named selection
	// must be honored and must never silently fall back to another model.
	configuration, err := s.store.SpeechModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评分基准提取模型配置失败")
		return
	}
	choices := make([]anchorStyleWorkbenchModel, 0, len(configuration.Profiles))
	for _, p := range configuration.Profiles {
		choices = append(choices, anchorStyleWorkbenchModel{ID: p.ID, Model: p.Model, Enabled: p.Enabled, HasAPIKey: p.HasAPIKey, IsDefault: p.ID == configuration.DefaultID})
	}
	profileID, err := resolveAnchorStyleScorecardCompiler(choices, input.CompilerProfile)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	messages := []agentgateway.Message{{Role: "user", Content: renderAnchorStyleScorecardRequest(prompt, source, input.SampleText)}}
	result, actual, err := s.completeAnchorStyleWorkbench(r.Context(), profileID, "style_scorecard_extraction", agentgateway.ResponseJSON, messages, 6500, false)
	if err != nil {
		writeError(w, 502, "提取评分基准失败："+err.Error())
		return
	}
	// Preserve the received model text verbatim, even if calibration fails.
	raw := result.Text
	canonical, calibrationErr := anchorstylescore.Calibrate(stripPolicyJSONFence(raw), input.SampleText, input.IncludeLe)
	status, calibrationError := "draft", ""
	if calibrationErr != nil {
		status, calibrationError = "calibration_failed", calibrationErr.Error()
	}
	// Ensure a late model completion cannot overwrite another sample's baseline.
	current, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if err != nil || current.ID != meta.ID {
		writeError(w, 409, "分析期间风格资产已变化，当前结果未覆盖新基准")
		return
	}
	trace := traceAnchorStyleWorkbenchMessages(actual)
	asset, err := s.store.SaveLiveAnchorStyleScorecard(r.Context(), tenantID, planID, actor.UserID, model.SaveLiveAnchorStyleScorecardInput{
		MetaPromptAssetID: meta.ID, SampleSHA256: hash, SampleText: input.SampleText, ExtractionPromptMD: prompt, PromptSource: source,
		OriginalScorecardJSON: canonical, RequestSystem: trace.System, RequestUser: trace.User, RawResponse: raw,
		Provider: result.Provider, Model: result.Model, LatencyMS: result.LatencyMS, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
		Status: status, CalibrationError: calibrationError,
	})
	if err != nil {
		writeError(w, 500, "评分基准已生成，但落库失败："+err.Error())
		return
	}
	writeJSON(w, 200, anchorStyleScorecardResponseFromAsset(asset))
}

func resolveAnchorStyleScorecardCompiler(models []anchorStyleWorkbenchModel, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = defaultAnchorStyleScorecardCompiler(models)
	}
	for _, p := range models {
		if p.ID == requested && p.Enabled && p.HasAPIKey {
			return p.ID, nil
		}
	}
	return "", errors.New("选择的评分基准提取模型不可用，请选择已启用且有Key的模型")
}

func (s *Server) anchorStyleWorkbenchScorecardConfirm(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	assetID, ok := liveAnchorStylePathID(w, r, "assetID", "评分基准资产")
	if !ok {
		return
	}
	var input struct {
		TenantID      int64  `json:"tenant_id,omitempty"`
		ScorecardJSON string `json:"scorecard_json"`
		IncludeLe     *bool  `json:"include_le,omitempty"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "确认评分基准请求格式错误")
		return
	}
	if utf8.RuneCountInString(input.ScorecardJSON) > 60000 {
		writeError(w, 400, "评分基准最多60000字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 409, "当前方案没有风格资产")
		return
	}
	a, err := s.store.GetLiveAnchorStyleScorecard(r.Context(), tenantID, planID, assetID)
	if err != nil || !a.Active || a.MetaPromptAssetID != meta.ID {
		writeError(w, 409, "这份评分基准已不是当前主播版本，请重新读取")
		return
	}
	sample, err := originalScorecardSample(a, "")
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	includeLe := scorecardIncludesLe(a.EffectiveScorecardJSON())
	if input.IncludeLe != nil {
		includeLe = *input.IncludeLe
	}
	// Operator edits apply to qualitative descriptions; all numerical fields
	// are remeasured from the immutable source sample before confirmation.
	canonical, err := anchorstylescore.Calibrate(input.ScorecardJSON, sample, includeLe)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a, err = s.store.ConfirmLiveAnchorStyleScorecard(r.Context(), tenantID, planID, assetID, actor.UserID, canonical)
	if errors.Is(err, appdb.ErrLiveAnchorStyleScorecardNotFound) {
		writeError(w, http.StatusConflict, "评分基准或风格资产已变化，请重新读取")
		return
	}
	if err != nil {
		writeError(w, 500, "保存评分基准失败："+err.Error())
		return
	}
	writeJSON(w, 200, anchorStyleScorecardResponseFromAsset(a))
}

type anchorStyleScoreResponse struct {
	Score             anchorstylescore.Result   `json:"score"`
	Feedback          anchorstylescore.Feedback `json:"feedback"`
	ScorecardAssetID  int64                     `json:"scorecard_asset_id"`
	MetaPromptAssetID int64                     `json:"meta_prompt_asset_id"`
	SampleSHA256      string                    `json:"sample_sha256"`
	TextSHA256        string                    `json:"text_sha256"`
	BaselineJSON      string                    `json:"baseline_json"`
}

// Scoring and feedback drafting are deterministic; nothing is sent to a model.
func (s *Server) anchorStyleWorkbenchScore(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID          int64                `json:"tenant_id,omitempty"`
		ScorecardAssetID  int64                `json:"scorecard_asset_id"`
		MetaPromptAssetID int64                `json:"meta_prompt_asset_id"`
		GeneratedText     string               `json:"generated_text"`
		Task              anchorStyleWheelTask `json:"task"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "打分请求格式错误")
		return
	}
	text := strings.TrimSpace(input.GeneratedText)
	if n := utf8.RuneCountInString(text); n == 0 || n > 60000 {
		writeError(w, 400, "待评分话术须为1到60000字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	a, err := s.store.GetActiveLiveAnchorStyleScorecard(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAnchorStyleScorecardNotFound) || err == nil && a.Status != "confirmed" {
		writeError(w, 409, "请先确认并保存主播专属评分基准表")
		return
	}
	if err != nil {
		writeError(w, 500, "读取评分基准失败")
		return
	}
	if scorecardCalibrationVersion(a.EffectiveScorecardJSON()) != anchorstylescore.CalibrationVersion {
		writeError(w, http.StatusConflict, "请先按模型定性／程序量化分工重新校对并确认旧基准")
		return
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if err != nil || a.MetaPromptAssetID != meta.ID || input.MetaPromptAssetID != meta.ID || input.ScorecardAssetID != a.ID {
		writeError(w, 409, "主播或评分基准已变化，请刷新后重试")
		return
	}
	b, err := anchorstylescore.Parse(a.EffectiveScorecardJSON())
	if err != nil {
		writeError(w, 409, "保存的评分基准格式无效："+err.Error())
		return
	}
	score := anchorstylescore.Score(text, b, input.Task.SellingPoint+" × "+input.Task.Angle)
	feedback := buildAnchorStyleScoreFeedback(score, b)
	writeJSON(w, 200, anchorStyleScoreResponse{
		Score: score, Feedback: feedback,
		ScorecardAssetID: a.ID, MetaPromptAssetID: a.MetaPromptAssetID, SampleSHA256: a.SampleSHA256,
		TextSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(text))), BaselineJSON: a.EffectiveScorecardJSON(),
	})
}

func buildAnchorStyleScoreFeedback(score anchorstylescore.Result, b anchorstylescore.Baseline) anchorstylescore.Feedback {
	md, source, err := loadAnchorStyleScoreFeedbackPrompt()
	if err != nil {
		return anchorstylescore.Feedback{Status: "template_error", Prompt: "", Error: err.Error(), TemplateSource: source}
	}
	template, err := anchorstylescore.ParseFeedbackTemplate(md)
	if err != nil {
		return anchorstylescore.Feedback{Status: "template_error", Prompt: "", Error: err.Error(), TemplateSource: source}
	}
	feedback := anchorstylescore.BuildFeedback(score, b, template)
	feedback.TemplateSource = source
	return feedback
}
