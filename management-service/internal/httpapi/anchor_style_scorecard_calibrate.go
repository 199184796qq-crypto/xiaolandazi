package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/model"
)

func scorecardCalibrationVersion(raw string) string {
	var root struct {
		Audit struct {
			Version string `json:"校对版本"`
		} `json:"样本统计校验"`
	}
	_ = json.Unmarshal([]byte(raw), &root)
	return root.Audit.Version
}
func scorecardIncludesLe(raw string) bool {
	b, err := anchorstylescore.Parse(raw)
	return err == nil && b.Quant.IncludeLe
}

func originalScorecardSample(a model.LiveAnchorStyleScorecardAsset, supplied string) (string, error) {
	sample := a.SampleText
	if sample == "" {
		sample = strings.TrimSpace(supplied)
	}
	if sample == "" {
		// Older assets already stored the exact rendered request. Recover only
		// the sample between known integration markers, then verify its SHA256.
		const begin = "【本次主播真实话术样本】\n"
		if _, tail, ok := strings.Cut(strings.ReplaceAll(a.RequestUser, "\r\n", "\n"), begin); ok {
			if end := strings.LastIndex(tail, "\n\n严格执行上面的通用框架"); end >= 0 {
				sample = strings.TrimSpace(tail[:end])
			}
		}
	}
	if sample == "" {
		return "", fmt.Errorf("缺少原始主播样本，请重新加载最初分析使用的全文")
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(sample))) != a.SampleSHA256 {
		return "", fmt.Errorf("样本与这份基准的原始全文不一致，不能用另一份文稿校对")
	}
	return sample, nil
}

// Recalibration reuses the saved raw model return. It does not call any model
// and writes a new draft version, preserving the previous raw and confirmed data.
func (s *Server) anchorStyleWorkbenchScorecardCalibrate(w http.ResponseWriter, r *http.Request) {
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
		TenantID   int64  `json:"tenant_id,omitempty"`
		SampleText string `json:"sample_text,omitempty"`
		IncludeLe  bool   `json:"include_le"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "校对请求格式错误")
		return
	}
	if utf8.RuneCountInString(input.SampleText) > 60000 {
		writeError(w, 400, "原始样本最多60000字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	a, err := s.store.GetLiveAnchorStyleScorecard(r.Context(), tenantID, planID, assetID)
	if err != nil || !a.Active {
		writeError(w, 409, "基准版本已变化，请刷新后重试")
		return
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if err != nil || meta.ID != a.MetaPromptAssetID {
		writeError(w, 409, "当前主播风格已变化，请重新提取评分基准")
		return
	}
	sample, err := originalScorecardSample(a, input.SampleText)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	canonical, err := anchorstylescore.Calibrate(stripPolicyJSONFence(a.RawResponse), sample, input.IncludeLe)
	if err != nil {
		writeError(w, 400, "保存的原始返回无法校对："+err.Error())
		return
	}
	next, err := s.store.SaveLiveAnchorStyleScorecard(r.Context(), tenantID, planID, actor.UserID, model.SaveLiveAnchorStyleScorecardInput{
		MetaPromptAssetID: a.MetaPromptAssetID, SampleSHA256: a.SampleSHA256, SampleText: sample,
		ExtractionPromptMD: a.ExtractionPromptMD, PromptSource: a.PromptSource,
		OriginalScorecardJSON: canonical, RequestSystem: a.RequestSystem, RequestUser: a.RequestUser,
		RawResponse: a.RawResponse, Provider: a.Provider, Model: a.Model,
		LatencyMS: a.LatencyMS, InputTokens: a.InputTokens, OutputTokens: a.OutputTokens,
	})
	if err != nil {
		writeError(w, 500, "校对结果保存失败："+err.Error())
		return
	}
	writeJSON(w, 200, anchorStyleScorecardResponseFromAsset(next))
}
