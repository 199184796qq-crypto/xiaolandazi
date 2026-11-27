package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

const customerAgentChatPath = "/api/v1/customer/agent-chat"
const customerAgentChatOpenPath = customerAgentChatPath + "/open"
const customerAgentSkillsPath = customerAgentChatPath + "/skills"
const customerAgentImageMaxBytes = 4 << 20
const customerAgentSkillMaxBytes = 256 << 10
const customerAgentSkillContentMaxBytes = 64 << 10

var customerAgentSkillSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type customerAgentImage struct {
	MIME string `json:"mime_type"`
	Data string `json:"data"`
}

func (image customerAgentImage) valid() bool {
	if len(image.Data) > base64.StdEncoding.EncodedLen(customerAgentImageMaxBytes) {
		return false
	}
	switch image.MIME {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return false
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(image.Data)
	return err == nil && len(raw) <= customerAgentImageMaxBytes && http.DetectContentType(raw) == image.MIME
}

type scriptFactReviewInput struct {
	Title    string `json:"title"`
	Original string `json:"original"`
}

func (review *scriptFactReviewInput) valid() bool {
	return strings.TrimSpace(review.Original) != "" && utf8.RuneCountInString(review.Original) <= 12000 &&
		utf8.RuneCountInString(review.Title) <= 100 && !strings.ContainsRune(review.Original, 0) &&
		!strings.ContainsFunc(review.Title, unicode.IsControl)
}

type customerAgentDocument struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

func (doc *customerAgentDocument) valid() bool {
	if doc.Name == "" || utf8.RuneCountInString(doc.Name) > 180 ||
		strings.ContainsAny(doc.Name, "/\\") || strings.ContainsFunc(doc.Name, unicode.IsControl) {
		return false
	}
	switch strings.ToLower(path.Ext(doc.Name)) {
	case ".docx", ".xlsx", ".xls", ".pdf":
	default:
		return false
	}
	if len(doc.Data) == 0 || len(doc.Data) > base64.StdEncoding.EncodedLen(customerAgentImageMaxBytes) {
		return false
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(doc.Data)
	return err == nil && len(raw) > 0 && len(raw) <= customerAgentImageMaxBytes
}

var customerAgentClient = &http.Client{
	Timeout: 255 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			return dialer.DialContext(ctx, "unix", customerAgentChatSocket())
		},
	},
}

var customerAgentStatsClient = &http.Client{
	Timeout:   520 * time.Second,
	Transport: customerAgentClient.Transport,
}

func customerAgentChatSocket() string {
	if socket := os.Getenv("XIAOLAN_AGENT_CHAT_SOCKET"); socket != "" {
		return socket
	}
	return "/run/xiaolan-agent-chat/chat.sock"
}

type customerAgentStreamWriter struct {
	writer  http.ResponseWriter
	control *http.ResponseController
}

func (s customerAgentStreamWriter) Write(chunk []byte) (int, error) {
	n, err := s.writer.Write(chunk)
	if err == nil {
		err = s.control.Flush()
	}
	return n, err
}

func (s *Server) registerCustomerAgentChat(mux *http.ServeMux) {
	mux.HandleFunc("GET "+customerAgentChatPath, s.customerAgentChat)
	mux.HandleFunc("POST "+customerAgentChatPath, s.customerAgentChat)
	mux.HandleFunc("POST "+customerAgentChatOpenPath, s.customerAgentChat)
	mux.HandleFunc("GET "+customerAgentQuestionStatsPath, s.listAgentQuestionRooms)
	mux.HandleFunc("GET "+customerAgentQuestionStatsPath+"/{roomID}/sessions", s.listAgentQuestionRooms)
	mux.HandleFunc("POST "+customerAgentFactEditPath+"/preview", s.customerAgentFactEditAction)
	mux.HandleFunc("POST "+customerAgentFactEditPath+"/confirm", s.customerAgentFactEditAction)
	mux.HandleFunc("POST "+customerFactEditPath+"/confirm", s.customerFactEdit)
	mux.HandleFunc("GET "+customerFactEditPath+"/results", s.customerFactEdit)
	mux.HandleFunc("GET "+customerAgentSkillsPath, s.customerAgentSkills)
	mux.HandleFunc("POST "+customerAgentSkillsPath, s.customerAgentSkills)
	mux.HandleFunc("GET "+customerAgentSkillsPath+"/{slug}", s.customerAgentSkills)
	mux.HandleFunc("PATCH "+customerAgentSkillsPath+"/{slug}", s.customerAgentSkills)
	mux.HandleFunc("DELETE "+customerAgentSkillsPath+"/{slug}", s.customerAgentSkills)
}

