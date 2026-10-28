package decisionexecutor

import (
	"context"
	"strings"

	"livecompanion/management/internal/model"
)

type questionLibrarySource interface {
	ListQuestionLibrary(context.Context, int64, int64) ([]model.QuestionLibraryEntry, error)
}

// normalizeQuestion strips spaces and punctuation so short viewer questions can
// be matched against library entries.
func normalizeQuestion(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case ' ', '\t', '\n', '，', '。', '！', '？', '、', '；', '：', '?', '!', ',', '.', '~', '～':
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// matchQuestionLibrary returns the best matching active entry: entries may hold
// several keywords separated by 、,，/, and the longest match wins.
func (w *Worker) matchQuestionLibrary(ctx context.Context, session model.LiveRuntimeSession, question string) (model.QuestionLibraryEntry, bool) {
	source, ok := w.store.(questionLibrarySource)
	if !ok || session.RoomID <= 0 {
		return model.QuestionLibraryEntry{}, false
	}
	entries, err := source.ListQuestionLibrary(ctx, session.TenantID, session.RoomID)
	if err != nil || len(entries) == 0 {
		return model.QuestionLibraryEntry{}, false
	}
	normalized := normalizeQuestion(question)
	if normalized == "" {
		return model.QuestionLibraryEntry{}, false
	}
	best := model.QuestionLibraryEntry{}
	bestLen := 0
	for _, entry := range entries {
		if length, ok := matchLibraryEntry(entry, normalized); ok && length > bestLen {
			best = entry
			bestLen = length
		}
	}
	if bestLen == 0 {
		return model.QuestionLibraryEntry{}, false
	}
	return best, true
}

func splitQuestionKeywords(value string) []string {
	replacer := strings.NewReplacer("，", "、", ",", "、", "/", "、", " ", "、", "|", "、")
	parts := strings.Split(replacer.Replace(value), "、")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// matchLibraryEntry reports the longest keyword hit for one entry. Entries with
// exclusion words are skipped when any exclusion appears in the question, so
// "砍" no longer matches "砍价".
func matchLibraryEntry(entry model.QuestionLibraryEntry, normalized string) (int, bool) {
	if strings.TrimSpace(entry.Answer) == "" {
		return 0, false
	}
	for _, word := range entry.ExcludeWords {
		key := normalizeQuestion(word)
		if key != "" && strings.Contains(normalized, key) {
			return 0, false
		}
	}
	best := 0
	for _, token := range splitQuestionKeywords(entry.Question) {
		key := normalizeQuestion(token)
		if key == "" {
			continue
		}
		if strings.Contains(normalized, key) && len([]rune(key)) > best {
			best = len([]rune(key))
		}
	}
	return best, best > 0
}
