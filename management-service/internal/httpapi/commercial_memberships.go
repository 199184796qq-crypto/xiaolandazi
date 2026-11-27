package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	storedb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

var membershipCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

type commercialListingStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) commercialListMemberships(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(
		w,
		r,
		"commercial.membership.view",
	); !ok {
		return
	}

	period := r.URL.Query().Get("period")
	start, end, ok := workspaceTimeCardPeriod(time.Now(), period)
	if !ok {
		writeError(w, http.StatusBadRequest, "订阅统计时段无效")
		return
	}
	items, err := s.store.ListCommercialMembershipPlans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取会员方案失败")
		return
	}
	if period == "" {
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}

	totals, err := s.store.MembershipSubscriptionTotals(r.Context(), start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取订阅统计失败")
		return
	}
	type planWithSubscriptions struct {
		model.CommercialMembershipPlan
		storedb.MembershipSubscriptionTotal
	}
	plans := make([]planWithSubscriptions, 0, len(items))
	for _, plan := range items {
		plans = append(plans, planWithSubscriptions{plan, totals[plan.ID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": plans})
}

func (s *Server) customerShopMemberships(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可访问会员购买")
		return
	}
	items, err := s.store.ListCustomerMembershipOffers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取会员商品失败")
		return
	}
	for i := range items {
		if err = s.store.AnnotateCampaignEligibility(r.Context(), *actor.TenantID, items[i].MarketingCampaigns); err != nil {
			writeError(w, http.StatusInternalServerError, "读取活动资格失败")
			return
		}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) commercialCreateMembership(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"commercial.membership.manage",
	)
	if !ok {
		return
	}

	input, ok := readMembershipInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.CreateCommercialMembershipPlan(
		r.Context(),
		actor.UserID,
		input,
	)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "会员内部编码已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建会员方案失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) commercialSaveMembershipDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"commercial.membership.manage",
	)
	if !ok {
		return
	}

	planID, ok := commercialPlanID(w, r)
	if !ok {
		return
	}
	input, ok := readMembershipInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.SaveCommercialMembershipDraft(
		r.Context(),
		actor.UserID,
		planID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "会员方案不存在")
		case errors.Is(err, storedb.ErrMembershipArchived):
			writeError(w, http.StatusConflict, err.Error())
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "会员内部编码已存在")
		default:
			writeError(w, http.StatusInternalServerError, "保存会员草稿失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialPublishMembership(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"commercial.membership.manage",
	)
	if !ok {
		return
	}

	planID, ok := commercialPlanID(w, r)
	if !ok {
		return
	}

	item, err := s.store.PublishCommercialMembershipDraft(
		r.Context(),
		actor.UserID,
		planID,
	)
	if err != nil {
		if errors.Is(err, storedb.ErrMembershipArchived) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "当前会员方案没有可发布的草稿")
			return
		}
		writeError(w, http.StatusInternalServerError, "发布会员方案失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialSetMembershipListing(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	planID, ok := commercialPlanID(w, r)
	if !ok {
		return
	}
	var input commercialListingStatusRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status != "active" && input.Status != "inactive" {
		writeError(w, http.StatusBadRequest, "上架状态无效")
		return
	}
	item, err := s.store.SetCommercialMembershipStatus(r.Context(), actor.UserID, planID, input.Status)
	if err != nil {
		if errors.Is(err, storedb.ErrMembershipArchived) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "会员方案不存在")
			return
		}
		if strings.Contains(err.Error(), "no published version") {
			writeError(w, http.StatusConflict, "会员方案还没有已发布版本，不能上架")
			return
		}
		writeError(w, http.StatusBadRequest, "更新会员方案上下架状态失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) workspaceSaveMembership(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	var planID int64
	if r.Method == http.MethodPut {
		if planID, ok = commercialPlanID(w, r); !ok {
			return
		}
	}
	input, ok := readWorkspaceMembership(w, r)
	if !ok {
		return
	}
	if err := s.store.SaveWorkspaceMembership(r.Context(), actor.UserID, planID, input); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "会员方案不存在")
		case errors.Is(err, storedb.ErrMembershipArchived):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "保存会员方案失败")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readWorkspaceMembership(w http.ResponseWriter, r *http.Request) (storedb.WorkspaceMembershipInput, bool) {
	var input storedb.WorkspaceMembershipInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "会员方案格式无效")
		return input, false
	}
	input.Name, input.Description = strings.TrimSpace(input.Name), strings.TrimSpace(input.Description)
	switch {
	case utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 128:
		writeError(w, http.StatusBadRequest, "会员名称需为 2–128 个字符")
	case utf8.RuneCountInString(input.Description) > 1024:
		writeError(w, http.StatusBadRequest, "会员说明最多 1024 个字符")
	case input.PriceCents > 100000000:
		writeError(w, http.StatusBadRequest, "会员月费超出允许范围")
	case input.IncludedSeconds > 10*365*24*3600:
		writeError(w, http.StatusBadRequest, "基础时长超出允许范围")
	case input.TimeCardReduction > 10000 || input.DeviceReduction > 10000:
		writeError(w, http.StatusBadRequest, "会员优惠需在 0–100% 之间")
	default:
		return input, true
	}
	return input, false
}

