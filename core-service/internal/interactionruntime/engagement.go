package interactionruntime

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var numericCall = regexp.MustCompile(`(?:扣|打|刷)(?:一个|一下|个|上|出)?\s*[“"「『]?([0-9０-９]{1,8}|[零一二三四五六七八九十]{1,4})`)
var quotedCall = regexp.MustCompile(`(?:扣|打|刷|回复|发送|发)(?:一个|一句|一下|个|出)?\s*[“"「『]([^”"」』，。！？；\n]{1,16})[”"」』]`)
var shortWordCall = regexp.MustCompile(`(?:扣|打|刷|回复)(?:一个|一句|一下|个|出)?\s*(想要|收到|支持|安排|明白|听懂了|来了|麻辣|五香)(?:[，。！？!?；;\s]|$)`)
var negativeCall = regexp.MustCompile(`(?:不要|不用|不必|禁止|别)(?:再|去|来|继续|大家|各位|给我|一直|重复|乱|随便|刷屏|给|我|都|总|先)*\s*$`)
var putCall = regexp.MustCompile(`把\s*[“"「『]?([^”"」』，。！？；\n]{1,16}?)[”"」』]?\s*(?:打在|发到|刷在)(?:公屏|评论区)`)

// ExtractEngagementTokens only recognizes an explicit spoken instruction.
// Repetition, a price, a product number or the phrase "想要" alone is not one.
func ExtractEngagementTokens(text string) []string {
	if utf8.RuneCountInString(text) > 8000 {
		return nil
	}
	type match struct {
		at    int
		token string
	}
	var matches []match
	for _, pattern := range []*regexp.Regexp{numericCall, quotedCall, putCall, shortWordCall} {
		for _, m := range pattern.FindAllStringSubmatchIndex(text, -1) {
			start := m[0]
			prefix := text[:start]
			if i := strings.LastIndexAny(prefix, "。！？!?；;，,\n"); i >= 0 {
				prefix = prefix[i+1:]
			}
			if negativeCall.MatchString(prefix) {
				continue
			}
			if pattern == numericCall && m[1] < len(text) {
				next, _ := utf8.DecodeRuneInString(text[m[1]:])
				if strings.ContainsRune("号斤块元只个天点折分度", next) || unicode.IsDigit(next) {
					continue
				}
			}
			token := echoKey(text[m[2]:m[3]])
			if token != "" {
				matches = append(matches, match{start, token})
			}
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].at < matches[j].at })
	var out []string
	seen := map[string]bool{}
	for _, m := range matches {
		if !seen[m.token] && len(out) < 8 {
			out = append(out, m.token)
			seen[m.token] = true
		}
	}
	return out
}

func echoKey(text string) string {
	text = strings.TrimFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) })
	text = strings.Map(func(r rune) rune {
		if r >= '０' && r <= '９' {
			return '0' + r - '０'
		}
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
	for i, s := range []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"} {
		if text == s {
			return fmt.Sprint(i)
		}
	}
	if text == "十" {
		return "10"
	}
	return text
}
func pureNumber(text string) bool {
	text = echoKey(text)
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsDigit(r) && r != '.' && r != ',' {
			return false
		}
	}
	return strings.IndexFunc(text, unicode.IsDigit) >= 0
}
func sameEcho(text, token string) bool {
	text, token = echoKey(text), echoKey(token)
	if text == token {
		return true
	}
	if !pureNumber(token) || len(text) < len(token) || token == "" {
		return false
	}
	// "扣666" also accepts a longer run of sixes, but "扣1" never
	// consumes "1号多少钱" or the different numeric option "10".
	runes := []rune(token)
	for _, r := range runes {
		if r != runes[0] {
			return false
		}
	}
	for _, r := range text {
		if r != runes[0] {
			return false
		}
	}
	return true
}
func commandTokens(c *command) []string {
	if len(c.Tokens) > 0 {
		return c.Tokens
	}
	return []string{c.Token}
}
func commandLabel(c *command) string { return strings.Join(commandTokens(c), "、") }
func firstUID(w Work) string {
	if len(w.People) == 1 {
		return w.People[0].UID
	}
	return ""
}

