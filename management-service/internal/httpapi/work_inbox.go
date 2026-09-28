package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/workinbox"
)

func (s *Server) SetWorkInbox(inbox *workinbox.Service) { s.inbox = inbox }
func (s *Server) registerWorkInboxRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/work/inbox", s.workInboxSnapshot)
	m.HandleFunc("GET /api/v1/work/inbox/items", s.workInboxItems)
	m.HandleFunc("GET /api/v1/work/inbox/stream", s.workInboxStream)
}
func (s *Server) inboxScopeForActor(r *http.Request, actor model.Actor) (model.InboxScope, error) {
	sc := model.InboxScope{Actor: actor, RequireDistinctReviewer: true}
	if !model.WorkInboxAvailable(actor.Role) {
		return sc, fmt.Errorf("当前账号不提供统一待办入口")
	}
	if actor.IsInternalStaff() {
		a, err := s.staffAccessForActor(r, actor)
		if err != nil {
			return sc, err
		}
		sc.Access = a
		if staffHasPermission(a, "finance.dashboard.view") {
			policy, err := s.store.FinanceReviewPolicy(r.Context())
			if err != nil {
				return sc, err
			}
			sc.RequireDistinctReviewer = policy.RequireDistinctReviewer
		}
	}
	return sc, nil
}
func (s *Server) inboxScope(w http.ResponseWriter, r *http.Request) (model.InboxScope, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.InboxScope{}, false
	}
	if !model.WorkInboxAvailable(actor.Role) {
		writeError(w, http.StatusForbidden, "终端用户请在订单、售后或运维协助页面查看进度")
		return model.InboxScope{}, false
	}
	if s.inbox == nil {
		writeError(w, 503, "统一待办尚未就绪")
		return model.InboxScope{}, false
	}
	sc, err := s.inboxScopeForActor(r, actor)
	if err != nil {
		writeError(w, 503, "无法核对当前待办权限，请重试")
		return sc, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return sc, true
}
func (s *Server) workInboxSnapshot(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.inboxScope(w, r)
	if !ok {
		return
	}
	v, err := s.inbox.Snapshot(r.Context(), sc)
	if err != nil {
		writeError(w, 503, "待办同步暂不可用")
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) workInboxItems(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.inboxScope(w, r)
	if !ok {
		return
	}
	key := r.URL.Query().Get("group")
	if len(key) > 80 || key == "" {
		writeError(w, 400, "请选择待办分类")
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	v, err := s.store.InboxItems(r.Context(), sc, key, page, size, r.URL.Query().Get("due") == "1")
	if err != nil {
		customerBusinessError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) workInboxStream(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.inboxScope(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 503, "当前连接不支持待办推送")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	version := ""
	authAt := time.Now()
	heartbeatAt := time.Now()
	writeEvent := func(event string, value any) bool {
		raw, err := json.Marshal(value)
		if err != nil {
			return false
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for {
		current := s.inbox.Version()
		if current != version || time.Since(authAt) >= 30*time.Second {
			// Reauthorize BEFORE every data push, also while idle. No claims from EventSource are trusted.
			actor, err := s.auth.Resolve(r)
			if err != nil || actor.UserID != sc.Actor.UserID {
				writeEvent("reset", map[string]string{"reason": "session_changed"})
				return
			}
			fresh, err := s.inboxScopeForActor(r, actor)
			if err != nil {
				writeEvent("unavailable", map[string]string{"reason": "authorization_unavailable"})
				return
			}
			sc = fresh
			authAt = time.Now()
			if current != version {
				snapshot, err := s.inbox.Snapshot(r.Context(), sc)
				if err != nil || !writeEvent("snapshot", snapshot) {
					return
				}
				version = current
				heartbeatAt = time.Now()
			}
		}
		if time.Since(heartbeatAt) >= 20*time.Second {
			if !writeEvent("heartbeat", map[string]bool{"ok": true}) {
				return
			}
			heartbeatAt = time.Now()
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
	}
}

// Covers nontransactional legacy writers too. Transactional source writers publish
// inside their commit; the revision read model coalesces duplicate invalidations.
func inboxMutationTopics(path string) []string {
	switch {
	case strings.HasPrefix(path, "/api/v1/staff/employees"), strings.HasPrefix(path, "/api/v1/staff/roles"), strings.HasPrefix(path, "/api/v1/staff/groups"), strings.HasPrefix(path, "/api/v1/system/settings"), strings.Contains(path, "sales-assignment"), strings.HasPrefix(path, "/api/v1/admin/sales/"):
		return []string{"access"}
	case strings.HasPrefix(path, "/api/v1/customer-business/receipts"), strings.HasPrefix(path, "/api/v1/staff/finance"), strings.HasPrefix(path, "/api/v1/finance/"):
		return []string{"finance", "inventory", "logistics", "sales"}
	case strings.HasPrefix(path, "/api/v1/service/tickets"):
		return []string{"support"}
	case strings.HasPrefix(path, "/api/v1/inventory/"), strings.HasPrefix(path, "/api/v1/after-sales/"):
		return []string{"inventory", "logistics"}
	case strings.HasPrefix(path, "/api/v1/logistics/"):
		return []string{"logistics", "inventory"}
	case strings.HasPrefix(path, "/api/v1/sales/"):
		return []string{"sales", "access"}
	case strings.HasPrefix(path, "/api/v1/shop/"):
		return []string{"inventory", "logistics", "finance"}
	}
	return nil
}

type inboxStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *inboxStatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *inboxStatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (s *Server) inboxMutationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		topics := inboxMutationTopics(r.URL.Path)
		if !isMutationMethod(r.Method) || len(topics) == 0 || s.inbox == nil {
			next.ServeHTTP(w, r)
			return
		}
		rw := &inboxStatusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		if rw.status >= 200 && rw.status < 300 {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := s.store.TouchInbox(ctx, topics...); err != nil {
				log.Printf("inbox legacy invalidation failed; bounded reconciliation remains active: %v", err)
			}
		}
	})
}

func inboxQueryIntent(message string) bool {
	if strings.Contains(message, "设计") || strings.Contains(message, "话术") {
		return false
	}
	for _, word := range []string{"待办", "待审核", "待接单", "待处理", "待补充", "待确认", "要处理什么", "该处理什么", "到期回访", "哪些超时", "哪些已经超时", "哪些逾期", "逾期待办"} {
		if strings.Contains(message, word) {
			return true
		}
	}
	return false
}
func (s *Server) tryInboxAgentResponse(w http.ResponseWriter, r *http.Request, actor model.Actor, message string) bool {
	if !model.WorkInboxAvailable(actor.Role) {
		// Product guidance only: never read counts, offer an inbox route or dispatch an action.
		if actor.Role == "customer" && (strings.Contains(message, "待办") || strings.Contains(message, "我的代办")) {
			writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
				Reply:        "终端不设置统一待办入口。订单、售后和运维协助的进度，请在对应业务页面查看；需要补充资料或确认处理结果，也在原页面办理。",
				Capabilities: clientAgentCapabilities(actor),
			})
			return true
		}
		return false
	}
	if !inboxQueryIntent(message) {
		return false
	}
	if s.inbox == nil {
		writeError(w, 503, "待办服务尚未就绪")
		return true
	}
	sc, err := s.inboxScopeForActor(r, actor)
	if err != nil {
		writeError(w, 503, "无法读取当前待办权限")
		return true
	}
	snap, err := s.inbox.Snapshot(r.Context(), sc)
	if err != nil {
		writeError(w, 503, "待办读取失败")
		return true
	}
	category := ""
	for _, v := range []struct{ word, key string }{{"待审核", "review"}, {"待接单", "accept"}, {"待处理", "process"}, {"待补充", "supplement"}, {"待确认", "confirm"}} {
		if strings.Contains(message, v.word) {
			category = v.key
			break
		}
	}
	department := ""
	for _, name := range []string{"财务", "运维", "仓储", "物流", "销售"} {
		if strings.Contains(message, name) {
			department = name
			break
		}
	}
	if strings.Contains(message, "维修") || strings.Contains(message, "库存") || strings.Contains(message, "仓库") {
		department = "仓储售后"
	}
	if strings.Contains(message, "超时") || strings.Contains(message, "逾期") {
		groups := make([]model.InboxGroup, 0, len(snap.Groups))
		for _, g := range snap.Groups {
			g.Count = g.DueCount
			groups = append(groups, g)
		}
		snap.Groups = groups
	}
	// Counts and references are server facts; no model completion invents a task.
	reply := workinbox.Summary(snap, category, department)
	for _, word := range []string{"全部通过", "一键通过", "直接付款", "全部完成"} {
		if strings.Contains(message, word) {
			reply = "待办提醒不会自动执行批量审核、付款或完成任务。请逐单核对并确认。\n" + reply
			break
		}
	}
	writeAgentChatOutput(w, 200, systemAgentChatOutput{Reply: reply, Capabilities: []string{"我的待办", "分类提醒"}, Model: "business-inbox", Navigate: &systemAgentNavigationContext{Title: "我的待办", To: "/work/inbox" + inboxFilterQuery(category), Section: "当前账号"}})
	return true
}
func inboxFilterQuery(category string) string {
	if category == "" {
		return ""
	}
	return "?category=" + category
}
