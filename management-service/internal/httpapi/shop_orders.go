package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) customerShopListOrders(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}

	items, err := s.store.ListCustomerShopOrders(r.Context(), *actor.TenantID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取商城订单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) customerShopGetOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	orderID, ok := shopOrderID(w, r)
	if !ok {
		return
	}

	item, err := s.store.GetCustomerShopOrder(r.Context(), *actor.TenantID, orderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "订单不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取订单失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) customerShopCreateOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}

	var input model.CreateCustomerShopOrderInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.ProductType = strings.ToLower(strings.TrimSpace(input.ProductType))
	input.MembershipCycle = strings.ToLower(strings.TrimSpace(input.MembershipCycle))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProductType == "" {
		input.ProductType = "time_card"
	}
	if input.ProductID <= 0 {
		writeError(w, http.StatusBadRequest, "商品 ID 无效")
		return
	}
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	if input.Quantity > 100 {
		writeError(w, http.StatusBadRequest, "单次购买数量不能超过 100")
		return
	}
	switch input.ProductType {
	case "time_card":
	case "membership":
		if input.MarketingCampaignID > 0 {
			if !strings.HasPrefix(input.MembershipCycle, "campaign:") {
				input.MembershipCycle = "campaign:" + strconv.FormatInt(input.MarketingCampaignID, 10)
			}
		} else {
			switch input.MembershipCycle {
			case "single_month", "recurring_month", "quarter", "half_year", "annual":
			default:
				writeError(w, http.StatusBadRequest, "会员购买周期不正确")
				return
			}
		}
		input.Quantity = 1
	case "device":
		input.RecipientName = strings.TrimSpace(input.RecipientName)
		input.RecipientPhone = strings.TrimSpace(input.RecipientPhone)
		input.Province = strings.TrimSpace(input.Province)
		input.City = strings.TrimSpace(input.City)
		input.District = strings.TrimSpace(input.District)
		input.Address = strings.TrimSpace(input.Address)
		if input.RecipientName == "" || input.RecipientPhone == "" ||
			input.Province == "" || input.City == "" ||
			input.District == "" || input.Address == "" {
			writeError(w, http.StatusBadRequest, "购买实体设备必须填写完整收货人、电话、省、市、区/县和详细地址")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "当前商品类型不支持")
		return
	}
	if utf8.RuneCountInString(input.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "幂等键过长")
		return
	}

	pendingID, err := s.audit.Begin(r.Context(), model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.order.create",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "pending",
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "审计日志服务暂不可用")
		return
	}

	item, err := s.store.CreateCustomerShopOrder(
		r.Context(),
		*actor.TenantID,
		actor.UserID,
		input,
	)
	if err != nil {
		_ = s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
			ActorUserID:    actor.UserID,
			ActorUsername:  actor.Username,
			Action:         "shop.order.create",
			TargetTenantID: *actor.TenantID,
			HTTPMethod:     r.Method,
			Path:           r.URL.Path,
			ClientIP:       requestClientIP(r),
			Result:         "failed",
		})
		switch {
		case errors.Is(err, db.ErrUnsupportedShopProduct):
			writeError(w, http.StatusBadRequest, "当前商品类型不支持")
		case errors.Is(err, db.ErrInsufficientDeviceStock):
			writeError(w, http.StatusConflict, "当前可售设备库存不足，请调整购买数量或联系管理员补充库存")
		case errors.Is(err, db.ErrMarketingCampaignStockInsufficient):
			writeError(w, http.StatusConflict, "当前活动名额或活动库存不足，请刷新活动后重试")
		case errors.Is(err, db.ErrDeviceShippingRequired):
			writeError(w, http.StatusBadRequest, "购买实体设备必须填写完整收货信息")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "商品不存在或尚未发布")
		default:
			writeError(w, http.StatusBadRequest, "创建订单失败")
		}
		return
	}

	if err := s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.order.create",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "created",
	}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "订单已创建，但审计确认暂不可用；可刷新订单列表继续")
		return
	}

	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) customerShopSandboxPayOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	orderID, ok := shopOrderID(w, r)
	if !ok {
		return
	}

	var input model.SandboxPayOrderInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.SimulateResult = strings.ToLower(strings.TrimSpace(input.SimulateResult))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.SimulateResult == "" {
		input.SimulateResult = "success"
	}
	switch input.SimulateResult {
	case "success", "failure":
	default:
		writeError(w, http.StatusBadRequest, "模拟支付结果不支持")
		return
	}
	if utf8.RuneCountInString(input.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "支付幂等键过长")
		return
	}

	pendingID, err := s.audit.Begin(r.Context(), model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.sandbox_payment",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "pending",
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "审计日志服务暂不可用")
		return
	}

	item, err := s.store.SandboxPayCustomerShopOrder(
		r.Context(),
		*actor.TenantID,
		actor.UserID,
		orderID,
		input,
	)
	if err != nil {
		result := "failed"
		status := http.StatusBadRequest
		message := "模拟支付失败"
		switch {
		case errors.Is(err, db.ErrSandboxAmountMismatch):
			result = "amount_mismatch"
			status = http.StatusConflict
			message = "输入金额与订单应付金额不一致，模拟支付未成功"
		case errors.Is(err, db.ErrSandboxPaymentFailed):
			result = "simulated_failure"
			status = http.StatusConflict
			message = "已记录一次模拟支付失败，订单仍可继续支付"
		case errors.Is(err, db.ErrShopOrderCancelled):
			result = "order_cancelled"
			status = http.StatusConflict
			message = "订单已取消，不能继续支付"
		case errors.Is(err, db.ErrShopOrderExpired):
			result = "order_expired"
			status = http.StatusConflict
			message = "订单锁库已超时，商品库存已自动释放，请重新下单"
		case errors.Is(err, db.ErrInsufficientDeviceStock):
			result = "device_stock_insufficient"
			status = http.StatusConflict
			message = "设备库存已发生变化，当前可售库存不足，请刷新商城后重新下单"
		case errors.Is(err, sql.ErrNoRows):
			result = "not_found"
			status = http.StatusNotFound
			message = "订单不存在"
		}
		_ = s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
			ActorUserID:    actor.UserID,
			ActorUsername:  actor.Username,
			Action:         "shop.sandbox_payment",
			TargetTenantID: *actor.TenantID,
			HTTPMethod:     r.Method,
			Path:           r.URL.Path,
			ClientIP:       requestClientIP(r),
			Result:         result,
		})
		writeError(w, status, message)
		return
	}

	if err := s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.sandbox_payment",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "paid",
	}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "支付与时长入账已完成，但审计确认暂不可用；请刷新订单")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"order":   item,
		"sandbox": true,
	})
}

func (s *Server) customerShopSandboxRefundOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	orderID, ok := shopOrderID(w, r)
	if !ok {
		return
	}

	var input model.SandboxRefundOrderInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.AmountCents == 0 {
		writeError(w, http.StatusBadRequest, "退款金额必须大于 0")
		return
	}
	if utf8.RuneCountInString(input.Reason) > 1024 {
		writeError(w, http.StatusBadRequest, "退款原因最多 1024 个字符")
		return
	}
	if utf8.RuneCountInString(input.IdempotencyKey) > 128 {
		writeError(w, http.StatusBadRequest, "退款幂等键过长")
		return
	}

	pendingID, err := s.audit.Begin(r.Context(), model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.sandbox_refund",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "pending",
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "审计日志服务暂不可用")
		return
	}

	order, refund, err := s.store.SandboxRefundCustomerShopOrder(
		r.Context(),
		*actor.TenantID,
		actor.UserID,
		orderID,
		input,
	)
	if err != nil {
		result := "failed"
		status := http.StatusBadRequest
		message := "模拟退款失败"
		switch {
		case errors.Is(err, db.ErrUnsupportedShopProduct):
			result = "device_after_sales_required"
			status = http.StatusConflict
			message = "实体设备订单退款需先走退货/售后流程"
		case errors.Is(err, db.ErrShopOrderNotPaid):
			result = "order_not_paid"
			status = http.StatusConflict
			message = "订单尚未支付，不能退款"
		case errors.Is(err, db.ErrShopOrderFullyRefunded):
			result = "already_refunded"
			status = http.StatusConflict
			message = "订单已全部退款"
		case errors.Is(err, db.ErrRefundAmountInvalid):
			result = "invalid_amount"
			status = http.StatusConflict
			message = "退款金额超过当前可退金额"
		case errors.Is(err, db.ErrInsufficientRefundableQuota):
			result = "quota_used"
			status = http.StatusConflict
			message = "购买时长已有部分被使用或转出，当前剩余资产不足以完成该退款"
		case errors.Is(err, sql.ErrNoRows):
			result = "not_found"
			status = http.StatusNotFound
			message = "订单或对应时长资产不存在"
		}
		_ = s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
			ActorUserID:    actor.UserID,
			ActorUsername:  actor.Username,
			Action:         "shop.sandbox_refund",
			TargetTenantID: *actor.TenantID,
			HTTPMethod:     r.Method,
			Path:           r.URL.Path,
			ClientIP:       requestClientIP(r),
			Result:         result,
		})
		writeError(w, status, message)
		return
	}

	if err := s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.sandbox_refund",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "refunded",
	}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "退款和时长冲减已完成，但审计确认暂不可用；请刷新订单")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"order":   order,
		"refund":  refund,
		"sandbox": true,
	})
}

func (s *Server) customerShopCancelOrder(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	orderID, ok := shopOrderID(w, r)
	if !ok {
		return
	}

	pendingID, err := s.audit.Begin(r.Context(), model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.order.cancel",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "pending",
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "审计日志服务暂不可用")
		return
	}

	item, err := s.store.CancelCustomerShopOrder(r.Context(), *actor.TenantID, orderID)
	if err != nil {
		_ = s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
			ActorUserID:    actor.UserID,
			ActorUsername:  actor.Username,
			Action:         "shop.order.cancel",
			TargetTenantID: *actor.TenantID,
			HTTPMethod:     r.Method,
			Path:           r.URL.Path,
			ClientIP:       requestClientIP(r),
			Result:         "failed",
		})
		switch {
		case errors.Is(err, db.ErrShopOrderAlreadyPaid):
			writeError(w, http.StatusConflict, "订单已支付，不能直接取消；后续请走退款流程")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "订单不存在")
		default:
			writeError(w, http.StatusBadRequest, "取消订单失败")
		}
		return
	}

	_ = s.audit.Complete(r.Context(), pendingID, model.AdminAuditLog{
		ActorUserID:    actor.UserID,
		ActorUsername:  actor.Username,
		Action:         "shop.order.cancel",
		TargetTenantID: *actor.TenantID,
		HTTPMethod:     r.Method,
		Path:           r.URL.Path,
		ClientIP:       requestClientIP(r),
		Result:         "cancelled",
	})
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) requireCustomerShopActor(
	w http.ResponseWriter,
	r *http.Request,
) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可访问商城订单")
		return model.Actor{}, false
	}
	return actor, true
}

func shopOrderID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("orderID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "订单 ID 无效")
		return 0, false
	}
	return value, true
}
