package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

// correctionEdit 记录一次纠错，保留原词和修正词，便于页面展示和审计。
type correctionEdit struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // normalize / homophone / typo / object
}

type correctionResult struct {
	Original   string           `json:"original"`
	Corrected  string           `json:"corrected"`
	Edits      []correctionEdit `json:"edits,omitempty"`
	Confidence float64          `json:"confidence"`
}

type danmakuLexEntry struct {
	term string
	kind string
}

type danmakuLexicon struct {
	entries []danmakuLexEntry
	known   map[string]bool
}

// loadDanmakuLexicon 从现有库表收集纠错词表：对象名、属性规范名/别名、商品名、事实主题。
// 不新建表，全部复用线上已有数据。
func (l *lab) loadDanmakuLexicon(ctx context.Context, target plan) (*danmakuLexicon, error) {
	lex := &danmakuLexicon{known: map[string]bool{}}
	add := func(term, kind string) {
		term = strings.TrimSpace(term)
		if len([]rune(term)) < 2 || lex.known[term] {
			return
		}
		// 纯数字/纯英文不参与模糊纠错，避免误改编号和型号。
		cjk := false
		for _, r := range term {
			if unicode.Is(unicode.Han, r) {
				cjk = true
				break
			}
		}
		if !cjk {
			return
		}
		lex.known[term] = true
		lex.entries = append(lex.entries, danmakuLexEntry{term: term, kind: kind})
	}
	if dir := l.loadDirectory(ctx, target.TenantID, target.ID); dir != nil {
		for _, name := range dir.Objects {
			add(name, "object")
		}
	}
	if rows, err := l.db.QueryContext(ctx, `SELECT canonical_name FROM live_fact_attributes WHERE status='active'`); err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				add(name, "attribute")
			}
		}
		rows.Close()
	}
	if rows, err := l.db.QueryContext(ctx, `SELECT alias FROM live_fact_attribute_aliases WHERE status='active'`); err == nil {
		for rows.Next() {
			var alias string
			if rows.Scan(&alias) == nil {
				add(alias, "attribute")
			}
		}
		rows.Close()
	}
	if rows, err := l.db.QueryContext(ctx, `SELECT product_name FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? AND status='active'`, target.TenantID, target.ID); err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				add(name, "product")
			}
		}
		rows.Close()
	}
	if rows, err := l.db.QueryContext(ctx, `SELECT DISTINCT topic FROM live_agent_plan_material_claims WHERE tenant_id=? AND plan_id=? AND status='active'`, target.TenantID, target.ID); err == nil {
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				add(name, "topic")
			}
		}
		rows.Close()
	}
	return lex, nil
}

func (l *lab) lexiconFor(ctx context.Context, target plan) (*danmakuLexicon, error) {
	l.mu.Lock()
	if l.lexicons == nil {
		l.lexicons = map[int64]*danmakuLexicon{}
	}
	cached := l.lexicons[target.ID]
	l.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	loaded, err := l.loadDanmakuLexicon(ctx, target)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.lexicons[target.ID] = loaded
	l.mu.Unlock()
	return loaded, nil
}

// correctDanmaku 只处理文本输入错误：全半角、空格、重复标点、表情、同音/形近错字。
// 不做语义归一（如“邮寄→配送”），语义归一属于查询改写。
func (l *lab) correctDanmaku(ctx context.Context, target plan, text string) correctionResult {
	original := text
	result := correctionResult{Original: original, Corrected: text, Confidence: 1}
	lex, err := l.lexiconFor(ctx, target)
	if err != nil {
		return result
	}
	corrected, normalizeEdits := normalizeDanmakuText(text)
	edits := append([]correctionEdit{}, normalizeEdits...)
	// 最多做 3 轮替换，避免相互影响。
	for round := 0; round < 3; round++ {
		next, edit, ok := fuzzyReplaceOnce(corrected, lex)
		if !ok {
			break
		}
		corrected = next
		edits = append(edits, edit)
	}
	result.Corrected = corrected
	result.Edits = edits
	if len(edits) == 0 {
		result.Confidence = 1
	} else {
		result.Confidence = 0.9
	}
	return result
}