// ObserveSpoken receives only a sentence/segment that entered the PCM output,
// never queued text, future captions, model drafts or downloaded audio.
func (e *Engine) ObserveSpoken(s Scope, id, text string, at, now time.Time) {
	e.ObserveSpokenWindow(s, id, text, at, time.Time{}, now)
}
func (e *Engine) ObserveSpokenWindow(s Scope, id, text string, at, until, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if id == "" || at.IsZero() || at.After(now.Add(time.Second)) || now.Sub(at) > 10*time.Second {
		return
	}
	r := e.rooms[s.Room]
	if r != nil && r.Scope != s {
		return
	}
	if r == nil {
		r = e.room(s, now)
	}
	if !enabled(r, "engagement") {
		return
	}
	if _, seen := r.Spoken[id]; seen {
		return
	}
	e.observeSpoken(r, id, text, at)
	if c := r.Command; c != nil && !at.Before(c.Opened) && until.After(c.PromptUntil) && until.Sub(at) <= 10*time.Minute && commandLabel(c) == strings.Join(ExtractEngagementTokens(text), "、") {
		c.PromptUntil = until
	}
}
func (e *Engine) observeSpoken(r *room, id, text string, at time.Time) {
	if r.Spoken == nil {
		r.Spoken = map[string]time.Time{}
	}
	if _, ok := r.Spoken[id]; ok {
		return
	}
	for key, t := range r.Spoken {
		if at.Sub(t) > 3*time.Minute {
			delete(r.Spoken, key)
		}
	}
	if len(r.Spoken) >= 2048 {
		return
	}
	r.Spoken[id] = at
	tokens := ExtractEngagementTokens(text)
	if len(tokens) == 0 {
		return
	}
	if c := r.Command; c != nil {
		if at.Before(c.Opened) {
			return
		}
		if commandLabel(c) == strings.Join(tokens, "、") && !commandQuiet(c, at) {
			return
		}
		e.finishCommand(r, c, at)
	}
	r.Command = &command{Token: tokens[0], Tokens: tokens, Opened: at, Last: at, Users: map[string]bool{}}
}
func (e *Engine) consumeParticipation(r *room, v Event, now time.Time) bool {
	if c := r.Command; c != nil {
		if commandQuiet(c, now) {
			e.finishCommand(r, c, now)
			r.Command = nil
		} else if !v.At.Before(c.Opened) {
			for _, token := range commandTokens(c) {
				if sameEcho(v.Text, token) {
					if v.UID != "" && !c.Users[v.UID] && len(c.Users) < 2048 {
						c.Users[v.UID] = true
						c.Last = now
					}
					return true
				}
			}
		}
	}
	if pureNumber(v.Text) {
		// Unassociated numeric input is held as participation evidence; it
		// does not invent a question, a spam verdict or a spoken vote count.
		e.recordParticipation(r, v.Text, v.UID, v.At, false)
		return true
	}
	return false
}
func (e *Engine) recordParticipation(r *room, text, uid string, at time.Time, confirmed bool) {
	if uid == "" || utf8.RuneCountInString(text) > 32 {
		return
	}
	if r.Participation == nil {
		r.Participation = map[string]*command{}
	}
	key := echoKey(text)
	if key == "" {
		return
	}
	// A model-confirmed participation group and a numeric holding group are
	// kept separate: pending numbers cannot silently become a spoken count.
	if confirmed {
		key = "model:" + key
	} else {
		key = "pending:" + key
	}
	for k, c := range r.Participation {
		if at.Sub(c.Last) >= 30*time.Second {
			delete(r.Participation, k)
		}
	}
	c := r.Participation[key]
	if c == nil {
		if len(r.Participation) >= 32 {
			return
		}
		c = &command{Token: echoKey(text), Opened: at, Last: at, Users: map[string]bool{}}
		r.Participation[key] = c
	}
	if !c.Users[uid] && len(c.Users) < 2048 {
		c.Users[uid] = true
		if at.After(c.Last) {
			c.Last = at
		}
	}
}
func (e *Engine) finishCommand(r *room, c *command, now time.Time) {
	count := len(c.Users)
	if count >= 3 {
		e.add(r, Work{Scope: r.Scope, Kind: "engagement", Text: fmt.Sprintf("本轮已播口令%s，共有%d位不同观众参与，请自然回应统计结果。", commandLabel(c), count), At: now, Due: now, Expires: now.Add(30 * time.Second), Count: count})
	} else if count > 0 {
		r.Carry = fmt.Sprintf("刚才有%d位观众回应口令%s，可在本次正文中简短接住。", count, commandLabel(c))
	}
}
func (e *Engine) finishParticipation(r *room, now time.Time) {
	if c := r.Command; c != nil && commandQuiet(c, now) {
		e.finishCommand(r, c, now)
		r.Command = nil
	}
	for key, c := range r.Participation {
		if now.Sub(c.Last) < 10*time.Second {
			continue
		}
		if strings.HasPrefix(key, "model:") && len(c.Users) >= 3 {
			e.add(r, Work{Scope: r.Scope, Kind: "engagement", Text: fmt.Sprintf("有%d位不同观众发送相同的互动参与内容“%s”，请简短回应；未关联已播口令，不要声称主播刚才发起了该口令。", len(c.Users), c.Token), At: now, Due: now, Expires: now.Add(30 * time.Second), Count: len(c.Users)})
		}
		delete(r.Participation, key)
	}
}

func commandQuiet(c *command, now time.Time) bool {
	tail := c.Last
	if c.PromptUntil.After(tail) {
		tail = c.PromptUntil
	}
	return now.Sub(tail) >= 10*time.Second
}
func rememberSemantic(r *room, w Work, intent Intent) {
	uid := firstUID(w)
	if !semanticKind(w.Kind) || uid == "" || !intent.Relevant || intent.Type == "主动互动" || intent.Type == "刷屏" {
		return
	}
	if r.SemanticUsers == nil {
		r.SemanticUsers = map[string]time.Time{}
	}
	if w.At.After(r.SemanticUsers[uid]) {
		r.SemanticUsers[uid] = w.At
	}
}
