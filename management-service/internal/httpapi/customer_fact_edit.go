package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

const customerFactEditPath = "/api/v1/customer/fact-edits"

var agentFactEditID = regexp.MustCompile("^([0-9a-f]{32})$")

type agentFactMentionChanges struct {
	Attribute string `json:"new_attribute,omitempty"`
	Value     string `json:"new_value,omitempty"`
	Source    string `json:"new_source,omitempty"`
	Content   string `json:"new_content,omitempty"`
	Polarity  string `json:"new_polarity,omitempty"`
}

type agentFactMentionProposal struct {
	Selection agentFactSelection      `json:"selection"`
	Preview   agentFactMentionChanges `json:"preview"`
}

type agentFactMentionReceipt struct {
	PreviewID   string `json:"preview_id"`
	ClaimID     int64  `json:"claim_id"`
	Object      string `json:"object"`
	Attribute   string `json:"attribute"`
	OldValue    string `json:"old_value,omitempty"`
	OldSource   string `json:"old_source,omitempty"`
	OldContent  string `json:"old_content,omitempty"`
	OldPolarity string `json:"old_polarity,omitempty"`
	Shared      bool   `json:"shared,omitempty"`
	agentFactMentionChanges
}

func fetchAgentFactMention(ctx context.Context, scope db.AgentFactEditScope, previewID string) (agentFactMentionProposal, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var proposal agentFactMentionProposal
	payload, _ := json.Marshal(map[string]any{
		"plan_id": scope.PlanID, "conversation_id": scope.ConversationID, "preview_id": previewID,
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent-chat/internal/fact-edit-preview", bytes.NewReader(payload))
	if err != nil {
		return proposal, http.StatusBadGateway, err
	}
	request.Header.Set("X-Xiaolan-Account", strconv.FormatInt(scope.ActorID, 10))
	request.Header.Set("Content-Type", "application/json")
	response, err := customerAgentClient.Do(request)
	if err != nil {
		return proposal, http.StatusServiceUnavailable, errors.New("暂时无法读取修改预览，请重试")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNotFound, http.StatusConflict:
		return proposal, http.StatusConflict, errors.New("修改预览已失效，请重新选中属性并生成预览")
	case http.StatusOK:
		if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&proposal); err == nil &&
			proposal.Selection.ID > 0 && proposal.Selection.Object != "" && len(proposal.Selection.Revision) == 64 && len(proposal.Selection.ObjectsRevision) == 64 {
			return proposal, 0, nil
		}
	}
	return proposal, http.StatusBadGateway, errors.New("无法读取完整的修改预览，请重试")
}

func prepareAgentFactMention(claim model.PlanMaterialClaim, ref agentFactEditReference, changes agentFactMentionChanges) (model.PlanMaterialClaimInput, error) {
	item := db.MaterialClaimInput(claim)
	if item.Destination != "" && item.Destination != "fact" {
		return item, errors.New("请选择事实清单中的事实属性")
	}
	if item.Destination == "" {
		item.Destination = "fact"
	}
	for _, text := range []string{changes.Attribute, changes.Value, changes.Source, changes.Content, changes.Polarity} {
		if text != strings.TrimSpace(text) || len(text) > 65535 || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
			return item, errors.New("修改内容过长或格式有误，请重新生成预览")
		}
	}
	if utf8.RuneCountInString(changes.Attribute) > 128 || strings.ContainsFunc(changes.Attribute, unicode.IsControl) ||
		utf8.RuneCountInString(changes.Value) > 12000 || utf8.RuneCountInString(changes.Content) > 12000 {
		return item, errors.New("修改内容过长或格式有误，请重新生成预览")
	}
	if changes == (agentFactMentionChanges{}) {
		return item, errors.New("预览里没有可保存的改动")
	}
	if changes.Attribute != "" {
		item.Attribute = changes.Attribute
		if item.Topic == "" || item.Topic == ref.Attribute {
			item.Topic = changes.Attribute
		}
	}
	if changes.Value != "" {
		if changes.Source == "" || changes.Value == ref.Value {
			return item, errors.New("请重新生成包含新值和修订原句的预览")
		}
		item.Value = changes.Value
		if changes.Content == "" {
			content, ok := replaceAgentFactValue(claim.Content, ref.Value, item.Value)
			if !ok {
				return item, errors.New("旧值无法在事实正文中唯一定位，请重新生成包含事实正文的预览")
			}
			item.Content = content
		}
	}
	if changes.Content != "" {
		if changes.Source == "" {
			return item, errors.New("请重新生成包含事实正文和修订原句的预览")
		}
		item.Content = changes.Content
	}
	if changes.Polarity != "" {
		if changes.Polarity != "positive" && changes.Polarity != "negative" {
			return item, errors.New("请核对修改的肯定或否定含义")
		}
		item.Polarity = changes.Polarity
		if item.FactType == "positive" || item.FactType == "negative" {
			item.FactType = changes.Polarity
		}
	}
	if changes.Source != "" {
		item.SourceSentence = changes.Source
		if !strings.Contains(changes.Source, item.SourceQuote) || item.SourceQuote == "" {
			item.SourceQuote = changes.Source
			if strings.Contains(changes.Source, item.Value) && item.Value != "" {
				item.SourceQuote = item.Value
			}
		}
		if err := bindEditedMaterialSource(&item); err != nil {
			return item, err
		}
	}
	return item, validateMaterialClaim(item)
}

