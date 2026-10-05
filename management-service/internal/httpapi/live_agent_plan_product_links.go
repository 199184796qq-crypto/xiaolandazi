package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func validateLiveAgentPlanProductLinkCandidate(candidate model.LiveAgentPlanProductLinkCandidate) error {
	if strings.TrimSpace(candidate.LinkKey) == "" {
		return errors.New("商品链接编号不能为空")
	}
	if utf8.RuneCountInString(candidate.LinkKey) > 64 {
		return errors.New("商品链接编号过长")
	}
	if strings.TrimSpace(candidate.ProductName) == "" {
		return errors.New("商品名称不能为空")
	}
	if utf8.RuneCountInString(candidate.ProductName) > 255 {
		return errors.New("商品名称不能超过255字")
	}
	if utf8.RuneCountInString(candidate.Spec) > 255 ||
		utf8.RuneCountInString(candidate.DailyPrice) > 255 ||
		utf8.RuneCountInString(candidate.Quantity) > 255 {
		return errors.New("商品规格、日常价或数量内容过长")
	}
	if utf8.RuneCountInString(candidate.Audience) > 512 {
		return errors.New("商品适用说明不能超过512字")
	}
	if len(candidate.SourceQuotes) > 20 {
		return errors.New("商品来源依据最多20条")
	}
	for _, quote := range candidate.SourceQuotes {
		if utf8.RuneCountInString(quote) > 2000 {
			return errors.New("商品来源依据单条不能超过2000字")
		}
	}
	if len(candidate.Attributes) > 40 {
		return errors.New("单个商品的个性属性最多40项")
	}
	seenAttributes := make(map[string]struct{}, len(candidate.Attributes))
	for _, attribute := range candidate.Attributes {
		if err := validateLiveAgentPlanProductAttribute(attribute); err != nil {
			return err
		}
		code := strings.ToLower(strings.TrimSpace(attribute.Code))
		if _, exists := seenAttributes[code]; exists {
			return errors.New("商品个性属性编码不能重复")
		}
		seenAttributes[code] = struct{}{}
	}
	switch strings.TrimSpace(candidate.ReviewBucket) {
	case "", "adoptable", "conflict", "discuss", "violation":
	default:
		return errors.New("商品审核分类无效")
	}
	return nil
}

func validateLiveAgentPlanProductAttribute(attribute model.LiveAgentPlanProductAttributeCandidate) error {
	code := strings.ToLower(strings.TrimSpace(attribute.Code))
	if code == "" || strings.TrimSpace(attribute.Label) == "" || strings.TrimSpace(attribute.Value) == "" {
		return errors.New("商品个性属性的编码、名称和值不能为空")
	}
	if utf8.RuneCountInString(code) > 96 || utf8.RuneCountInString(attribute.Label) > 96 {
		return errors.New("商品个性属性编码或名称过长")
	}
	if utf8.RuneCountInString(attribute.Value) > 2000 || utf8.RuneCountInString(attribute.SourceQuote) > 2000 {
		return errors.New("商品个性属性值或来源依据不能超过2000字")
	}
	if utf8.RuneCountInString(attribute.Unit) > 48 {
		return errors.New("商品个性属性单位不能超过48字")
	}
	switch code {
	case "link_key", "product_name", "spec", "daily_price", "quantity", "audience":
		return errors.New("商品个性属性不能与通用字段重复")
	}
	switch strings.ToLower(strings.TrimSpace(attribute.DisplayType)) {
	case "", "text", "tags", "price":
	default:
		return errors.New("商品个性属性展示类型无效")
	}
	return nil
}

