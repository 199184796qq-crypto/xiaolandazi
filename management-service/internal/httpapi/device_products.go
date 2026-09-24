package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

var skuCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,95}$`)

func (s *Server) commercialListDeviceProducts(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.device.view"); !ok {
		return
	}
	items, err := s.store.ListCommercialDeviceProducts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备商品失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) commercialListDeviceSKUTypes(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.manage"); !ok {
		return
	}
	items, err := s.store.ListInventoryDeviceSKUTypes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取库存设备类型失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) commercialCreateDeviceProduct(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.manage")
	if !ok {
		return
	}
	input, ok := readDeviceProductInput(w, r)
	if !ok {
		return
	}
	item, err := s.store.CreateCommercialDeviceProduct(r.Context(), actor.UserID, input)
	if err != nil {
		switch {
		case isDuplicateDBError(err):
			writeError(w, http.StatusConflict, "该库存设备类型已经绑定其他商城商品")
		case strings.Contains(err.Error(), "inventory sku not found"):
			writeError(w, http.StatusBadRequest, "所选库存设备类型不存在，请重新选择")
		case errors.Is(err, db.ErrInvalidDeviceSalesStock):
			writeError(w, http.StatusConflict, "销售库存不能大于当前真实可用库存")
		default:
			writeError(w, http.StatusInternalServerError, "创建设备商品失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) commercialSaveDeviceProductDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.manage")
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
			writeError(w, http.StatusConflict, "该库存设备类型已经绑定其他商城商品")
		case strings.Contains(err.Error(), "inventory sku not found"):
			writeError(w, http.StatusBadRequest, "所选库存设备类型不存在，请重新选择")
		case errors.Is(err, db.ErrInvalidDeviceSalesStock):
			writeError(w, http.StatusConflict, "销售库存不能大于当前真实可用库存")
		default:
			writeError(w, http.StatusInternalServerError, "保存设备商品草稿失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialPublishDeviceProduct(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.device.listing.manage")
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

func (s *Server) commercialSetDeviceProductListing(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.device.listing.manage")
	if !ok {
		return
	}
	productID, ok := deviceProductID(w, r)
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
	item, err := s.store.SetCommercialDeviceProductStatus(r.Context(), actor.UserID, productID, input.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "设备商品不存在")
			return
		}
		if strings.Contains(err.Error(), "no published version") {
			writeError(w, http.StatusConflict, "设备商品还没有已发布版本，不能上架")
			return
		}
		writeError(w, http.StatusBadRequest, "更新设备商品上下架状态失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialArchiveDeviceProduct(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "inventory.manage")
	if !ok {
		return
	}
	productID, ok := deviceProductID(w, r)
	if !ok {
		return
	}
	item, err := s.store.SetCommercialDeviceProductStatus(r.Context(), actor.UserID, productID, "archived")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "设备商品不存在")
			return
		}
		writeError(w, http.StatusBadRequest, "删除设备商品失败")
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
	items, err := s.store.ListCustomerDeviceOffers(r.Context(), *actor.TenantID)
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
	input.ImageURL = strings.TrimSpace(input.ImageURL)
	input.UnitCode = strings.TrimSpace(input.UnitCode)
	if input.UnitCode == "" {
		input.UnitCode = "unit"
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
	if utf8.RuneCountInString(input.ImageURL) > 2048 {
		writeError(w, http.StatusBadRequest, "商品图片地址最多 2048 个字符")
		return model.CommercialDeviceInput{}, false
	}
	if utf8.RuneCountInString(input.UnitCode) > 96 {
		writeError(w, http.StatusBadRequest, "产品单位编码不正确")
		return model.CommercialDeviceInput{}, false
	}
	if input.SalesStock < 0 {
		writeError(w, http.StatusBadRequest, "销售库存不能小于 0")
		return model.CommercialDeviceInput{}, false
	}
	if input.CostPriceCents == 0 || input.CostPriceCents > 1000000000 {
		writeError(w, http.StatusBadRequest, "设备商品成本价不正确")
		return model.CommercialDeviceInput{}, false
	}
	if input.SalePriceCents == 0 || input.SalePriceCents > 1000000000 {
		writeError(w, http.StatusBadRequest, "设备商品销售价需大于 0")
		return model.CommercialDeviceInput{}, false
	}
	// The customer-facing list price must never be below the normal sale price.
	// The current editor uses the normal sale price as the list price; a future
	// marketing strike-through price can raise this field independently.
	if input.ListPriceCents < input.SalePriceCents {
		input.ListPriceCents = input.SalePriceCents
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