func (s *Server) customerFactEdit(w http.ResponseWriter, r *http.Request) {
	actor, tenant, scopedPlan, ok := s.customerAgentActor(w, r)
	if !ok {
		return
	}
	scope := db.AgentFactEditScope{ActorID: actor.UserID, TenantID: tenant}
	var previewID string
	var shared bool
	applying := r.Method == http.MethodPost
	if applying {
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusBadRequest, "请求格式无效")
			return
		}
		var input struct {
			PlanID         int64  `json:"plan_id"`
			ConversationID string `json:"conversation_id"`
			PreviewID      string `json:"preview_id"`
			ConfirmShared  bool   `json:"confirm_shared"`
		}
		if err := readJSONWithLimit(w, r, &input, 2<<10); err != nil || !agentFactEditID.MatchString(input.PreviewID) {
			writeError(w, http.StatusBadRequest, "请选择一张修改预览后再确认")
			return
		}
		scope.PlanID, scope.ConversationID = input.PlanID, input.ConversationID
		previewID, shared = input.PreviewID, input.ConfirmShared
	} else {
		scope.PlanID, _ = strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
		scope.ConversationID = r.URL.Query().Get("conversation_id")
	}
	if scope.PlanID <= 0 || !agentFactEditID.MatchString(scope.ConversationID) {
		writeError(w, http.StatusBadRequest, "请刷新方案页面后再提交")
		return
	}
	if scopedPlan != 0 && scope.PlanID != scopedPlan {
		writeError(w, http.StatusForbidden, "协助仅限客户授权的当前方案")
		return
	}
	if scopedPlan == 0 {
		scope.TenantID, ok = s.resolveLiveAgentPlanTenant(w, r, actor, 0, applying)
		if !ok {
			return
		}
	}
	ctx := r.Context()
	if !applying {
		results, err := s.store.AgentFactEditReceipts(ctx, scope)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取事实修改结果失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return
	}
	s.applyCustomerFactMention(w, r, scope, previewID, shared)
}

func (s *Server) applyCustomerFactMention(w http.ResponseWriter, r *http.Request, scope db.AgentFactEditScope, previewID string, shared bool) {
	ctx := r.Context()
	existing, err := s.store.AgentFactEditReceipt(ctx, scope, previewID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取事实修改结果失败，请重试")
		return
	}
	sendResult := func(result json.RawMessage) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
	}
	if existing != nil {
		sendResult(existing)
		return
	}
	proposal, status, err := fetchAgentFactMention(ctx, scope, previewID)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	claims, catalog, links, err := s.store.PlanRewriteFactsSnapshot(ctx, scope.TenantID, scope.PlanID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前方案的事实失败")
		return
	}
	ref, err := selectedAgentFact(proposal.Selection, claims, catalog, links)
	objectsRevision := qapair.Hash([]any{catalog, links})
	replay := func() bool {
		existing, err := s.store.AgentFactEditReceipt(ctx, scope, previewID)
		if err == nil && existing != nil {
			sendResult(existing)
			return true
		}
		return false
	}
	if err != nil || objectsRevision != proposal.Selection.ObjectsRevision {
		if !replay() {
			writeError(w, http.StatusConflict, errAgentFactEditStale.Error())
		}
		return
	}
	claim := claims[slices.IndexFunc(claims, func(item model.PlanMaterialClaim) bool { return item.ID == proposal.Selection.ID })]
	if ref.Shared && !shared {
		writeError(w, http.StatusConflict, "这项属性由多个对象共用，请明确确认修改全部关联对象")
		return
	}
	input, err := prepareAgentFactMention(claim, *ref, proposal.Preview)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	name, vector, status, err := s.materialClaimEditVector(ctx, claim, input)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	receipt := agentFactMentionReceipt{PreviewID: previewID, ClaimID: claim.ID, Object: ref.Object, Attribute: ref.Attribute,
		OldValue: ref.Value, OldSource: ref.Source, OldContent: ref.Content, OldPolarity: ref.Polarity,
		Shared: ref.Shared, agentFactMentionChanges: proposal.Preview}
	// Persist the values actually stored, including the synchronized claim body.
	if receipt.Value != "" || receipt.Content != "" {
		if input.Content != ref.Content && input.Content != input.Value {
			receipt.Content = input.Content
		}
	}
	result, _ := json.Marshal(receipt)
	err = s.store.CommitAgentFactEdit(ctx, scope, previewID, result, claim.ID, input, name, vector,
		db.MaterialLibraryRevision(claims), objectsRevision, proposal.Preview.Source != "")
	if err != nil {
		if replay() {
			return
		}
		if errors.Is(err, db.ErrMaterialClaimConflict) || errors.Is(err, db.ErrMaterialClaimNotFound) {
			writeError(w, http.StatusConflict, errAgentFactEditStale.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "事实保存结果未确认，请重试")
		}
		return
	}
	s.scheduleFactCardRebuild(scope.TenantID, scope.PlanID)
	sendResult(result)
}
