package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

const customerAgentFactEditPath = customerAgentChatPath + "/fact-edits"

var (
	agentFactEditCommand = regexp.MustCompile(`^(?:请(?:帮我)?[ \t]*)?(?:修改|更改|更新|将|把)[ \t]*(?:第)?([0-9０-９]+)[ \t]*号[ \t]*链接[ \t]*(?:的[ \t]*)?([^:=：\r\n]{1,50}?)[ \t]*(?:修改为|改为|改成|设为|为|=)[ \t]*([^\r\n]+)$`)
	// Confirmation tokens are scoped to this Management process and expire.
	agentFactEditKey      = []byte(rand.Text())
	errAgentFactEditStale = errors.New("事实清单或对象已变化，请重新发送修改指令并核对")
)

type agentFactEdit struct {
	linkNumber int64
	attribute  string
	value      string
}

type agentFactEditGrant struct {
	ActorID, TenantID, PlanID int64
	ConversationID            string
	Library, Objects          string
	Message                   string
	ExpiresAt                 int64
}

type agentFactEditPreview struct {
	Link      string `json:"link"`
	Attribute string `json:"attribute"`
	OldValue  string `json:"old_value"`
	NewValue  string `json:"new_value"`
	OldSource string `json:"old_source"`
	NewSource string `json:"new_source"`
	Token     string `json:"confirmation_token"`
}

func parseAgentFactEdit(message string) (agentFactEdit, bool) {
	match := agentFactEditCommand.FindStringSubmatch(strings.TrimSpace(message))
	if match == nil {
		return agentFactEdit{}, false
	}
	number, _ := strconv.ParseInt(materialHeadingKey(match[1]), 10, 64)
	edit := agentFactEdit{number, strings.TrimSpace(match[2]), strings.TrimSpace(match[3])}
	return edit, edit.linkNumber > 0 && edit.attribute != "" && edit.value != ""
}

func agentFactAttributeKey(name string) string {
	key := strings.ToLower(materialHeadingKey(name))
	switch key {
	case "品名", "商品名", "商品名称", "产品名称", "product_name":
		return "商品名称"
	}
	return key
}

func findAgentFactEdit(edit agentFactEdit, claims []model.PlanMaterialClaim, catalog model.MaterialObjectCatalog, links []db.QALink) (model.PlanMaterialClaim, error) {
	var linkID int64
	for _, link := range links {
		number := materialSourceNumberedLink.FindStringSubmatch(materialHeadingKey(link.LinkKey))
		if number == nil || materialInt64(number[1]) != edit.linkNumber || slices.Contains(catalog.Directory.RemovedLinkIDs, link.ID) {
			continue
		}
		if linkID != 0 {
			return model.PlanMaterialClaim{}, errors.New("链接编号对应多个对象，请先核对对象目录")
		}
		linkID = link.ID
	}
	if linkID == 0 {
		return model.PlanMaterialClaim{}, errors.New("当前方案没有该编号的商品链接")
	}
	field := agentFactAttributeKey(edit.attribute)
	var found model.PlanMaterialClaim
	for _, claim := range claims {
		if claim.Destination != "fact" || (field != agentFactAttributeKey(claim.Attribute) &&
			field != agentFactAttributeKey(claim.Topic) && field != agentFactAttributeKey(claim.ClaimKey)) {
			continue
		}
		matches := slices.ContainsFunc(claim.ObjectTargets, func(target model.MaterialObjectTarget) bool {
			return target.Kind == "product" && target.LinkID == linkID
		})
		if len(claim.ObjectTargets) == 0 {
			matches = claim.ScopeKind == "product" && claim.ProductLinkID == linkID
		}
		if !matches {
			continue
		}
		if len(claim.ObjectTargets) > 1 || found.ID != 0 {
			return model.PlanMaterialClaim{}, errors.New("这项事实有多个匹配或由多个对象共用，请到事实清单选定要改的条目")
		}
		found = claim
	}
	if found.ID == 0 {
		return found, errors.New("在该链接下没有找到对应的事实")
	}
	return found, nil
}