func (s *Server) customerAgentActor(w http.ResponseWriter, r *http.Request) (model.Actor, int64, int64, bool) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return actor, 0, 0, false
	}
	if actor.UserID > 0 && !actor.MustChangePassword {
		if actor.Role == "customer" {
			return actor, 0, 0, true
		}
		if planID, _, valid := livePlanSupportScope(r); actor.IsInternalStaff() && valid {
			tenantID, ok := s.resolveScopedLivePlanSupport(w, r, actor, requestTenantID(r), planID)
			return actor, tenantID, planID, ok
		}
	}
	writeError(w, http.StatusForbidden, "当前账号无法使用智能体")
	return actor, 0, 0, false
}

func (s *Server) customerAgentChat(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, supportPlanID, ok := s.customerAgentActor(w, r)
	if !ok {
		return
	}
	var payload []byte
	if r.Method == http.MethodPost {
		opening := r.URL.Path == customerAgentChatOpenPath
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusBadRequest, "消息格式无效")
			return
		}
		var input struct {
			Message         string                       `json:"message"`
			PlanID          int64                        `json:"plan_id"`
			ReasoningEffort string                       `json:"reasoning_effort"`
			WebSearch       bool                         `json:"web_search,omitempty"`
			Document        *customerAgentDocument       `json:"document,omitempty"`
			Image           *customerAgentImage          `json:"image,omitempty"`
			FactSelection   *agentFactSelection          `json:"fact_selection,omitempty"`
			ConversationID  string                       `json:"conversation_id,omitempty"`
			ScriptReview    *scriptFactReviewInput       `json:"script_review,omitempty"`
			QuestionStats   *agentQuestionStatsSelection `json:"question_stats,omitempty"`
		}
		requestLimit := int64(base64.StdEncoding.EncodedLen(customerAgentImageMaxBytes) + (32 << 10))
		if opening {
			requestLimit = 2 << 10
		}
		if err := readJSONWithLimit(w, r, &input, requestLimit); err != nil {
			var tooLarge *http.MaxBytesError
			if !opening && errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "图片或文件大小不能超过 4 MB")
				return
			}
			writeError(w, http.StatusBadRequest, "消息格式无效")
			return
		}
		if input.PlanID <= 0 || !opening && len(input.ConversationID) != 32 {
			writeError(w, http.StatusBadRequest, "请刷新方案页面后再发送")
			return
		}
		if supportPlanID != 0 && input.PlanID != supportPlanID {
			writeError(w, http.StatusForbidden, "协助仅限客户授权的当前方案")
			return
		}
		if input.ScriptReview != nil && (opening || !input.ScriptReview.valid() || input.Message != "" ||
			input.WebSearch || input.Image != nil || input.Document != nil || input.FactSelection != nil) {
			writeError(w, http.StatusBadRequest, "请重新打开核对面板，并检查原话术")
			return
		}
		if input.QuestionStats != nil {
			choice := *input.QuestionStats
			instruction, valid := questionStatsInstruction(input.Message)
			if opening || choice.RoomID <= 0 || choice.SessionID <= 0 || !valid || input.WebSearch ||
				input.Image != nil || input.Document != nil || input.FactSelection != nil || input.ScriptReview != nil {
				writeError(w, http.StatusBadRequest, "请重新选择要统计的直播场次")
				return
			}
			if supportPlanID == 0 {
				tenantID, ok = s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
				if !ok {
					return
				}
			}
			s.relayAgentQuestionStats(w, r, actor, tenantID, input.PlanID, input.ConversationID, choice, instruction)
			return
		}
		if !opening {
			input.Message = strings.TrimSpace(input.Message)
			if input.FactSelection != nil && (input.Message == "" || input.Document != nil || input.Image != nil) {
				writeError(w, http.StatusBadRequest, "请用文字说明要怎样修改所选属性")
				return
			}
			if input.ScriptReview == nil && (utf8.RuneCountInString(input.Message) > 4000 || input.Message == "" && input.Image == nil && input.Document == nil) {
				writeError(w, http.StatusBadRequest, "请输入消息或添加文件，文字最多 4000 字")
				return
			}
			if input.Document != nil && (input.Image != nil || !input.Document.valid()) {
				writeError(w, http.StatusBadRequest, "每次可发送一个 4 MB 以内的 Word（.docx）、Excel（.xlsx、.xls）、PDF 或图片")
				return
			}
			if input.Image != nil && !input.Image.valid() {
				writeError(w, http.StatusBadRequest, "请粘贴 4 MB 以内的 PNG、JPEG、WebP 或 GIF 图片")
				return
			}
			switch input.ReasoningEffort {
			case "":
				input.ReasoningEffort = "off"
			case "off", "low", "high", "max":
			default:
				writeError(w, http.StatusBadRequest, "推理等级无效")
				return
			}
		}
		if supportPlanID == 0 {
			tenantID, ok = s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
			if !ok {
				return
			}
		}
		ctx := r.Context()
		plan, err := s.store.GetLiveAgentPlan(ctx, tenantID, input.PlanID)
		if errors.Is(err, db.ErrLiveAgentPlanNotFound) {
			writeError(w, http.StatusNotFound, "直播智能体方案不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
			return
		}
		if opening {
			payload, _ = json.Marshal(struct {
				PlanID int64 `json:"plan_id"`
			}{plan.ID})
			relayCustomerAgentChat(w, r, actor.UserID, payload)
			return
		}
		if edit, ok := parseAgentFactEdit(input.Message); ok && input.FactSelection == nil && input.Image == nil && input.Document == nil {
			s.previewCustomerAgentFactEdit(w, r, actor.UserID, tenantID, plan.ID, input.ConversationID, input.Message, edit)
			return
		}
		claims, catalog, links, err := s.store.PlanRewriteFactsSnapshot(ctx, tenantID, plan.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取方案事实库失败")
			return
		}
		planContext := customerAgentPlanContext{Name: plan.Name, Facts: []json.RawMessage{}}
		if input.FactSelection != nil {
			planContext.FactEdit, err = selectedAgentFact(*input.FactSelection, claims, catalog, links)
			if err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			input.FactSelection.ObjectsRevision = qapair.Hash([]any{catalog, links})
			planContext.FactSelection = input.FactSelection
			input.WebSearch = false
		} else {
			facts, _ := projectRewriteFactsByObject(claims, catalog, links, input.ScriptReview != nil)
			planContext = customerAgentContext(plan.Name, facts)
			if input.ScriptReview != nil {
				if len(planContext.Facts) == 0 {
					writeError(w, http.StatusUnprocessableEntity, "当前方案没有可用的已确认事实")
					return
				}
				planContext.ScriptReviewBasis = db.MaterialReviewRevision(claims, catalog, links)
				input.ReasoningEffort = "low"
				input.Message = "/system-script-fact-review 核对话术片"
			}
		}
		// 事实抽取技能需要知道当前方案已有的对象叫法，才能把“6号”这类
		// 原文简称改写成目录里的“6号链接”；原句仍由模型逐字保留。
		if strings.HasPrefix(strings.TrimSpace(input.Message), "/system-atomic-fact-json") {
			if directory := customerAgentObjectDirectory(catalog); directory != "" {
				input.Message = strings.TrimSpace(input.Message) + "\n\n" + directory
			}
		}
		payload, _ = json.Marshal(struct {
			Message         string                   `json:"message"`
			Plan            customerAgentPlanContext `json:"plan_context"`
			ReasoningEffort string                   `json:"reasoning_effort"`
			WebSearch       bool                     `json:"web_search,omitempty"`
			Document        *customerAgentDocument   `json:"document,omitempty"`
			Image           *customerAgentImage      `json:"image,omitempty"`
			PlanID          int64                    `json:"plan_id"`
			ConversationID  string                   `json:"conversation_id"`
			ScriptReview    *scriptFactReviewInput   `json:"script_review,omitempty"`
		}{input.Message, planContext, input.ReasoningEffort, input.WebSearch, input.Document, input.Image, plan.ID, input.ConversationID, input.ScriptReview})
	}
	relayCustomerAgentChat(w, r, actor.UserID, payload)
}