func (s *Server) liveAgentPlanProductLinkList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	items, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式商品链接失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanProductLinkAdopt(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.AdoptLiveAgentPlanProductLinksInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "采纳商品链接格式错误")
		return
	}
	if len(input.Links) == 0 {
		writeError(w, http.StatusBadRequest, "请至少选择一条商品链接")
		return
	}
	if len(input.Links) > 30 {
		writeError(w, http.StatusBadRequest, "单次最多采纳30条商品链接")
		return
	}
	if utf8.RuneCountInString(input.SourceRef) > 255 {
		writeError(w, http.StatusBadRequest, "商品来源标识过长")
		return
	}
	for _, candidate := range input.Links {
		if err := validateLiveAgentPlanProductLinkCandidate(candidate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}

	output := model.AdoptLiveAgentPlanProductLinksOutput{Results: make([]model.LiveAgentPlanProductLinkAdoptionResult, 0, len(input.Links))}
	for _, candidate := range input.Links {
		candidate.LinkKey = canonicalPlanProductLinkKey(candidate.LinkKey)
		candidate.ProductName = strings.TrimSpace(candidate.ProductName)
		candidate.Spec = strings.TrimSpace(candidate.Spec)
		candidate.DailyPrice = strings.TrimSpace(candidate.DailyPrice)
		candidate.Quantity = strings.TrimSpace(candidate.Quantity)
		candidate.Audience = strings.TrimSpace(candidate.Audience)
		candidate.ReviewBucket = strings.TrimSpace(candidate.ReviewBucket)
		for index := range candidate.Attributes {
			candidate.Attributes[index].Code = strings.ToLower(strings.TrimSpace(candidate.Attributes[index].Code))
			candidate.Attributes[index].Label = strings.TrimSpace(candidate.Attributes[index].Label)
			candidate.Attributes[index].Value = strings.TrimSpace(candidate.Attributes[index].Value)
			candidate.Attributes[index].Unit = strings.TrimSpace(candidate.Attributes[index].Unit)
			candidate.Attributes[index].DisplayType = strings.ToLower(strings.TrimSpace(candidate.Attributes[index].DisplayType))
			candidate.Attributes[index].SourceQuote = strings.TrimSpace(candidate.Attributes[index].SourceQuote)
		}
		if candidate.ReviewBucket == "" {
			candidate.ReviewBucket = "discuss"
		}
		if candidate.ReviewBucket == "violation" {
			output.Blocked++
			output.Results = append(output.Results, model.LiveAgentPlanProductLinkAdoptionResult{
				Candidate: candidate, Status: "blocked", Message: "严重违规商品候选不能采纳为正式商品链接",
			})
			continue
		}
		if candidate.ReviewBucket == "conflict" {
			output.Conflicts++
			output.Results = append(output.Results, model.LiveAgentPlanProductLinkAdoptionResult{
				Candidate: candidate, Status: "conflict", Message: "该商品候选存在矛盾，需要先通过智能体确认后再采纳",
			})
			continue
		}
		result, err := s.store.AdoptLiveAgentPlanProductLink(
			r.Context(), tenantID, planID, actor.UserID, candidate, strings.TrimSpace(input.SourceRef),
		)
		if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
			writeError(w, http.StatusNotFound, "直播智能体方案不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "采纳商品链接失败："+err.Error())
			return
		}
		switch result.Status {
		case "adopted":
			output.Adopted++
		case "conflict":
			output.Conflicts++
		default:
			output.Skipped++
		}
		output.Results = append(output.Results, result)
	}
	writeJSON(w, http.StatusOK, output)
}

func liveAgentPlanProductAttributePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.PathValue("attributeID"))
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "商品个性属性编号无效")
		return 0, false
	}
	return id, true
}

