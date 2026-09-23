package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	storedb "livecompanion/management/internal/db"
)

const maxContractAttachmentBytes = int64(10 << 20)
const maxContractAttachmentFiles = 30

type agentLevelRequest struct {
	Code              string `json:"code"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	EntryFeeCents     uint64 `json:"entry_fee_cents"`
	IncludedDevices   uint64 `json:"included_devices"`
	DeviceDiscountBPS uint64 `json:"device_discount_bps"`
	ConsumerShareBPS  uint64 `json:"consumer_share_bps"`
	ReserveBPS        uint64 `json:"reserve_bps"`
	SettlementCycle   string `json:"settlement_cycle"`
	HoldDays          uint64 `json:"hold_days"`
	OEMEnabled        bool   `json:"oem_enabled"`
	Note              string `json:"note"`
}

type agentLevelAssignmentRequest struct {
	LevelID     int64  `json:"level_id"`
	EffectiveAt string `json:"effective_at"`
	Reason      string `json:"reason"`
}

type agentContractRequest struct {
	ExternalContractNo  string `json:"external_contract_no"`
	AgentTenantID       int64  `json:"agent_tenant_id"`
	ParentContractID    *int64 `json:"parent_contract_id,omitempty"`
	ContractType        string `json:"contract_type"`
	LevelID             *int64 `json:"level_id,omitempty"`
	StartsOn            string `json:"starts_on"`
	EndsOn              string `json:"ends_on,omitempty"`
	ContractAmountCents uint64 `json:"contract_amount_cents"`
	Note                string `json:"note"`
}

type agentContractTransitionRequest struct {
	Status string `json:"status"`
}

func (s *Server) adminAgentLevels(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "agent.view_all"); !ok {
		return
	}
	levels, err := s.store.ListAgentLevels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取代理等级失败")
		return
	}
	history, err := s.store.ListAgentLevelHistory(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取代理等级历史失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"levels": levels, "history": history})
}

func readAgentLevelInput(w http.ResponseWriter, r *http.Request) (storedb.AgentLevelInput, bool) {
	var input agentLevelRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return storedb.AgentLevelInput{}, false
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Status = strings.TrimSpace(input.Status)
	input.SettlementCycle = strings.TrimSpace(input.SettlementCycle)
	input.Note = strings.TrimSpace(input.Note)

	if input.Code == "" || len(input.Code) > 64 {
		writeError(w, http.StatusBadRequest, "等级编码不能为空且最多 64 个字符")
		return storedb.AgentLevelInput{}, false
	}
	if utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 128 {
		writeError(w, http.StatusBadRequest, "等级名称需为 1-128 个字符")
		return storedb.AgentLevelInput{}, false
	}
	if input.Status == "" {
		input.Status = "active"
	}
	if input.Status != "active" && input.Status != "inactive" {
		writeError(w, http.StatusBadRequest, "等级状态不支持")
		return storedb.AgentLevelInput{}, false
	}
	if input.DeviceDiscountBPS == 0 {
		input.DeviceDiscountBPS = 10000
	}
	if input.DeviceDiscountBPS > 10000 || input.ConsumerShareBPS > 10000 || input.ReserveBPS > 10000 {
		writeError(w, http.StatusBadRequest, "折扣、返还比例和准备金比例不能超过 100%")
		return storedb.AgentLevelInput{}, false
	}
	if input.SettlementCycle == "" {
		input.SettlementCycle = "monthly"
	}
	switch input.SettlementCycle {
	case "weekly", "monthly", "quarterly", "manual":
	default:
		writeError(w, http.StatusBadRequest, "结算周期不支持")
		return storedb.AgentLevelInput{}, false
	}
	if input.HoldDays > 3650 {
		writeError(w, http.StatusBadRequest, "留存天数不能超过 3650 天")
		return storedb.AgentLevelInput{}, false
	}
	if utf8.RuneCountInString(input.Note) > 1024 {
		writeError(w, http.StatusBadRequest, "等级说明最多 1024 个字符")
		return storedb.AgentLevelInput{}, false
	}

	return storedb.AgentLevelInput{
		Code:              input.Code,
		Name:              input.Name,
		Status:            input.Status,
		EntryFeeCents:     input.EntryFeeCents,
		IncludedDevices:   input.IncludedDevices,
		DeviceDiscountBPS: input.DeviceDiscountBPS,
		ConsumerShareBPS:  input.ConsumerShareBPS,
		ReserveBPS:        input.ReserveBPS,
		SettlementCycle:   input.SettlementCycle,
		HoldDays:          input.HoldDays,
		OEMEnabled:        input.OEMEnabled,
		Note:              input.Note,
	}, true
}

func (s *Server) adminCreateAgentLevel(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.level.manage")
	if !ok {
		return
	}
	input, ok := readAgentLevelInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.CreateAgentLevel(r.Context(), actor.UserID, input)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "等级编码已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建代理等级失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func agentCommercialPathID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(key)), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "ID 无效")
		return 0, false
	}
	return value, true
}

func (s *Server) adminUpdateAgentLevel(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.level.manage")
	if !ok {
		return
	}
	levelID, ok := agentCommercialPathID(w, r, "levelID")
	if !ok {
		return
	}
	input, ok := readAgentLevelInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.UpdateAgentLevel(r.Context(), levelID, actor.UserID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "代理等级不存在")
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "等级编码已存在")
		default:
			writeError(w, http.StatusInternalServerError, "保存代理等级失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminAssignAgentLevel(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.level.manage")
	if !ok {
		return
	}
	organizationID, ok := agentCommercialPathID(w, r, "organizationID")
	if !ok {
		return
	}
	var input agentLevelAssignmentRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if input.LevelID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择代理等级")
		return
	}
	effectiveAt := time.Now().UTC()
	if strings.TrimSpace(input.EffectiveAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(input.EffectiveAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "生效时间格式不正确")
			return
		}
		effectiveAt = parsed.UTC()
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if utf8.RuneCountInString(input.Reason) > 512 {
		writeError(w, http.StatusBadRequest, "调整原因最多 512 个字符")
		return
	}

	item, err := s.store.AssignAgentLevel(r.Context(), organizationID, input.LevelID, actor.UserID, effectiveAt, input.Reason)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "代理或等级不存在")
		case strings.Contains(err.Error(), "already uses this level"):
			writeError(w, http.StatusConflict, "代理当前已经是该等级")
		case strings.Contains(err.Error(), "not active"):
			writeError(w, http.StatusConflict, "代理或等级当前不可用")
		default:
			writeError(w, http.StatusInternalServerError, "调整代理等级失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) adminAgentContracts(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "agent.view_all"); !ok {
		return
	}
	items, err := s.store.ListAgentContracts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取代理合同失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func readAgentContractInput(w http.ResponseWriter, r *http.Request) (storedb.AgentContractInput, bool) {
	var input agentContractRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return storedb.AgentContractInput{}, false
	}
	input.ExternalContractNo = strings.TrimSpace(input.ExternalContractNo)
	input.ContractType = strings.TrimSpace(input.ContractType)
	input.StartsOn = strings.TrimSpace(input.StartsOn)
	input.EndsOn = strings.TrimSpace(input.EndsOn)
	input.Note = strings.TrimSpace(input.Note)

	if utf8.RuneCountInString(input.ExternalContractNo) > 96 {
		writeError(w, http.StatusBadRequest, "纸质合同编号最多 96 个字符")
		return storedb.AgentContractInput{}, false
	}
	if input.AgentTenantID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择代理")
		return storedb.AgentContractInput{}, false
	}
	if input.ContractType == "" {
		input.ContractType = "cooperation"
	}
	switch input.ContractType {
	case "cooperation", "renewal", "supplement":
	default:
		writeError(w, http.StatusBadRequest, "合同类型不支持")
		return storedb.AgentContractInput{}, false
	}
	startsOn, err := time.Parse("2006-01-02", input.StartsOn)
	if err != nil {
		writeError(w, http.StatusBadRequest, "合同生效日期格式不正确")
		return storedb.AgentContractInput{}, false
	}
	var endsOn *time.Time
	if input.EndsOn != "" {
		parsed, err := time.Parse("2006-01-02", input.EndsOn)
		if err != nil {
			writeError(w, http.StatusBadRequest, "合同到期日期格式不正确")
			return storedb.AgentContractInput{}, false
		}
		if parsed.Before(startsOn) {
			writeError(w, http.StatusBadRequest, "合同到期日期不能早于生效日期")
			return storedb.AgentContractInput{}, false
		}
		endsOn = &parsed
	}
	if input.LevelID != nil && *input.LevelID <= 0 {
		input.LevelID = nil
	}
	if input.ParentContractID != nil && *input.ParentContractID <= 0 {
		input.ParentContractID = nil
	}
	if utf8.RuneCountInString(input.Note) > 1024 {
		writeError(w, http.StatusBadRequest, "合同备注最多 1024 个字符")
		return storedb.AgentContractInput{}, false
	}

	return storedb.AgentContractInput{
		ExternalContractNo:  input.ExternalContractNo,
		AgentTenantID:       input.AgentTenantID,
		ParentContractID:    input.ParentContractID,
		ContractType:        input.ContractType,
		LevelID:             input.LevelID,
		StartsOn:            startsOn,
		EndsOn:              endsOn,
		ContractAmountCents: input.ContractAmountCents,
		Note:                input.Note,
	}, true
}

func (s *Server) adminCreateAgentContract(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.contract.manage")
	if !ok {
		return
	}
	input, ok := readAgentContractInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.CreateAgentContract(r.Context(), actor.UserID, input)
	if err != nil {
		switch {
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "合同编号已存在")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "代理、等级或主合同不存在")
		default:
			writeError(w, http.StatusInternalServerError, "创建代理合同失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) adminUpdateAgentContract(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.contract.manage")
	if !ok {
		return
	}
	contractID, ok := agentCommercialPathID(w, r, "contractID")
	if !ok {
		return
	}
	input, ok := readAgentContractInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.UpdateAgentContractDraft(r.Context(), contractID, actor.UserID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "合同不存在或已不能编辑")
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "合同编号已存在")
		case strings.Contains(err.Error(), "cannot be edited"):
			writeError(w, http.StatusConflict, "合同签署后不可修改，请创建续签或补充协议")
		default:
			writeError(w, http.StatusInternalServerError, "保存代理合同失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminTransitionAgentContract(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.contract.manage")
	if !ok {
		return
	}
	contractID, ok := agentCommercialPathID(w, r, "contractID")
	if !ok {
		return
	}
	var input agentContractTransitionRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Status = strings.TrimSpace(input.Status)
	switch input.Status {
	case "draft", "pending_signature", "signed", "active", "expired", "terminated", "void":
	default:
		writeError(w, http.StatusBadRequest, "目标合同状态不支持")
		return
	}
	item, err := s.store.TransitionAgentContract(r.Context(), contractID, actor.UserID, input.Status)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "合同不存在")
		case strings.Contains(err.Error(), "contract attachment required"):
			writeError(w, http.StatusConflict, "签署合同前请先上传合同扫描件")
		case strings.Contains(err.Error(), "invalid contract status transition"):
			writeError(w, http.StatusConflict, "当前合同状态不能执行该流转")
		default:
			writeError(w, http.StatusInternalServerError, "更新合同状态失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) adminUploadAgentContractAttachments(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.contract.manage")
	if !ok {
		return
	}
	contractID, ok := agentCommercialPathID(w, r, "contractID")
	if !ok {
		return
	}
	contract, err := s.store.GetAgentContract(r.Context(), contractID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "合同不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取合同失败")
		return
	}
	if contract.Status != "draft" && contract.Status != "pending_signature" {
		writeError(w, http.StatusConflict, "合同已签署或生效，原始扫描件已锁定")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxContractAttachmentBytes*maxContractAttachmentFiles+1<<20)
	if err := r.ParseMultipartForm(maxContractAttachmentBytes * maxContractAttachmentFiles); err != nil {
		writeError(w, http.StatusBadRequest, "合同扫描件过大或上传格式错误")
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "请选择合同扫描图片")
		return
	}
	if len(files) > maxContractAttachmentFiles {
		writeError(w, http.StatusBadRequest, "单次最多上传 30 张合同扫描图片")
		return
	}

	contractDir := filepath.Join(filepath.Dir(s.avatarDir), "contracts")
	if err := os.MkdirAll(contractDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "合同附件存储目录不可用")
		return
	}

	items := make([]any, 0, len(files))
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "读取合同扫描图片失败")
			return
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxContractAttachmentBytes+1))
		_ = file.Close()
		if readErr != nil || len(data) == 0 || int64(len(data)) > maxContractAttachmentBytes {
			writeError(w, http.StatusBadRequest, "单张合同扫描图片需小于 10MB")
			return
		}
		contentType := http.DetectContentType(data)
		ext := ""
		switch contentType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/webp":
			ext = ".webp"
		default:
			writeError(w, http.StatusBadRequest, "合同扫描件仅支持 JPG、PNG 或 WebP 图片")
			return
		}
		token, err := randomAvatarToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "生成合同附件文件名失败")
			return
		}
		fileName := fmt.Sprintf("contract-%d-%s%s", contractID, token, ext)
		targetPath := filepath.Join(contractDir, fileName)
		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, "保存合同扫描件失败")
			return
		}
		fileURL := "/api/v1/admin/agent-contract-files/" + fileName
		item, err := s.store.CreateAgentContractAttachment(
			r.Context(), contractID, actor.UserID, header.Filename, fileURL, contentType, uint64(len(data)),
		)
		if err != nil {
			_ = os.Remove(targetPath)
			writeError(w, http.StatusInternalServerError, "保存合同附件记录失败")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": items})
}

func (s *Server) adminAgentContractFile(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "agent.view_all"); !ok {
		return
	}
	fileName := strings.TrimSpace(r.PathValue("file"))
	if fileName == "" || filepath.Base(fileName) != fileName || !strings.HasPrefix(fileName, "contract-") {
		http.NotFound(w, r)
		return
	}
	contractDir := filepath.Join(filepath.Dir(s.avatarDir), "contracts")
	target := filepath.Join(contractDir, fileName)
	if _, err := os.Stat(target); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, target)
}

func (s *Server) adminDeleteAgentContractAttachment(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "agent.contract.manage"); !ok {
		return
	}
	contractID, ok := agentCommercialPathID(w, r, "contractID")
	if !ok {
		return
	}
	attachmentID, ok := agentCommercialPathID(w, r, "attachmentID")
	if !ok {
		return
	}
	item, err := s.store.DeleteAgentContractAttachment(r.Context(), contractID, attachmentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "合同附件不存在")
			return
		}
		if strings.Contains(err.Error(), "locked") {
			writeError(w, http.StatusConflict, "合同已签署或生效，原始扫描件不可删除")
			return
		}
		writeError(w, http.StatusInternalServerError, "删除合同附件失败")
		return
	}
	const prefix = "/api/v1/admin/agent-contract-files/"
	if strings.HasPrefix(item.FileURL, prefix) {
		fileName := strings.TrimPrefix(item.FileURL, prefix)
		if filepath.Base(fileName) == fileName {
			_ = os.Remove(filepath.Join(filepath.Dir(s.avatarDir), "contracts", fileName))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