func normalizeDanmakuText(text string) (string, []correctionEdit) {
	edits := []correctionEdit{}
	original := text
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		case r == 0x3000:
			b.WriteRune(' ')
		case r >= 0x1F300 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF:
			// 表情不进入检索。
		default:
			b.WriteRune(r)
		}
	}
	text = b.String()
	// 去掉中文之间的空格，保留数字/英文之间的空格。
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if r == ' ' {
			prev, next := rune(0), rune(0)
			if i > 0 {
				prev = runes[i-1]
			}
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if unicode.Is(unicode.Han, prev) || unicode.Is(unicode.Han, next) {
				continue
			}
		}
		if unicode.IsSpace(r) && r != ' ' {
			continue
		}
		out = append(out, r)
	}
	text = strings.TrimSpace(string(out))
	// 合并重复标点：“？？？”→“？”。
	var collapsed strings.Builder
	var prev rune
	for _, r := range text {
		if strings.ContainsRune("？?！!。，,、；;：:", r) && r == prev {
			continue
		}
		collapsed.WriteRune(r)
		prev = r
	}
	text = collapsed.String()
	// 中文数字编号：一号→1号，方便对象名匹配（只处理 1~10）。
	replacements := []struct{ from, to string }{
		{"十号", "10号"}, {"九号", "9号"}, {"八号", "8号"}, {"七号", "7号"},
		{"六号", "6号"}, {"五号", "5号"}, {"四号", "4号"}, {"三号", "3号"},
		{"二号", "2号"}, {"一号", "1号"},
	}
	for _, item := range replacements {
		if strings.Contains(text, item.from) {
			text = strings.ReplaceAll(text, item.from, item.to)
			edits = append(edits, correctionEdit{From: item.from, To: item.to, Kind: "normalize"})
		}
	}
	if text != original {
		// 没有具体替换项时也记录一次清洗，方便页面提示。
		if len(edits) == 0 {
			edits = append(edits, correctionEdit{From: original, To: text, Kind: "normalize"})
		}
	}
	return text, edits
}

// fuzzyReplaceOnce 在文本中找最像的词表词：优先同音字，其次首字相同的单字错字。
func fuzzyReplaceOnce(text string, lex *danmakuLexicon) (string, correctionEdit, bool) {
	runes := []rune(text)
	bestFrom, bestTo, bestIndex, bestLen := "", "", -1, 0
	bestScore := 2
	for _, entry := range lex.entries {
		termRunes := []rune(entry.term)
		length := len(termRunes)
		if length < 2 {
			continue
		}
		pinyinTerm := pinyinSequence(entry.term)
		for index := 0; index+length <= len(runes); index++ {
			window := string(runes[index : index+length])
			if window == entry.term || lex.known[window] {
				continue
			}
			if containsDigit(window) != containsDigit(entry.term) {
				continue
			}
			// 不改带语气/结构助词的片段，避免把“哪产的”改成“哪产地”。
			if strings.ContainsAny(window, "的了呢吗吧啊呀嘛") {
				continue
			}
			if hasKnownPrefix(lex, window) {
				continue
			}
			score := 2
			if samePinyinSequence(pinyinSequence(window), pinyinTerm) {
				score = 0
			} else if length == 2 {
				// 2 字词只认同音，避免“新疆→新鲜”这类首字相同的误改。
				continue
			} else if length == 3 {
				// 3 字词要求首字相同、只差一字，且窗口里没有再包含已知词（防止级联改）。
				if termRunes[0] == runes[index] && levenshtein(window, entry.term) == 1 && !containsKnownTerm(lex, window) {
					score = 1
				}
			} else if termRunes[0] == runes[index] && levenshtein(window, entry.term) == 1 {
				score = 1
			}
			if score >= 2 {
				continue
			}
			if score < bestScore || (score == bestScore && length > bestLen) {
				bestFrom, bestTo, bestIndex, bestLen, bestScore = window, entry.term, index, length, score
			}
		}
	}
	if bestIndex < 0 {
		return text, correctionEdit{}, false
	}
	out := append([]rune{}, runes[:bestIndex]...)
	out = append(out, []rune(bestTo)...)
	out = append(out, runes[bestIndex+bestLen:]...)
	kind := "typo"
	if bestScore == 0 {
		kind = "homophone"
	}
	return string(out), correctionEdit{From: bestFrom, To: bestTo, Kind: kind}, true
}

// pinyinSequence 取每个字的无声调拼音；非汉字保留原字符。
func pinyinSequence(text string) []string {
	out := []string{}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			values := pinyin.Pinyin(string(r), pinyin.NewArgs())
			if len(values) > 0 && len(values[0]) > 0 {
				out = append(out, values[0][0])
				continue
			}
		}
		out = append(out, string(r))
	}
	return out
}

func samePinyinSequence(a, b []string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// containsKnownTerm 判断窗口里是否已经包含一个已知词，防止级联替换。
func containsKnownTerm(lex *danmakuLexicon, window string) bool {
	for _, entry := range lex.entries {
		if len(entry.term) >= 2 && entry.term != window && strings.Contains(window, entry.term) {
			return true
		}
	}
	return false
}

// hasKnownPrefix 防止把更长规范词的前缀误改（例如“原产”是“原产地”的前缀）。
func hasKnownPrefix(lex *danmakuLexicon, window string) bool {
	for _, entry := range lex.entries {
		if len(entry.term) > len(window) && strings.HasPrefix(entry.term, window) {
			return true
		}
	}
	return false
}

func containsDigit(text string) bool {
	for _, r := range text {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := 0; j <= len(br); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = minInt(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func minInt(values ...int) int {
	min := values[0]
	for _, value := range values[1:] {
		if value < min {
			min = value
		}
	}
	return min
}

func (l *lab) handleCorrect(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PlanID   int64  `json:"plan_id"`
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
		return
	}
	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请输入弹幕"})
		return
	}
	target, err := l.planByID(r.Context(), request.PlanID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, l.correctDanmaku(r.Context(), target, request.Question))
}
