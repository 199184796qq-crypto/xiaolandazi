package httpapi

import (
	"context"
	"errors"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type speechMinerConfirmInput struct {
	TenantID  int64                  `json:"tenant_id,omitempty"`
	Confirmed bool                   `json:"confirmed"`
	SessionID string                 `json:"session_id"`
	Facts     []speechMinerFactDraft `json:"facts"`
	History   []speechMinerMessage   `json:"history"`
}

func validateSpeechMinerConfirmation(input speechMinerConfirmInput, catalogs ...factCategoryCatalog) ([]model.LiveAgentPlanFactCandidate, error) {
	if !input.Confirmed {
		return nil, errors.New("请先核对并确认本轮事实")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9-]{8,80}$`).MatchString(input.SessionID) {
		return nil, errors.New("挖掘会话无效，请重新开始")
	}
	if len(input.Facts) == 0 || len(input.Facts) > 12 || len(input.History) > 60 {
		return nil, errors.New("请确认1到12条事实，会话过长请分批保存")
	}
	chars := 0
	for _, message := range input.History {
		chars += utf8.RuneCountInString(message.Text)
	}
	if chars > 120000 {
		return nil, errors.New("会话内容过长")
	}
	items := []model.LiveAgentPlanFactCandidate{}
	seen := map[string]bool{}
	for _, fact := range input.Facts {
		fact.Category = strings.TrimSpace(fact.Category)
		fact.Key = strings.TrimSpace(fact.Key)
		fact.Value = strings.TrimSpace(fact.Value)
		fact.SourceQuote = strings.TrimSpace(fact.SourceQuote)
		candidate := model.LiveAgentPlanFactCandidate{Category: fact.Category, Key: fact.Key, Value: fact.Value, SourceQuote: fact.SourceQuote, ReviewBucket: "adoptable"}
		if err := validateLiveAgentPlanFactCandidate(candidate, catalogs...); err != nil {
			return nil, err
		}
		if fact.Category == "" || fact.SourceQuote == "" {
			return nil, errors.New("事实必须有分类和用户原话依据")
		}
		evidence := false
		for _, message := range input.History {
			if message.Role == "user" && strings.Contains(message.Text, fact.SourceQuote) {
				evidence = true
				break
			}
		}
		if !evidence {
			return nil, errors.New("事实依据必须来自用户回答，不能采用智能体猜测")
		}
		key := fact.Category + "\x00" + fact.Key
		if seen[key] {
			return nil, errors.New("本轮事实名称重复，请先核对")
		}
		seen[key] = true
		items = append(items, candidate)
	}
	return items, nil
}

// The dedicated flow intentionally never calls initialize-anchor, saves or
// publishes a room version, or hotReloadLiveAgentPlanRooms.
func (s *Server) speechMinerConfirm(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input speechMinerConfirmInput
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "确认事实格式错误")
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	catalog, _, err := s.tenantFactCategoryCatalog(r.Context(), tenant)
	if err != nil {
		writeError(w, 500, "读取事实分类失败")
		return
	}
	facts, err := validateSpeechMinerConfirmation(input, catalog)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	w, r = anchorAnalysisStream(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()
	reportAnchorAnalysis(ctx, 10, "正在保存你确认的事实依据")
	added, err := s.store.SaveSpeechMinerFacts(ctx, tenant, plan, actor.UserID, facts, "speech-miner:guided:"+input.SessionID)
	if err != nil {
		if errors.Is(err, appdb.ErrSpeechMinerFactConflict) {
			writeError(w, 409, err.Error())
		} else if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, 404, "方案不存在")
		} else {
			writeError(w, 500, "事实保存失败，本轮未生效")
		}
		return
	}
	output := map[string]any{"facts_saved": true, "added": added, "wheel_ready": false, "published": false}
	reportAnchorAnalysis(ctx, 35, "事实已保存，正在更新事实包与风险登记")
	if err = s.refreshPlanFactRiskRegistry(ctx, tenant, plan); err != nil {
		output["notice"] = "事实依据已保存，事实包更新失败；可重试，不会重复保存或发布"
		writeJSON(w, 200, output)
		return
	}
	reportAnchorAnalysis(ctx, 55, "正在根据事实包整理并保存挖掘数据")
	points, angles, err := s.initializeSellingPointWheel(ctx, tenant, plan, actor.UserID)
	if err != nil {
		output["notice"] = "事实依据已保存，挖掘数据整理暂未完成；可重试，旧发布版本不变"
		output["error"] = err.Error()
		writeJSON(w, 200, output)
		return
	}
	wheel, err := s.store.GetActiveLiveSellingPointWheel(ctx, tenant, plan)
	if err != nil {
		output["notice"] = "挖掘数据已保存，请刷新查看"
		writeJSON(w, 200, output)
		return
	}
	output["wheel_ready"] = true
	output["wheel_asset_id"] = wheel.ID
	output["selling_points"] = points
	output["angles"] = angles
	output["notice"] = "事实依据和挖掘结果已保存，尚未发布；需另行保存方案版本并发布到直播间"
	reportAnchorAnalysis(ctx, 100, "事实依据和挖掘结果已保存，未发布到直播间")
	writeJSON(w, 200, output)
}