func replaceAgentFactValue(text, old, value string) (string, bool) {
	if old == "" || strings.Count(text, old) != 1 {
		return text, false
	}
	before, after, _ := strings.Cut(text, old)
	left, _ := utf8.DecodeLastRuneInString(before)
	right, _ := utf8.DecodeRuneInString(after)
	first, _ := utf8.DecodeRuneInString(old)
	last, _ := utf8.DecodeLastRuneInString(old)
	// A value such as 9 must not be substituted into 19 or 9.9.
	if unicode.IsDigit(first) && (unicode.IsDigit(left) || left == '.' || left == '．') ||
		unicode.IsDigit(last) && (unicode.IsDigit(right) || right == '.' || right == '．') {
		return text, false
	}
	return before + value + after, true
}

func prepareAgentFactEdit(claim model.PlanMaterialClaim, edit agentFactEdit) (model.PlanMaterialClaimInput, agentFactEditPreview, error) {
	next := db.MaterialClaimInput(claim)
	old := strings.TrimSpace(claim.Value)
	if old == "" {
		old = strings.TrimSpace(claim.Content)
	}
	if old == edit.value {
		return next, agentFactEditPreview{}, errors.New("这项事实已是这个值")
	}
	source := claim.SourceSentence
	if source == "" {
		source = claim.SourceQuote
	}
	corrected, ok := replaceAgentFactValue(source, old, edit.value)
	if !ok {
		return next, agentFactEditPreview{}, errors.New("无法在原句中唯一定位旧值，请到事实清单核对原句")
	}
	content, ok := replaceAgentFactValue(claim.Content, old, edit.value)
	if !ok {
		return next, agentFactEditPreview{}, errors.New("事实正文和旧值无法对应，请到事实清单核对正文")
	}
	next.Value, next.Content, next.SourceSentence = edit.value, content, corrected
	if quote, ok := replaceAgentFactValue(claim.SourceQuote, old, edit.value); ok {
		next.SourceQuote = quote
	}
	if err := bindEditedMaterialSource(&next); err != nil {
		return next, agentFactEditPreview{}, err
	}
	if err := validateMaterialClaim(next); err != nil {
		return next, agentFactEditPreview{}, err
	}
	label := claim.Attribute
	if label == "" {
		label = edit.attribute
	}
	return next, agentFactEditPreview{
		Link: strconv.FormatInt(edit.linkNumber, 10) + "号链接", Attribute: label,
		OldValue: old, NewValue: edit.value, OldSource: source, NewSource: next.SourceSentence,
	}, nil
}

