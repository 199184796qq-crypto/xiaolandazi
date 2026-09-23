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

var membershipCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

func (s *Server) commercialListMemberships(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(
		w,
		r,
		"commercial.membership.view",
	); !ok {
		return
	}

	items, err := s.store.ListCommercialMembershipPlans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取会员方案失败")
		return
	}
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
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "当前会员方案没有可发布的草稿")
			return
		}
		writeError(w, http.StatusInternalServerError, "发布会员方案失败")
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
	if input.DefaultTimeCardDiscountBPS > 10000 {
		writeError(w, http.StatusBadRequest, "时长卡折扣不能超过 100%")
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
