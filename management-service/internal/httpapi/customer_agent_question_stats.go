package httpapi

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const customerAgentQuestionStatsPrefix = customerAgentChatPath + "/question-stats/"
const customerAgentQuestionStatsPath = customerAgentQuestionStatsPrefix + "{planID}/rooms"
const agentQuestionStatsCommand = "/system-user-question-stats"

type agentQuestionStatsSelection struct {
	RoomID    int64 `json:"room_id"`
	SessionID int64 `json:"session_id"`
}

type agentQuestionStatsContext struct {
	RoomName  string `json:"room_name"`
	StartedAt string `json:"started_at"`
	Total     int    `json:"total"`
	Rows      int    `json:"rows"`
	FileID    string `json:"file_id"`
}

type questionStatsFileScope struct {
	AccountID      int64  `json:"account_id"`
	PlanID         int64  `json:"plan_id"`
	ConversationID string `json:"conversation_id"`
}

var commentContact = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}|\b(?:\+?86[ -]?)?1[3-9][0-9]{9}\b`)

func (s *Server) listAgentQuestionRooms(w http.ResponseWriter, r *http.Request) {
	actor, tenant, supportPlan, ok := s.customerAgentActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	if supportPlan > 0 && supportPlan != planID {
		writeError(w, http.StatusForbidden, "协助仅限客户授权的当前方案")
		return
	}
	if supportPlan == 0 {
		tenant, ok = s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
		if !ok {
			return
		}
	}
	roomID := int64(0)
	if r.PathValue("roomID") != "" {
		roomID, ok = pathID(w, r)
		if !ok {
			return
		}
	}
	rooms, ok := s.agentQuestionRooms(w, r, actor, tenant, planID, roomID)
	if !ok {
		return
	}
	if roomID == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"items": rooms})
		return
	}
	if len(rooms) != 1 {
		writeError(w, http.StatusNotFound, "当前方案没有可访问的这个直播间")
		return
	}
	sessions, err := s.store.ListQuestionStatsSessions(r.Context(), tenant, roomID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取直播场次失败，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": sessions})
}

func (s *Server) agentQuestionRooms(w http.ResponseWriter, r *http.Request, actor model.Actor, tenant, plan, room int64) ([]db.QuestionStatsRoom, bool) {
	items, err := s.store.ListQuestionStatsRooms(r.Context(), tenant, plan, room)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取方案关联直播间失败，请重试")
		return nil, false
	}
	rooms := []db.QuestionStatsRoom{}
	for _, item := range items {
		if actor.IsInternalStaff() {
			allowed, err := s.store.HasLiveSupportAuthorization(r.Context(), tenant, item.ID, actor.UserID, model.LiveSupportCapabilityL3Policy)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "校验直播间协助授权失败")
				return nil, false
			}
			if !allowed {
				continue
			}
		}
		item.Name = strings.Join(strings.Fields(item.Name), " ")
		if item.Name == "" {
			item.Name = fmt.Sprintf("直播间 %d", item.ID)
		}
		rooms = append(rooms, item)
	}
	return rooms, true
}

func questionStatsInstruction(message string) (string, bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", true // Clients can still submit a saved selection without notes.
	}
	after, found := strings.CutPrefix(message, agentQuestionStatsCommand)
	instruction := strings.TrimSpace(after)
	return instruction, found && (after == "" || strings.TrimLeftFunc(after, unicode.IsSpace) != after) &&
		!strings.ContainsRune(instruction, 0) && utf8.RuneCountInString(message) <= 4000
}

func questionStatsMessage(report agentQuestionStatsContext, instruction string) string {
	message := agentQuestionStatsCommand + " 统计用户问题 · " + report.RoomName + " · " + report.StartedAt
	if instruction != "" {
		message += "\n\n" + instruction
	}
	return message
}

func (s *Server) relayAgentQuestionStats(w http.ResponseWriter, r *http.Request, actor model.Actor, tenant, plan int64,
	conversation string, choice agentQuestionStatsSelection, instruction string) {
	rooms, ok := s.agentQuestionRooms(w, r, actor, tenant, plan, choice.RoomID)
	if !ok {
		return
	}
	if len(rooms) != 1 {
		writeError(w, http.StatusNotFound, "当前方案没有可访问的这个直播间")
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		writeError(w, http.StatusServiceUnavailable, "无法准备直播弹幕文件")
		return
	}
	fileID := hex.EncodeToString(token[:])
	path := filepath.Join(filepath.Dir(customerAgentChatSocket()), "question-stats", "stats-"+fileID+".jsonl")
	createdAt := time.Now()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "无法准备直播弹幕文件，请重试")
		return
	}
	defer os.Remove(path)
	defer file.Close()
	buffer := bufio.NewWriterSize(file, 64<<10)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(questionStatsFileScope{actor.UserID, plan, conversation}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "无法准备直播弹幕文件，请重试")
		return
	}
	rows := 0
	session, total, err := s.store.StreamQuestionStatsComments(r.Context(), tenant, plan, choice.RoomID, choice.SessionID,
		func(comment db.QuestionStatsComment) error {
			comment.Text = strings.Map(func(char rune) rune {
				if unicode.IsControl(char) {
					return ' '
				}
				return char
			}, comment.Text)
			comment.Text = commentContact.ReplaceAllString(strings.Join(strings.Fields(comment.Text), " "), "[联系方式]")
			if err := encoder.Encode(comment); err != nil {
				return err
			}
			rows++
			return nil
		})
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "所选场次已不在最近记录中，请重新选择")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取该场直播的弹幕失败，请重试")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusUnprocessableEntity, "这场直播暂无可分析的用户弹幕")
		return
	}
	if err := buffer.Flush(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "整理弹幕文件失败，请重试")
		return
	}
	if err := os.Chtimes(path, createdAt, createdAt); err != nil {
		writeError(w, http.StatusServiceUnavailable, "整理弹幕文件失败，请重试")
		return
	}
	if err := file.Chmod(0o640); err != nil {
		writeError(w, http.StatusServiceUnavailable, "弹幕文件暂不可读取，请重试")
		return
	}
	if err := file.Close(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "整理弹幕文件失败，请重试")
		return
	}
	report := agentQuestionStatsContext{RoomName: rooms[0].Name,
		StartedAt: session.StartedAt.In(time.FixedZone("UTC+8", 8*3600)).Format("2006-01-02 15:04"),
		Total:     total, Rows: rows, FileID: fileID}
	message := questionStatsMessage(report, instruction)
	if utf8.RuneCountInString(message) > 4000 {
		writeError(w, http.StatusBadRequest, "统计要求过长，请缩短后再发送")
		return
	}
	payload, err := json.Marshal(struct {
		Message        string                   `json:"message"`
		PlanID         int64                    `json:"plan_id"`
		ConversationID string                   `json:"conversation_id"`
		PlanContext    customerAgentPlanContext `json:"plan_context"`
	}{message, plan, conversation,
		customerAgentPlanContext{Facts: []json.RawMessage{}, QuestionStats: &report}})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "整理弹幕数据失败，请重试")
		return
	}
	relayCustomerAgentRequest(w, r, actor.UserID, "/conversation", payload, customerAgentStatsClient)
}
