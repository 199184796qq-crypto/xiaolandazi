package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

func (s *Server) salesSelfPerformance(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "仅销售人员可查看本人业绩")
		return
	}

	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = time.Now().Format("2006-01")
	}
	if !salesPerformancePeriodPattern.MatchString(period) {
		writeError(w, http.StatusBadRequest, "业绩周期格式应为 YYYY-MM")
		return
	}
	periodStart, err := time.ParseInLocation("2006-01", period, time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, "业绩周期无效")
		return
	}
	periodEnd := periodStart.AddDate(0, 1, 0)
	scope := model.StaffBusinessScope{
		Mode:        "self",
		ActorUserID: actor.UserID,
		ManagerView: false,
	}

	items, total, totals, err := s.store.ListSalesPerformanceScoped(
		r.Context(),
		scope,
		periodStart.UTC(),
		periodEnd.UTC(),
		"",
		"revenue-desc",
		1,
		10,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取本人销售业绩失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period":       period,
		"period_start": periodStart,
		"period_end":   periodEnd,
		"items":        items,
		"total":        total,
		"page":         1,
		"page_size":    10,
		"total_pages":  totalPages(total, 10),
		"totals":       totals,
		"scope":        scope,
	})
}

func (s *Server) salesListFollowups(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "仅销售人员可查看本人跟进记录")
		return
	}

	items, summary, err := s.store.ListSalesFollowupsByUser(r.Context(), actor.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{
				"items":   []any{},
				"summary": model.SalesFollowupSummary{},
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "读取跟进记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   items,
		"summary": summary,
	})
}

func (s *Server) salesCreateFollowup(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "仅销售人员可新增本人跟进记录")
		return
	}

	var input model.SalesFollowupInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.FollowupType = strings.ToLower(strings.TrimSpace(input.FollowupType))
	input.Content = strings.TrimSpace(input.Content)
	if input.TenantID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择客户")
		return
	}
	switch input.FollowupType {
	case "phone", "wechat", "visit", "message", "note":
	default:
		input.FollowupType = "note"
	}
	if input.Content == "" || utf8.RuneCountInString(input.Content) > 2000 {
		writeError(w, http.StatusBadRequest, "跟进内容需为 1-2000 个字符")
		return
	}

	item, err := s.store.CreateSalesFollowupByUser(r.Context(), actor.UserID, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusForbidden, "该客户不在你的负责范围内")
			return
		}
		writeError(w, http.StatusInternalServerError, "保存跟进记录失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) salesCatalog(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "仅销售人员可查看销售方案")
		return
	}

	result := model.SalesCatalog{
		Memberships: make([]model.SalesCatalogMembership, 0),
		TimeCards:   make([]model.SalesCatalogTimeCard, 0),
		Devices:     make([]model.SalesCatalogDevice, 0),
	}

	memberships, err := s.store.ListCustomerMembershipOffers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取会员方案失败")
		return
	}
	for _, item := range memberships {
		plans, err := s.store.GetCommercialMembershipPlan(r.Context(), item.ID)
		if err != nil || plans.ActiveVersion == nil {
			continue
		}
		version := plans.ActiveVersion
		result.Memberships = append(result.Memberships, model.SalesCatalogMembership{
			ID:                 item.ID,
			Code:               item.Code,
			Name:               item.Name,
			Description:        item.Description,
			PriceCents:         item.MonthlyPriceCents,
			IncludedSeconds:    item.IncludedSeconds,
			BillingPeriodUnit:  version.BillingPeriodUnit,
			BillingPeriodCount: version.BillingPeriodCount,
			AllowAutoRenew:     item.AllowAutoRenew,
			VersionNo:          item.VersionNo,
		})
	}

	timeCards, err := s.store.ListCommercialTimeCards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取时长卡方案失败")
		return
	}
	for _, item := range timeCards {
		if item.Status != "active" || item.ActiveVersion == nil {
			continue
		}
		version := item.ActiveVersion
		result.TimeCards = append(result.TimeCards, model.SalesCatalogTimeCard{
			ID:                     item.ID,
			Code:                   item.Code,
			Name:                   item.Name,
			Description:            item.Description,
			PriceCents:             version.PriceCents,
			DurationSeconds:        version.DurationSeconds,
			ValidityDays:           version.ValidityDays,
			ActivationMode:         version.ActivationMode,
			ActivationDeadlineDays: version.ActivationDeadlineDays,
			VersionNo:              version.VersionNo,
		})
	}

	devices, err := s.store.ListCommercialDeviceProducts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备方案失败")
		return
	}
	for _, item := range devices {
		if item.Status != "active" || item.ActiveVersion == nil {
			continue
		}
		version := item.ActiveVersion
		result.Devices = append(result.Devices, model.SalesCatalogDevice{
			ID:             item.ID,
			Code:           item.Code,
			SKUCode:        item.SKUCode,
			Name:           item.Name,
			Description:    item.Description,
			ImageURL:       item.ImageURL,
			UnitLabel:      item.UnitLabel,
			ListPriceCents: version.ListPriceCents,
			SalePriceCents: version.SalePriceCents,
			AvailableStock: item.AvailableStock,
			VersionNo:      version.VersionNo,
		})
	}

	writeJSON(w, http.StatusOK, result)
}