// customerAgentObjectDirectory renders the plan object names for the fact
// extraction skill. It is appended to the skill message only, so the model can
// normalize scope names without touching the original sentence.
func customerAgentObjectDirectory(catalog model.MaterialObjectCatalog) string {
	names := []string{}
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	add(catalog.Directory.GlobalDisplayName)
	for _, object := range catalog.Directory.Objects {
		add(object.DisplayName)
	}
	if len(names) == 0 {
		return ""
	}
	return "【当前方案对象目录】\n" + strings.Join(names, "、") +
		"\n填写 scope_name 时使用目录里的规范叫法；原文简称（如“6号”“2号”）按目录中“N号链接”的命名规律改写；目录中没有的对象按同样规律推定；source_sentence 必须保持原文不变。"
}

func (s *Server) customerAgentSkills(w http.ResponseWriter, r *http.Request) {
	actor, _, _, ok := s.customerAgentActor(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, http.StatusForbidden, "请求来源无效")
		return
	}
	path := "/skills"
	if slug := r.PathValue("slug"); slug != "" {
		if len(slug) > 60 || !customerAgentSkillSlug.MatchString(slug) {
			writeError(w, http.StatusNotFound, "技能不存在")
			return
		}
		path += "/" + slug
	}
	var payload []byte
	if r.Method == http.MethodPost || r.Method == http.MethodPatch {
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" {
			writeError(w, http.StatusBadRequest, "技能文件格式无效")
			return
		}
	}
	switch r.Method {
	case http.MethodPost:
		var input struct {
			Filename string `json:"filename"`
			Data     string `json:"data"`
		}
		encodedLimit := base64.StdEncoding.EncodedLen(customerAgentSkillMaxBytes)
		err := readJSONWithLimit(w, r, &input, int64(encodedLimit+2048))
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || len(input.Data) > encodedLimit {
			writeError(w, http.StatusRequestEntityTooLarge, "上传文件最多 256 KB")
			return
		}
		if err != nil || len(input.Filename) > 255 || input.Filename == "" || input.Data == "" {
			writeError(w, http.StatusBadRequest, "请选择 SKILL.md 或 ZIP 文件")
			return
		}
		payload, _ = json.Marshal(input)
	case http.MethodPatch:
		var input struct {
			Content  string `json:"content"`
			Revision string `json:"revision"`
		}
		err := readJSONWithLimit(w, r, &input, 6*customerAgentSkillContentMaxBytes+2048)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || len(input.Content) > customerAgentSkillContentMaxBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "SKILL.md 最多 64 KB")
			return
		}
		if err != nil || len(input.Revision) != 64 || strings.TrimSpace(input.Content) == "" {
			writeError(w, http.StatusBadRequest, "请检查技能内容并重新保存")
			return
		}
		payload, _ = json.Marshal(input)
	}
	relayCustomerAgentRequest(w, r, actor.UserID, path, payload, customerAgentClient)
}

