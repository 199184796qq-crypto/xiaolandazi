package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) commercialListMarketingCampaigns(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.view"); !ok {
		return
	}
	items, err := s.store.ListMarketingCampaigns(r.Context(), "", 0, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取营销活动失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) commercialCreateMarketingCampaign(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.manage")
	if !ok {
		return
	}
	input, ok := readMarketingCampaignInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.CreateMarketingCampaign(r.Context(), actor.UserID, input)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "营销活动编码已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建营销活动失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) commercialUpdateMarketingCampaign(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.manage")
	if !ok {
		return
	}
	campaignID, ok := marketingCampaignPathID(w, r)
	if !ok {
		return
	}
	input, ok := readMarketingCampaignInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.UpdateMarketingCampaign(
		r.Context(),
		campaignID,
		actor.UserID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "营销活动不存在")
		case errors.Is(err, db.ErrMarketingCampaignPendingOrders):
			writeError(w, http.StatusConflict, "该活动存在待支付订单，请等待支付完成、取消或超时释放后再修改")
		case errors.Is(err, db.ErrMarketingCampaignHistoryLocked):
			writeError(w, http.StatusConflict, "该活动已经产生订单，只能修改名称、状态和活动时间，不能再改商品组合、数量或折扣")
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "营销活动编码已存在")
		default:
			writeError(w, http.StatusInternalServerError, "保存营销活动失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialDeleteMarketingCampaign(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.manage"); !ok {
		return
	}
	campaignID, ok := marketingCampaignPathID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteMarketingCampaign(r.Context(), campaignID); err != nil {
		if errors.Is(err, db.ErrMarketingCampaignPendingOrders) {
			writeError(w, http.StatusConflict, "该活动存在待支付订单，不能删除")
			return
		}
		if errors.Is(err, db.ErrMarketingCampaignHistoryLocked) {
			writeError(w, http.StatusConflict, "该活动已有订单历史，不能删除；可以将活动停用")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "营销活动不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "删除营销活动失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readMarketingCampaignInput(
	w http.ResponseWriter,
	r *http.Request,
) (model.MarketingCampaignInput, bool) {
	var input model.MarketingCampaignInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return model.MarketingCampaignInput{}, false
	}

	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Status = strings.TrimSpace(input.Status)
	input.PricingRule = strings.TrimSpace(input.PricingRule)
	input.StartsAt = strings.TrimSpace(input.StartsAt)
	input.EndsAt = strings.TrimSpace(input.EndsAt)

	if input.Code == "" || len(input.Code) > 128 {
		writeError(w, http.StatusBadRequest, "活动编码不能为空且最多 128 个字符")
		return model.MarketingCampaignInput{}, false
	}
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 160 {
		writeError(w, http.StatusBadRequest, "活动名称需为 2-160 个字符")
		return model.MarketingCampaignInput{}, false
	}
	if utf8.RuneCountInString(input.Description) > 2000 {
		writeError(w, http.StatusBadRequest, "活动说明最多 2000 个字符")
		return model.MarketingCampaignInput{}, false
	}
	if input.Status == "" {
		input.Status = "active"
	}
	switch input.Status {
	case "active", "inactive", "draft":
	default:
		writeError(w, http.StatusBadRequest, "活动状态不支持")
		return model.MarketingCampaignInput{}, false
	}
	if input.PricingRule == "" {
		input.PricingRule = "floor_yuan"
	}
	if input.PricingRule != "floor_yuan" {
		writeError(w, http.StatusBadRequest, "当前仅支持整元结算规则")
		return model.MarketingCampaignInput{}, false
	}
	if input.StartsAt != "" {
		if _, err := time.Parse(time.RFC3339, input.StartsAt); err != nil {
			writeError(w, http.StatusBadRequest, "活动开始时间格式不正确")
			return model.MarketingCampaignInput{}, false
		}
	}
	if input.EndsAt != "" {
		if _, err := time.Parse(time.RFC3339, input.EndsAt); err != nil {
			writeError(w, http.StatusBadRequest, "活动结束时间格式不正确")
			return model.MarketingCampaignInput{}, false
		}
	}
	if input.StartsAt != "" && input.EndsAt != "" {
		start, _ := time.Parse(time.RFC3339, input.StartsAt)
		end, _ := time.Parse(time.RFC3339, input.EndsAt)
		if !end.After(start) {
			writeError(w, http.StatusBadRequest, "活动结束时间必须晚于开始时间")
			return model.MarketingCampaignInput{}, false
		}
	}
	if len(input.Items) == 0 {
		writeError(w, http.StatusBadRequest, "营销活动至少需要 1 个标的")
		return model.MarketingCampaignInput{}, false
	}
	if len(input.Items) > 100 {
		writeError(w, http.StatusBadRequest, "单个营销活动最多 100 个标的")
		return model.MarketingCampaignInput{}, false
	}
	for index := range input.Items {
		item := &input.Items[index]
		item.TargetType = strings.TrimSpace(item.TargetType)
		item.PricingMode = strings.TrimSpace(item.PricingMode)
		switch item.TargetType {
		case "membership", "time_card", "device_product":
		default:
			writeError(w, http.StatusBadRequest, "营销标的类型不正确")
			return model.MarketingCampaignInput{}, false
		}
		if item.TargetID <= 0 {
			writeError(w, http.StatusBadRequest, "请选择具体营销标的")
			return model.MarketingCampaignInput{}, false
		}
		if item.PricingMode == "" {
			item.PricingMode = "discount"
		}
		switch item.PricingMode {
		case "discount", "package", "fixed", "free":
		default:
			writeError(w, http.StatusBadRequest, "营销定价模式不正确")
			return model.MarketingCampaignInput{}, false
		}
		if item.PackageMonths == 0 {
			item.PackageMonths = 1
		}
		if item.PackageMonths > 120 {
			writeError(w, http.StatusBadRequest, "营销套餐周期不能超过 120 个月")
			return model.MarketingCampaignInput{}, false
		}
		if item.DiscountBPS > 10000 {
			writeError(w, http.StatusBadRequest, "营销折扣不能超过原价")
			return model.MarketingCampaignInput{}, false
		}
		if item.Quantity == 0 {
			item.Quantity = 1
		}
		if item.Quantity > 10000 {
			writeError(w, http.StatusBadRequest, "营销数量过大")
			return model.MarketingCampaignInput{}, false
		}
		item.SortOrder = (index + 1) * 10
		if item.PricingMode == "fixed" && (item.FixedPriceCents == nil || *item.FixedPriceCents > 10000000000) {
			writeError(w, http.StatusBadRequest, "固定售价须以分填写，范围0-1亿元")
			return input, false
		}
	}
	if err := db.ValidateMarketingControls(input.Controls, input.StartsAt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return input, false
	}
	return input, true
}

func marketingCampaignPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("campaignID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "营销活动 ID 无效")
		return 0, false
	}
	return value, true
}

func (s *Server) customerShopMarketingCampaigns(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可访问营销活动")
		return
	}
	items, err := s.store.ListMarketingCampaigns(r.Context(), "", 0, true, "shop")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取营销活动失败")
		return
	}
	if err = s.store.AnnotateCampaignEligibility(r.Context(), *actor.TenantID, items); err != nil {
		writeError(w, 500, "读取活动资格失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