func signAgentFactEdit(grant agentFactEditGrant) string {
	payload, _ := json.Marshal(grant)
	mac := hmac.New(sha256.New, agentFactEditKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(mac.Sum(nil))
}

func verifyAgentFactEdit(token string, actor, tenant, plan int64, conversation string, now time.Time) (agentFactEditGrant, bool) {
	var grant agentFactEditGrant
	encoded, hexMAC, _ := strings.Cut(token, ".")
	payload, payloadErr := base64.RawURLEncoding.DecodeString(encoded)
	signature, macErr := hex.DecodeString(hexMAC)
	mac := hmac.New(sha256.New, agentFactEditKey)
	mac.Write(payload)
	ok := payloadErr == nil && macErr == nil && hmac.Equal(signature, mac.Sum(nil)) &&
		json.Unmarshal(payload, &grant) == nil && grant.ExpiresAt > now.Unix() && grant.ActorID == actor &&
		grant.TenantID == tenant && grant.PlanID == plan && grant.ConversationID == conversation
	return grant, ok
}

func (s *Server) previewCustomerAgentFactEdit(w http.ResponseWriter, r *http.Request, actor, tenant, plan int64, conversation, message string, edit agentFactEdit) {
	claims, catalog, links, err := s.store.PlanRewriteFactsSnapshot(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前方案的事实失败")
		return
	}
	claim, err := findAgentFactEdit(edit, claims, catalog, links)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	_, preview, err := prepareAgentFactEdit(claim, edit)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	preview.Token = signAgentFactEdit(agentFactEditGrant{
		ActorID: actor, TenantID: tenant, PlanID: plan, ConversationID: conversation, Message: message,
		Library: db.MaterialLibraryRevision(claims), Objects: qapair.Hash([]any{catalog, links}),
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	})
	writeJSON(w, http.StatusOK, map[string]any{"fact_edit": preview})
}

func (s *Server) customerAgentFactEditAction(w http.ResponseWriter, r *http.Request) {
	actor, tenant, scopedPlan, ok := s.customerAgentActor(w, r)
	if !ok {
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	var input struct {
		Plan         int64  `json:"plan_id"`
		Conversation string `json:"conversation_id"`
		Message      string `json:"message"`
		Token        string `json:"confirmation_token"`
	}
	// A signed message includes base64 overhead and JSON escaping.
	if err := readJSONWithLimit(w, r, &input, 128<<10); err != nil {
		writeError(w, http.StatusBadRequest, "事实修改请求格式无效")
		return
	}
	if input.Plan <= 0 || len(input.Conversation) != 32 {
		writeError(w, http.StatusBadRequest, "请刷新方案页面后再提交")
		return
	}
	if scopedPlan != 0 && scopedPlan != input.Plan {
		writeError(w, http.StatusForbidden, "协助仅限客户授权的当前方案")
		return
	}
	if scopedPlan == 0 {
		tenant, ok = s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
		if !ok {
			return
		}
	}
	if r.URL.Path == customerAgentFactEditPath+"/preview" {
		edit, ok := parseAgentFactEdit(input.Message)
		if !ok || utf8.RuneCountInString(input.Message) > 4000 {
			writeError(w, http.StatusBadRequest, "请按「修改 1号链接 品名 为 新品名」发送")
			return
		}
		s.previewCustomerAgentFactEdit(w, r, actor.UserID, tenant, input.Plan, input.Conversation, input.Message, edit)
		return
	}
	grant, ok := verifyAgentFactEdit(input.Token, actor.UserID, tenant, input.Plan, input.Conversation, time.Now())
	if !ok {
		writeError(w, http.StatusConflict, "确认已失效，请重新发送修改指令")
		return
	}
	s.confirmCustomerAgentFactEdit(w, r, grant)
}

func (s *Server) confirmCustomerAgentFactEdit(w http.ResponseWriter, r *http.Request, grant agentFactEditGrant) {
	ctx := r.Context()
	claims, catalog, links, err := s.store.PlanRewriteFactsSnapshot(ctx, grant.TenantID, grant.PlanID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前方案的事实失败")
		return
	}
	if db.MaterialLibraryRevision(claims) != grant.Library || qapair.Hash([]any{catalog, links}) != grant.Objects {
		writeError(w, http.StatusConflict, errAgentFactEditStale.Error())
		return
	}
	edit, _ := parseAgentFactEdit(grant.Message)
	claim, err := findAgentFactEdit(edit, claims, catalog, links)
	if err != nil {
		writeError(w, http.StatusConflict, errAgentFactEditStale.Error())
		return
	}
	input, _, err := prepareAgentFactEdit(claim, edit)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	modelName, vector, status, err := s.materialClaimEditVector(ctx, claim, input)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	if err := s.store.CommitEditedPlanMaterialClaim(ctx, grant.TenantID, grant.PlanID, claim.ID, input, modelName, vector, grant.Library, grant.Objects); err != nil {
		if errors.Is(err, db.ErrMaterialClaimConflict) || errors.Is(err, db.ErrMaterialClaimNotFound) {
			writeError(w, http.StatusConflict, errAgentFactEditStale.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "事实保存失败，请重试")
		}
		return
	}
	s.scheduleFactCardRebuild(grant.TenantID, grant.PlanID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