func purgeCustomerAgentAccount(ctx context.Context, userID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://agent-chat/internal/purge-account", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-Xiaolan-Account", strconv.FormatInt(userID, 10))
	response, err := customerAgentClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result struct {
		Deleted bool `json:"deleted"`
	}
	if response.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&result) != nil || !result.Deleted {
		return errors.New("智能体账号目录清理尚未完成")
	}
	return nil
}

type customerAgentPlanContext struct {
	Name              string                     `json:"方案名称"`
	Facts             []json.RawMessage          `json:"事实"`
	More              bool                       `json:"部分事实未随本轮发送,omitempty"`
	FactEdit          *agentFactEditReference    `json:"修改预览,omitempty"`
	FactSelection     *agentFactSelection        `json:"fact_selection,omitempty"`
	ScriptReviewBasis string                     `json:"script_review_basis,omitempty"`
	QuestionStats     *agentQuestionStatsContext `json:"直播弹幕统计,omitempty"`
}

func customerAgentContext(name string, facts []rewriteFact) customerAgentPlanContext {
	// Keep complete facts together, including their restrictions, within the
	// budget. The database returns the most recently updated facts first.
	remaining := 64 << 10
	out := customerAgentPlanContext{Name: name, Facts: []json.RawMessage{}}
	for _, fact := range facts {
		raw, err := json.Marshal(fact)
		if err != nil || len(raw)+1 > remaining {
			out.More = true
			continue
		}
		out.Facts = append(out.Facts, raw)
		remaining -= len(raw) + 1
	}
	return out
}

func relayCustomerAgentChat(w http.ResponseWriter, r *http.Request, userID int64, payload []byte) {
	// The session owner comes from the authenticated actor, never from browser input.
	path := "/conversation"
	if r.URL.Path == customerAgentChatOpenPath {
		path += "/open"
	}
	relayCustomerAgentRequest(w, r, userID, path, payload, customerAgentClient)
}

func relayCustomerAgentRequest(w http.ResponseWriter, r *http.Request, userID int64, path string, payload []byte, client *http.Client) {
	ctx := r.Context()
	if path != "/conversation" || r.Method != http.MethodPost {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 100*time.Second)
		defer cancel()
	}
	request, err := http.NewRequestWithContext(ctx, r.Method, "http://agent-chat"+path, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusBadGateway, "智能体暂时不可用")
		return
	}
	request.Header.Set("X-Xiaolan-Account", strconv.FormatInt(userID, 10))
	request.Header.Set("Content-Type", "application/json")
	stream := path == "/conversation" && r.Method == http.MethodPost &&
		r.Header.Get("Accept") == "text/event-stream" && anchorResponseCanFlush(w)
	if stream {
		request.Header.Set("Accept", "text/event-stream")
	}
	response, err := client.Do(request)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "智能体暂时不可用。发送结果如未确认，请先刷新对话。")
		return
	}
	defer response.Body.Close()
	if stream && response.StatusCode == http.StatusOK && strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		out := customerAgentStreamWriter{w, http.NewResponseController(w)}
		if out.control.Flush() == nil {
			_, _ = io.Copy(out, io.LimitReader(response.Body, 12<<20))
		}
		return
	}
	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound,
		http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusGatewayTimeout:
		body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		if err == nil && len(body) <= 2<<20 && json.Valid(body) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(response.StatusCode)
			_, _ = w.Write(body)
			return
		}
	}
	writeError(w, http.StatusBadGateway, "智能体暂时不可用。发送结果如未确认，请先刷新对话。")
}