func (s *Server) liveAgentPlanProductAttributeCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	var input model.CreateLiveAgentPlanProductAttributeInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "新增商品个性属性格式错误")
		return
	}
	candidate := model.LiveAgentPlanProductAttributeCandidate{Code: input.Code, Label: input.Label, Value: input.Value, Unit: input.Unit, DisplayType: input.DisplayType, DisplayPriority: input.DisplayPriority, SourceQuote: input.SourceQuote}
	if err := validateLiveAgentPlanProductAttribute(candidate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Label, input.Value, input.Unit = strings.TrimSpace(input.Label), strings.TrimSpace(input.Value), strings.TrimSpace(input.Unit)
	input.DisplayType, input.SourceQuote = strings.ToLower(strings.TrimSpace(input.DisplayType)), strings.TrimSpace(input.SourceQuote)
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.CreateLiveAgentPlanProductAttribute(r.Context(), tenantID, planID, productLinkID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanProductLinkNotFound) {
		writeError(w, http.StatusNotFound, "正式商品链接不存在")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "same product attribute code already exists") {
			writeError(w, http.StatusConflict, "当前商品已存在相同属性编码")
			return
		}
		writeError(w, http.StatusInternalServerError, "新增商品个性属性失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveAgentPlanProductAttributeUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	attributeID, ok := liveAgentPlanProductAttributePathID(w, r)
	if !ok {
		return
	}
	var input model.UpdateLiveAgentPlanProductAttributeInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "修改商品个性属性格式错误")
		return
	}
	candidate := model.LiveAgentPlanProductAttributeCandidate{Code: input.Code, Label: input.Label, Value: input.Value, Unit: input.Unit, DisplayType: input.DisplayType, DisplayPriority: input.DisplayPriority}
	if err := validateLiveAgentPlanProductAttribute(candidate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Label, input.Value, input.Unit = strings.TrimSpace(input.Label), strings.TrimSpace(input.Value), strings.TrimSpace(input.Unit)
	input.DisplayType = strings.ToLower(strings.TrimSpace(input.DisplayType))
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.UpdateLiveAgentPlanProductAttribute(r.Context(), tenantID, planID, productLinkID, attributeID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanProductAttributeNotFound) {
		writeError(w, http.StatusNotFound, "商品个性属性不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanProductAttributeVersionConflict) {
		writeError(w, http.StatusConflict, "商品个性属性已经产生新版本，请刷新后重新修改")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "same product attribute code already exists") {
			writeError(w, http.StatusConflict, "当前商品已存在相同属性编码")
			return
		}
		writeError(w, http.StatusInternalServerError, "修改商品个性属性失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveAgentPlanProductAttributeDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	attributeID, ok := liveAgentPlanProductAttributePathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	err := s.store.DeleteLiveAgentPlanProductAttribute(r.Context(), tenantID, planID, productLinkID, attributeID, actor.UserID)
	if errors.Is(err, appdb.ErrLiveAgentPlanProductAttributeNotFound) {
		writeError(w, http.StatusNotFound, "商品个性属性不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除商品个性属性失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func liveAgentPlanProductLinkPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value := strings.TrimSpace(r.PathValue("productLinkID"))
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "商品链接记录编号无效")
		return 0, false
	}
	return id, true
}

func (s *Server) liveAgentPlanProductLinkUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}

	var input model.UpdateLiveAgentPlanProductLinkInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "修改商品链接格式错误")
		return
	}
	candidate := model.LiveAgentPlanProductLinkCandidate{
		LinkKey:      strings.TrimSpace(input.LinkKey),
		ProductName:  strings.TrimSpace(input.ProductName),
		Spec:         strings.TrimSpace(input.Spec),
		DailyPrice:   strings.TrimSpace(input.DailyPrice),
		Quantity:     strings.TrimSpace(input.Quantity),
		Audience:     strings.TrimSpace(input.Audience),
		ReviewBucket: "adoptable",
	}
	if err := validateLiveAgentPlanProductLinkCandidate(candidate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.LinkKey = canonicalPlanProductLinkKey(candidate.LinkKey)
	input.ProductName = candidate.ProductName
	input.Spec = candidate.Spec
	input.DailyPrice = candidate.DailyPrice
	input.Quantity = candidate.Quantity
	input.Audience = candidate.Audience

	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	updated, err := s.store.UpdateLiveAgentPlanProductLink(r.Context(), tenantID, planID, productLinkID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanProductLinkNotFound) {
		writeError(w, http.StatusNotFound, "正式商品链接不存在")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanProductLinkVersionConflict) {
		writeError(w, http.StatusConflict, "商品链接已经产生新版本，请刷新后重新修改")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "same product link key already exists") {
			writeError(w, http.StatusConflict, "当前方案已经存在相同链接编号")
			return
		}
		writeError(w, http.StatusInternalServerError, "修改正式商品链接失败")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) liveAgentPlanProductLinkDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if err := s.store.DeleteLiveAgentPlanProductLink(r.Context(), tenantID, planID, productLinkID, actor.UserID); errors.Is(err, appdb.ErrLiveAgentPlanProductLinkNotFound) {
		writeError(w, http.StatusNotFound, "正式商品链接不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "删除正式商品链接失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
