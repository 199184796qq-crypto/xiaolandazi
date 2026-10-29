package decisionexecutor

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/model"
)

type fallbackClarification struct {
	Original string
	Expires  time.Time
}

var fallbackClarifications sync.Map

func fallbackClarificationKey(session model.LiveRuntimeSession, item decisionItem) string {
	user := ""
	if len(item.Nicknames) > 0 {
		user = strings.TrimSpace(item.Nicknames[0])
	}
	if user == "" {
		user = strings.TrimSpace(item.resolvedAddress)
	}
	if user == "" {
		return ""
	}
	return strings.Join([]string{
		strconv.FormatInt(session.TenantID, 10),
		strconv.FormatInt(session.RoomID, 10),
		user,
	}, ":")
}

func saveFallbackClarification(session model.LiveRuntimeSession, item decisionItem, original string) bool {
	key := fallbackClarificationKey(session, item)
	if key == "" || strings.TrimSpace(original) == "" {
		return false
	}
	fallbackClarifications.Store(key, fallbackClarification{Original: original, Expires: time.Now().Add(45 * time.Second)})
	return true
}

// takeFallbackClarification only consumes a pending clarification when the next
// user line is an affirmative short answer; unrelated questions are left alone.
func takeFallbackClarification(session model.LiveRuntimeSession, item *decisionItem, question string) string {
	key := fallbackClarificationKey(session, *item)
	if key == "" {
		return ""
	}
	value, ok := fallbackClarifications.Load(key)
	if !ok {
		return ""
	}
	pending := value.(fallbackClarification)
	if time.Now().After(pending.Expires) {
		fallbackClarifications.Delete(key)
		return ""
	}
	q := strings.TrimSpace(question)
	affirmative := q == "是" || q == "对" || q == "嗯" || q == "是的" || q == "对的" || q == "要" || q == "可以"
	if !affirmative {
		negative := strings.Contains(q, "不是") || strings.Contains(q, "不对") || strings.Contains(q, "算了") || strings.Contains(q, "不要")
		if negative || len([]rune(q)) == 0 || len([]rune(q)) > 8 {
			fallbackClarifications.Delete(key)
			return ""
		}
		// A short candidate ("半只"/"新建区") is the answer to the clarification,
		// not a brand-new question. Keep the original question and append it.
		fallbackClarifications.Delete(key)
		item.ClarificationAsked = true
		return pending.Original + "（澄清补充：" + q + "）"
	}
	fallbackClarifications.Delete(key)
	item.ClarificationAsked = true
	return pending.Original
}
