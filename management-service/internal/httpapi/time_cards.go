package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

var timeCardCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

type customerTimeCardOffer struct {
	ID                 int64  `json:"id"`
	Code               string `json:"code"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	DurationSeconds    uint64 `json:"duration_seconds"`
	ValidityDays       uint32 `json:"validity_days"`
	OriginalPriceCents uint64 `json:"original_price_cents"`
	DiscountBPS        uint32 `json:"discount_bps"`
	SalePriceCents     uint64 `json:"sale_price_cents"`
	VersionNo          uint32 `json:"version_no"`
}

func (s *Server) commercialListTimeCards(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.membership.view"); !ok {
		return
	}

	items, err := s.store.ListCommercialTimeCards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取时长卡失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) commercialCreateTimeCard(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	input, ok := readTimeCardInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.CreateCommercialTimeCard(r.Context(), actor.UserID, input)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "时长卡内部编码已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建时长卡失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) commercialSaveTimeCardDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	productID, ok := timeCardProductID(w, r)
	if !ok {
		return
	}
	input, ok := readTimeCardInput(w, r)
	if !ok {
		return
	}

	item, err := s.store.SaveCommercialTimeCardDraft(
		r.Context(),
		actor.UserID,
		productID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "时长卡不存在")
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "时长卡内部编码已存在")
		default:
			writeError(w, http.StatusInternalServerError, "保存时长卡草稿失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialPublishTimeCard(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	productID, ok := timeCardProductID(w, r)
	if !ok {
		return
	}

	item, err := s.store.PublishCommercialTimeCard(r.Context(), actor.UserID, productID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "当前时长卡没有可发布草稿")
			return
		}
		writeError(w, http.StatusInternalServerError, "发布时长卡失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) customerShopTimeCards(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可访问商城")
		return
	}

	discountBPS, err := s.store.CustomerTimeCardDiscountBPS(r.Context(), *actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取终端折扣失败")
		return
	}

	items, err := s.store.ListCommercialTimeCards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取时长卡失败")
		return
	}

	offers := make([]customerTimeCardOffer, 0)
	for _, item := range items {
		if item.Status != "active" || item.ActiveVersion == nil {
			continue
		}
		version := item.ActiveVersion
		salePrice := version.PriceCents * uint64(discountBPS) / 10000
		offers = append(offers, customerTimeCardOffer{
			ID:                 item.ID,
			Code:               item.Code,
			Name:               item.Name,
			Description:        item.Description,
			DurationSeconds:    version.DurationSeconds,
			ValidityDays:       version.ValidityDays,
			OriginalPriceCents: version.PriceCents,
			DiscountBPS:        discountBPS,
			SalePriceCents:     salePrice,
			VersionNo:          version.VersionNo,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": offers})
}

func readTimeCardInput(
	w http.ResponseWriter,
	r *http.Request,
) (model.CommercialTimeCardInput, bool) {
	var input model.CommercialTimeCardInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return model.CommercialTimeCardInput{}, false
	}

	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)

	if !timeCardCodePattern.MatchString(input.Code) {
		writeError(w, http.StatusBadRequest, "内部编码需为 2-64 位小写字母、数字、下划线或短横线")
		return model.CommercialTimeCardInput{}, false
	}
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 128 {
		writeError(w, http.StatusBadRequest, "时长卡名称需为 2-128 个字符")
		return model.CommercialTimeCardInput{}, false
	}
	if utf8.RuneCountInString(input.Description) > 1024 {
		writeError(w, http.StatusBadRequest, "时长卡说明最多 1024 个字符")
		return model.CommercialTimeCardInput{}, false
	}
	if input.PriceCents > 1000000000 {
		writeError(w, http.StatusBadRequest, "时长卡价格超出允许范围")
		return model.CommercialTimeCardInput{}, false
	}
	if input.DurationSeconds == 0 || input.DurationSeconds > 20*365*24*3600 {
		writeError(w, http.StatusBadRequest, "时长范围不正确")
		return model.CommercialTimeCardInput{}, false
	}
	if input.ValidityDays == 0 || input.ValidityDays > 3650 {
		writeError(w, http.StatusBadRequest, "有效期需为 1-3650 天")
		return model.CommercialTimeCardInput{}, false
	}

	return input, true
}

func timeCardProductID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("productID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "时长卡 ID 无效")
		return 0, false
	}
	return value, true
}
