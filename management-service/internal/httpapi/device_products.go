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

var deviceProductCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)
var skuCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,95}$`)

func (s *Server) commercialListDeviceProducts(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.membership.view"); !ok {
		return
	}
	items, err := s.store.ListCommercialDeviceProducts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备商品失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) commercialCreateDeviceProduct(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	input, ok := readDeviceProductInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.CreateCommercialDeviceProduct(r.Context(), actor.UserID, input)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "设备商品编码或 SKU 已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建设备商品失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) commercialSaveDeviceProductDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	productID, ok := deviceProductID(w, r)
	if !ok {
		return
	}
	input, ok := readDeviceProductInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.SaveCommercialDeviceProductDraft(
		r.Context(),
		actor.UserID,
		productID,
		input,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "设备商品不存在")
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "设备商品编码或 SKU 已存在")
		default:
			writeError(w, http.StatusInternalServerError, "保存设备商品草稿失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialPublishDeviceProduct(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.membership.manage")
	if !ok {
		return
	}
	productID, ok := deviceProductID(w, r)
	if !ok {
		return
	}
	item, err := s.store.PublishCommercialDeviceProduct(
		r.Context(),
		actor.UserID,
		productID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "当前设备商品没有可发布草稿")
			return
		}
		writeError(w, http.StatusInternalServerError, "发布设备商品失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) customerShopDevices(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可访问商城")
		return
	}
	items, err := s.store.ListCustomerDeviceOffers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备商品失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func readDeviceProductInput(
	w http.ResponseWriter,
	r *http.Request,
) (model.CommercialDeviceInput, bool) {
	var input model.CommercialDeviceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return model.CommercialDeviceInput{}, false
	}

	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.SKUCode = strings.TrimSpace(input.SKUCode)
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)

	if !deviceProductCodePattern.MatchString(input.Code) {
		writeError(w, http.StatusBadRequest, "内部编码需为 2-64 位小写字母、数字、下划线或短横线")
		return model.CommercialDeviceInput{}, false
	}
	if !skuCodePattern.MatchString(input.SKUCode) {
		writeError(w, http.StatusBadRequest, "SKU 编码需为 2-96 位字母、数字、点、下划线或短横线")
		return model.CommercialDeviceInput{}, false
	}
	if utf8.RuneCountInString(input.Name) < 2 || utf8.RuneCountInString(input.Name) > 128 {
		writeError(w, http.StatusBadRequest, "设备商品名称需为 2-128 个字符")
		return model.CommercialDeviceInput{}, false
	}
	if utf8.RuneCountInString(input.Description) > 1024 {
		writeError(w, http.StatusBadRequest, "设备商品说明最多 1024 个字符")
		return model.CommercialDeviceInput{}, false
	}
	if input.ListPriceCents == 0 || input.ListPriceCents > 1000000000 {
		writeError(w, http.StatusBadRequest, "设备商品原价不正确")
		return model.CommercialDeviceInput{}, false
	}
	if input.SalePriceCents == 0 || input.SalePriceCents > input.ListPriceCents {
		writeError(w, http.StatusBadRequest, "设备商品售价需大于 0 且不能高于原价")
		return model.CommercialDeviceInput{}, false
	}
	return input, true
}

func deviceProductID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("productID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "设备商品 ID 无效")
		return 0, false
	}
	return value, true
}
