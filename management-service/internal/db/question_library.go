package db

import (
	"context"
	"encoding/json"
	"strings"

	"livecompanion/management/internal/model"
)

// ListQuestionLibrary returns the room's question-library entries.
func (s *Store) ListQuestionLibrary(ctx context.Context, tenantID, roomID int64) ([]model.QuestionLibraryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, room_id, plan_id, question_type, question, answer, status, updated_at
FROM live_agent_question_library
WHERE tenant_id = ? AND room_id = ? AND status = 'active'
ORDER BY id`, tenantID, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.QuestionLibraryEntry, 0, 16)
	for rows.Next() {
		var item model.QuestionLibraryEntry
		if err := rows.Scan(&item.ID, &item.TenantID, &item.RoomID, &item.PlanID, &item.QuestionType, &item.Question, &item.Answer, &item.Status, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpsertQuestionLibrary creates or updates one entry; id=0 inserts.
func (s *Store) UpsertQuestionLibrary(ctx context.Context, tenantID, roomID, userID int64, id int64, input model.QuestionLibraryInput) (model.QuestionLibraryEntry, error) {
	questionType := strings.TrimSpace(input.QuestionType)
	if questionType == "" {
		questionType = "fact"
	}
	question := strings.TrimSpace(input.Question)
	answer := strings.TrimSpace(input.Answer)
	if id > 0 {
		_, err := s.db.ExecContext(ctx, `
UPDATE live_agent_question_library
SET question_type = ?, question = ?, answer = ?
WHERE id = ? AND tenant_id = ? AND room_id = ?`,
			questionType, question, answer, id, tenantID, roomID)
		if err != nil {
			return model.QuestionLibraryEntry{}, err
		}
		return s.questionLibraryEntry(ctx, id)
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO live_agent_question_library (tenant_id, room_id, plan_id, question_type, question, answer, created_by_user_id)
VALUES (?, ?, 0, ?, ?, ?, ?)`, tenantID, roomID, questionType, question, answer, userID)
	if err != nil {
		return model.QuestionLibraryEntry{}, err
	}
	newID, _ := res.LastInsertId()
	return s.questionLibraryEntry(ctx, newID)
}

func (s *Store) questionLibraryEntry(ctx context.Context, id int64) (model.QuestionLibraryEntry, error) {
	var item model.QuestionLibraryEntry
	err := s.db.QueryRowContext(ctx, `
SELECT id, tenant_id, room_id, plan_id, question_type, question, answer, status, updated_at
FROM live_agent_question_library WHERE id = ?`, id).
		Scan(&item.ID, &item.TenantID, &item.RoomID, &item.PlanID, &item.QuestionType, &item.Question, &item.Answer, &item.Status, &item.UpdatedAt)
	return item, err
}

// DeleteQuestionLibrary marks one entry inactive.
func (s *Store) DeleteQuestionLibrary(ctx context.Context, tenantID, roomID, id int64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE live_agent_question_library SET status = 'deleted'
WHERE id = ? AND tenant_id = ? AND room_id = ?`, id, tenantID, roomID)
	return err
}

// AppendQuestionGap records an unanswered question discovered by intent mining.
// It dedupes by type and question similarity so repeated asks do not pile up.
func (s *Store) AppendQuestionGap(ctx context.Context, tenantID, roomID, planID int64, questionType, question string) (bool, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return false, nil
	}
	existing, err := s.ListQuestionLibrary(ctx, tenantID, roomID)
	if err != nil {
		return false, err
	}
	normalized := normalizeGapQuestion(question)
	for _, item := range existing {
		if item.QuestionType != questionType {
			continue
		}
		other := normalizeGapQuestion(item.Question)
		if other == "" {
			continue
		}
		if other == normalized || strings.Contains(other, normalized) || strings.Contains(normalized, other) {
			return false, nil
		}
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO live_agent_question_library (tenant_id, room_id, plan_id, question_type, question, answer, keywords_json, created_by_user_id)
VALUES (?, ?, ?, ?, ?, '', ?, NULL)`, tenantID, roomID, planID, questionType, question, gapKeywords(question))
	return err == nil, err
}

func normalizeGapQuestion(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch r {
		case ' ', '\t', '\n', '，', '。', '！', '？', '、', '；', '：', '?', '!', ',', '.', '~', '～':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// gapKeywords derives rough match keywords from the question (bigrams).
func gapKeywords(question string) string {
	runes := []rune(strings.TrimSpace(question))
	keywords := make([]string, 0, 6)
	seen := map[string]bool{}
	for i := 0; i+1 < len(runes); i++ {
		pair := string(runes[i : i+2])
		if strings.ContainsAny(pair, "与的和及或吗呢吧啊") || seen[pair] {
			continue
		}
		seen[pair] = true
		keywords = append(keywords, pair)
		if len(keywords) >= 6 {
			break
		}
	}
	raw, _ := json.Marshal(keywords)
	return string(raw)
}
