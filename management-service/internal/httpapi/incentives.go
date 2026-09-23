package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

func (s *Server) commercialListIncentivePrograms(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.membership.view"); !ok {
		return
	}
	programType := strings.TrimSpace(r.URL.Query().Get("type"))
	if programType != "" && !validIncentiveProgramType(programType) {
		writeError(w, http.StatusBadRequest, "奖励/结算规则类型不支持")
		return
	}

	items, err := s.store.ListIncentivePrograms(r.Context(), programType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取奖励/结算规则失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) commercialCreateIncentiveProgram(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	input, ok := readIncentiveProgramInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.CreateIncentiveProgram(r.Context(), actor.UserID, input)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "规则内部编码已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建规则失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) commercialSaveIncentiveDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	programID, ok := incentivePathID(w, r, "programID")
	if !ok {
		return
	}
	input, ok := readIncentiveProgramInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.SaveIncentiveProgramDraft(
		r.Context(),
		actor.UserID,
		programID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "规则不存在")
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "规则内部编码已存在")
		default:
			writeError(w, http.StatusInternalServerError, "保存规则草稿失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialPublishIncentive(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	programID, ok := incentivePathID(w, r, "programID")
	if !ok {
		return
	}

	item, err := s.store.PublishIncentiveProgram(r.Context(), actor.UserID, programID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "当前规则没有可发布草稿")
			return
		}
		writeError(w, http.StatusInternalServerError, "发布规则失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financeSettlementDashboard(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.dashboard.view"); !ok {
		return
	}

	earnings, err := s.store.ListIncentiveEarnings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取收益明细失败")
		return
	}
	batches, err := s.store.ListSettlementBatches(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取结算批次失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"earnings": earnings,
		"batches":  batches,
	})
}

func (s *Server) financeCreateSettlementBatch(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.create")
	if !ok {
		return
	}

	var input model.CreateSettlementBatchInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.BeneficiaryType = strings.TrimSpace(input.BeneficiaryType)
	if input.BeneficiaryType != "sales_staff" && input.BeneficiaryType != "agent" {
		writeError(w, http.StatusBadRequest, "结算对象类型不支持")
		return
	}
	if input.BeneficiaryID <= 0 {
		writeError(w, http.StatusBadRequest, "结算对象 ID 无效")
		return
	}
	if input.PeriodStartAt.IsZero() || input.PeriodEndAt.IsZero() ||
		!input.PeriodEndAt.After(input.PeriodStartAt) {
		writeError(w, http.StatusBadRequest, "结算周期不正确")
		return
	}

	item, err := s.store.CreateSettlementBatch(
		r.Context(),
		actor.UserID,
		input,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "该周期没有可结算收益")
			return
		}
		writeError(w, http.StatusInternalServerError, "生成结算批次失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) financeApproveSettlementBatch(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve")
	if !ok {
		return
	}
	batchID, ok := incentivePathID(w, r, "batchID")
	if !ok {
		return
	}

	item, err := s.store.ApproveSettlementBatch(r.Context(), actor.UserID, batchID)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "maker_checker_conflict"):
			writeError(w, http.StatusConflict, "经办人不能审核自己生成的结算批次")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusBadRequest, "结算批次不存在或当前状态不能审核")
		default:
			writeError(w, http.StatusInternalServerError, "审核结算批次失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financeRejectSettlementBatch(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve"); !ok {
		return
	}
	batchID, ok := incentivePathID(w, r, "batchID")
	if !ok {
		return
	}

	item, err := s.store.RejectSettlementBatch(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "结算批次不存在或当前状态不能驳回")
			return
		}
		writeError(w, http.StatusInternalServerError, "驳回结算批次失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financePaySettlementBatch(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.settlement.pay"); !ok {
		return
	}
	batchID, ok := incentivePathID(w, r, "batchID")
	if !ok {
		return
	}

	item, err := s.store.PaySettlementBatch(r.Context(), batchID)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "结算批次不存在")
		case strings.Contains(err.Error(), "batch not approved"):
			writeError(w, http.StatusConflict, "结算批次尚未审核通过")
		default:
			writeError(w, http.StatusInternalServerError, "确认结算支付失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func readIncentiveProgramInput(
	w http.ResponseWriter,
	r *http.Request,
) (model.IncentiveProgramInput, bool) {
	var input model.IncentiveProgramInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return model.IncentiveProgramInput{}, false
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.ProgramType = strings.TrimSpace(input.ProgramType)
	input.Description = strings.TrimSpace(input.Description)

	if !timeCardCodePattern.MatchString(input.Code) {
		writeError(w, http.StatusBadRequest, "内部编码需为 2-64 位小写字母、数字、下划线或短横线")
		return model.IncentiveProgramInput{}, false
	}
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 128 {
		writeError(w, http.StatusBadRequest, "规则名称需为 2-128 个字符")
		return model.IncentiveProgramInput{}, false
	}
	if !validIncentiveProgramType(input.ProgramType) {
		writeError(w, http.StatusBadRequest, "规则类型不支持")
		return model.IncentiveProgramInput{}, false
	}
	if input.PendingDays > 3650 {
		writeError(w, http.StatusBadRequest, "冻结期最多 3650 天")
		return model.IncentiveProgramInput{}, false
	}
	if len(input.Rules) == 0 || len(input.Rules) > 50 {
		writeError(w, http.StatusBadRequest, "至少配置 1 条且最多 50 条规则")
		return model.IncentiveProgramInput{}, false
	}
	for i := range input.Rules {
		rule := &input.Rules[i]
		rule.EventType = strings.TrimSpace(rule.EventType)
		rule.ActionType = strings.TrimSpace(rule.ActionType)
		rule.ConditionsJSON = strings.TrimSpace(rule.ConditionsJSON)
		rule.ActionConfigJSON = strings.TrimSpace(rule.ActionConfigJSON)
		if rule.EventType == "" || rule.ActionType == "" {
			writeError(w, http.StatusBadRequest, "事件类型和奖励动作不能为空")
			return model.IncentiveProgramInput{}, false
		}
		if rule.ConditionsJSON == "" {
			rule.ConditionsJSON = "{}"
		}
		if rule.ActionConfigJSON == "" {
			rule.ActionConfigJSON = "{}"
		}
		var parsed any
		if err := json.Unmarshal([]byte(rule.ConditionsJSON), &parsed); err != nil {
			writeError(w, http.StatusBadRequest, "规则条件不是有效 JSON")
			return model.IncentiveProgramInput{}, false
		}
		if err := json.Unmarshal([]byte(rule.ActionConfigJSON), &parsed); err != nil {
			writeError(w, http.StatusBadRequest, "奖励动作配置不是有效 JSON")
			return model.IncentiveProgramInput{}, false
		}
	}
	return input, true
}

func validIncentiveProgramType(value string) bool {
	switch value {
	case "referral", "sales_commission", "agent_settlement":
		return true
	default:
		return false
	}
}

func incentivePathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(name)), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "ID 无效")
		return 0, false
	}
	return value, true
}

func parseDateOnly(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", value, time.Local)
}