func (s *Server) commercialArchiveMembership(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	planID, ok := commercialPlanID(w, r)
	if !ok {
		return
	}
	item, err := s.store.SetCommercialMembershipStatus(r.Context(), actor.UserID, planID, "archived")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "会员方案不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "删除会员方案失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func readMembershipInput(
	w http.ResponseWriter,
	r *http.Request,
) (model.CommercialMembershipInput, bool) {
	var input model.CommercialMembershipInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return model.CommercialMembershipInput{}, false
	}

	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.AllowAutoRenew = false
	if input.RecurringMonthDiscountBPS == 0 {
		input.RecurringMonthDiscountBPS = 10000
	}
	if input.RecurringQuarterDiscountBPS == 0 {
		input.RecurringQuarterDiscountBPS = 10000
	}
	if input.AnnualDiscountBPS == 0 {
		input.AnnualDiscountBPS = 10000
	}
	if input.DefaultTimeCardDiscountBPS == 0 {
		input.DefaultTimeCardDiscountBPS = 10000
	}
	if input.DefaultDeviceDiscountBPS == 0 {
		input.DefaultDeviceDiscountBPS = 10000
	}

	if !membershipCodePattern.MatchString(input.Code) {
		writeError(w, http.StatusBadRequest, "内部编码需为 2-64 位小写字母、数字、下划线或短横线")
		return model.CommercialMembershipInput{}, false
	}
	nameLength := utf8.RuneCountInString(input.Name)
	if nameLength < 2 || nameLength > 128 {
		writeError(w, http.StatusBadRequest, "会员名称需为 2-128 个字符")
		return model.CommercialMembershipInput{}, false
	}
	if utf8.RuneCountInString(input.Description) > 1024 {
		writeError(w, http.StatusBadRequest, "会员说明最多 1024 个字符")
		return model.CommercialMembershipInput{}, false
	}
	if input.PriceCents > 100000000 {
		writeError(w, http.StatusBadRequest, "会员月费超出允许范围")
		return model.CommercialMembershipInput{}, false
	}
	if input.IncludedSeconds > 10*365*24*3600 {
		writeError(w, http.StatusBadRequest, "基础时长超出允许范围")
		return model.CommercialMembershipInput{}, false
	}
	if input.RecurringMonthDiscountBPS > 10000 ||
		input.RecurringQuarterDiscountBPS > 10000 ||
		input.AnnualDiscountBPS > 10000 {
		writeError(w, http.StatusBadRequest, "会员连续购买折扣不能超过 100%")
		return model.CommercialMembershipInput{}, false
	}
	if input.RecurringQuarterDiscountBPS > input.RecurringMonthDiscountBPS ||
		input.AnnualDiscountBPS > input.RecurringQuarterDiscountBPS {
		writeError(w, http.StatusBadRequest, "购买月数越多，会员折扣应保持不变或更优惠")
		return model.CommercialMembershipInput{}, false
	}
	if input.DefaultTimeCardDiscountBPS > 10000 {
		writeError(w, http.StatusBadRequest, "时长卡折扣不能超过 100%")
		return model.CommercialMembershipInput{}, false
	}
	if input.DefaultDeviceDiscountBPS > 10000 {
		writeError(w, http.StatusBadRequest, "设备会员折扣不能超过 100%")
		return model.CommercialMembershipInput{}, false
	}

	return input, true
}

func commercialPlanID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(r.PathValue("planID"))
	planID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || planID <= 0 {
		writeError(w, http.StatusBadRequest, "无效的会员方案 ID")
		return 0, false
	}
	return planID, true
}

func isDuplicateDBError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "error 1062")
}
