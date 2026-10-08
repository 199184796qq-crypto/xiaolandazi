package anchorstylescore

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const RollingVersion = "rolling-three-part-v1"

type RollingIssue struct {
	Key      string `json:"issue_key"`
	Category string `json:"category"`
	Actual   string `json:"actual"`
	Target   string `json:"target"`
	Evidence []int  `json:"segment_indices,omitempty"`
}
type EdgeGroup struct {
	Key      string   `json:"key"`
	Count    int      `json:"count"`
	Share    float64  `json:"share"`
	Indices  []int    `json:"indices"`
	Adjacent [][2]int `json:"adjacent"`
}
type SegmentEdge struct {
	Index   int    `json:"index"`
	Opening string `json:"opening"`
	Ending  string `json:"ending"`
}
type RollingReport struct {
	Version          string         `json:"version"`
	WindowCount      int            `json:"window_count"`
	WindowLimit      int            `json:"window_limit,omitempty"`
	WindowMode       string         `json:"window_mode,omitempty"`
	ObservationOnly  bool           `json:"observation_only,omitempty"`
	AllowedAddresses []string       `json:"allowed_addresses"`
	Edges            []SegmentEdge  `json:"edges"`
	Openings         []EdgeGroup    `json:"opening_groups"`
	Endings          []EdgeGroup    `json:"ending_groups"`
	Stats            Stats          `json:"stats"`
	Issues           []RollingIssue `json:"issues"`
}

var addressSection = regexp.MustCompile(`(?m)^\s*(?:[0-9]+[.、．]\s*)?(?:观众)?称呼(?:体系)?[：:]([^\n]+)`)
var particleSection = regexp.MustCompile(`(?m)^\s*(?:[0-9]+[.、．]\s*)?语气词[：:]([^\n]+)`)
var quotedAddress = regexp.MustCompile(`[「“"‘]([^」”"’]+)[」”"’]`)
var edgeDelimiter = regexp.MustCompile(`[。！？!?；;\n]+`)
var openingDelimiter = regexp.MustCompile(`[，,。！？!?：:；;\n]`)
var edgeNumbers = regexp.MustCompile(`[0-9０-９]+(?:[.．][0-9０-９]+)?`)

// Suggestions are drawn from the affirmative address section of the confirmed
// style, never from mentions in its blacklist, examples or the product facts.
func StyleAddresses(meta string, b Baseline) []string {
	m := addressSection.FindStringSubmatch(meta)
	if len(m) < 2 {
		return []string{}
	}
	positive := m[1]
	for _, marker := range []string{"反推", "禁用", "禁止", "不会", "别用", "不用", "自称", "自指"} {
		if i := strings.Index(positive, marker); i >= 0 {
			positive = positive[:i]
		}
	}
	terms := []string{}
	for _, match := range quotedAddress.FindAllStringSubmatch(positive, -1) {
		for _, word := range regexp.MustCompile(`[／/、,，]`).Split(match[1], -1) {
			if n := len([]rune(word)); n > 0 && n <= 16 {
				terms = append(terms, word)
			}
		}
	}
	known := append([]string{b.Identity.Audience.Main}, b.Identity.Audience.Other...)
	for _, aliases := range b.Identity.Audience.Aliases {
		known = append(known, aliases...)
	}
	for _, word := range known {
		if word != "" && strings.Contains(positive, word) {
			terms = append(terms, word)
		}
	}
	terms = clean(terms)
	sort.Slice(terms, func(i, j int) bool {
		if len([]rune(terms[i])) != len([]rune(terms[j])) {
			return len([]rune(terms[i])) > len([]rune(terms[j]))
		}
		return terms[i] < terms[j]
	})
	return terms
}

func groupEdges(keys []string, start int) []EdgeGroup {
	groups := map[string]*EdgeGroup{}
	for i, key := range keys {
		if key == "" {
			continue
		}
		if groups[key] == nil {
			groups[key] = &EdgeGroup{Key: key, Indices: []int{}, Adjacent: [][2]int{}}
		}
		g := groups[key]
		g.Count++
		g.Indices = append(g.Indices, start+i)
		if i > 0 && keys[i-1] == key {
			g.Adjacent = append(g.Adjacent, [2]int{start + i - 1, start + i})
		}
	}
	result := []EdgeGroup{}
	for _, g := range groups {
		g.Share = round(float64(g.Count) / float64(len(keys)))
		result = append(result, *g)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Key < result[j].Key
	})
	return result
}

// One API completion is one scheduling segment, even if it contains line breaks.
// Rates are character-weighted over the window; small punctuation expectations
// are not rounded up to a compulsory mark on every short segment.
func AnalyzeRolling(texts []string, start int, b Baseline, meta string) RollingReport {
	r := RollingReport{Version: RollingVersion, WindowCount: len(texts), AllowedAddresses: StyleAddresses(meta, b), Edges: []SegmentEdge{}, Issues: []RollingIssue{}}
	openings, endings := []string{}, []string{}
	for i, text := range texts {
		text = strings.TrimLeftFunc(text, func(c rune) bool { return unicode.IsSpace(c) || strings.ContainsRune("\"“「‘", c) })
		first := openingDelimiter.Split(text, 2)[0]
		opening := string([]rune(first)[:min(8, len([]rune(first)))])
		for _, term := range r.AllowedAddresses {
			if strings.HasPrefix(text, term) {
				opening = term
				break
			}
		}
		for main, aliases := range b.Identity.Audience.Aliases {
			for _, alias := range aliases {
				if opening == alias {
					opening = main
				}
			}
		}
		parts := edgeDelimiter.Split(text, -1)
		ending := ""
		for j := len(parts) - 1; j >= 0; j-- {
			if strings.TrimSpace(parts[j]) != "" {
				ending = strings.TrimSpace(parts[j])
				break
			}
		}
		ending = string([]rune(ending)[max(0, len([]rune(ending))-80):])
		key := edgeNumbers.ReplaceAllString(ending, "#")
		for _, address := range r.AllowedAddresses {
			key = strings.ReplaceAll(key, address, "[称呼]")
		}
		key = strings.Map(func(c rune) rune {
			if unicode.IsSpace(c) || unicode.IsPunct(c) {
				return -1
			}
			return c
		}, key)
		for _, particle := range clean(append([]string{b.Identity.Particles.Main}, b.Identity.Particles.Other...)) {
			key = strings.TrimSuffix(key, particle)
		}
		if len([]rune(key)) < 4 {
			key = ""
		}
		openings = append(openings, opening)
		endings = append(endings, key)
		r.Edges = append(r.Edges, SegmentEdge{start + i, opening, ending})
	}
	r.Openings = groupEdges(openings, start)
	r.Endings = groupEdges(endings, start)
	for _, category := range []string{"opening", "ending"} {
		groups := r.Openings
		if category == "ending" {
			groups = r.Endings
		}
		for _, g := range groups {
			if len(g.Adjacent) > 0 || (len(texts) >= 5 && g.Share > 0.5) {
				r.Issues = append(r.Issues, RollingIssue{category + "_rotation", category, fmt.Sprintf("%s：%d/%d段，占比%.1f%%，相邻重复%v", g.Key, g.Count, len(texts), g.Share*100, g.Adjacent), "相邻段不重复；五段窗口同一种起头或尾句占比不超过50%；不减少主力称呼总量", g.Indices})
				break
			}
		}
	}
	r.Stats = Measure(strings.Join(texts, "\n"), b)
	for _, hit := range r.Stats.Forbidden {
		if hit.Count > 0 {
			r.Issues = append(r.Issues, RollingIssue{"forbidden_word", "style", fmt.Sprintf("禁用词%s：%d次（最近采纳段）", hit.Term, hit.Count), "遵守本方案确认的禁用表达；算账、预告及催单用允许的称呼替换，不新增称呼", nil})
		}
	}
	return rollingDensity(r, texts, b, meta, len(texts) >= 3 && r.Stats.Chars >= 200, true)
}

// Macro statistics are calculated AFTER adoption. Edges retain complete recent
// segments, while density is measured over precisely the last 1000 nonspace
// Unicode characters. A partial window observes, never demands density repairs.
func AnalyzeMacro1000(window string, recent []string, start int, b Baseline, meta string) RollingReport {
	r := AnalyzeRolling(recent, start, b, meta)
	r.Version = "accepted-macro-1000-v1"
	r.WindowMode = "accepted_chars"
	r.WindowLimit = 1000
	issues := r.Issues
	r.Issues = []RollingIssue{}
	// Keep only full-segment opening/ending issues, not the old five-segment
	// density calculations or local bursts. Macro correction never repairs drafts.
	for _, issue := range issues {
		if issue.Category != "style" || issue.Key == "forbidden_word" {
			r.Issues = append(r.Issues, issue)
		}
	}
	window = TailStyleChars(window, 1000)
	r.Stats = Measure(window, b)
	r.ObservationOnly = r.Stats.Chars < 1000
	return rollingDensity(r, []string{window}, b, meta, !r.ObservationOnly, false)
}

// Keep sentence/paragraph whitespace intact for rhythm measurement, but it
// does not consume the character budget used by the calibrated baseline.
func TailStyleChars(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	count := 0
	start := 0
	for i := len(runes) - 1; i >= 0; i-- {
		if !unicode.IsSpace(runes[i]) {
			count++
			if count == limit {
				start = i
				break
			}
		}
	}
	return strings.TrimSpace(string(runes[start:]))
}

func rollingDensity(r RollingReport, texts []string, b Baseline, meta string, eligible, localBursts bool) RollingReport {
	if !eligible {
		return r
	}
	metrics := []struct {
		key    string
		count  int
		target float64
	}{
		{"主力称呼", r.Stats.MainAudienceCount, b.Quant.Audience}, {"自指", r.Stats.SelfCount, b.Quant.Self},
		{"问句", r.Stats.Questions, questionTarget(b)}, {"感叹号", r.Stats.Exclamations, b.Quant.Exclamations},
	}
	for _, v := range r.Stats.Particles {
		// An analysis may include a grammatical fragment as an auxiliary token.
		// Do not turn it into a correction unless the confirmed style actually
		// names that exact token as a particle; keep original score statistics.
		if section := particleSection.FindStringSubmatch(meta); len(section) > 1 && v.Term != b.Identity.Particles.Main {
			listed := false
			for _, match := range quotedAddress.FindAllStringSubmatch(section[1], -1) {
				if strings.TrimSpace(match[1]) == v.Term {
					listed = true
					break
				}
			}
			if !listed {
				continue
			}
		}
		target := 0.0
		if v.Term == b.Identity.Particles.Main {
			target = b.Quant.MainParticle
		}
		for i, term := range b.Identity.Particles.Other {
			if v.Term == term && i == 0 {
				target = b.Quant.Aux1
			}
			if v.Term == term && i == 1 {
				target = b.Quant.Aux2
			}
		}
		metrics = append(metrics, struct {
			key    string
			count  int
			target float64
		}{v.Term, v.Count, target})
	}
	deviations := []RollingIssue{}
	for _, m := range metrics {
		actual := density(m.count, r.Stats.Chars)
		if m.target <= 0 {
			continue
		}
		expected := m.target * float64(r.Stats.Chars) / 100
		punctuation := m.key == "问句" || m.key == "感叹号"
		if (expected >= 2 && actual < m.target*0.8) || (!punctuation && m.count >= 3 && actual > m.target*1.3) {
			deviations = append(deviations, RollingIssue{"style_density", "style", fmt.Sprintf("%s：%d次，%.4g/百字", m.key, m.count, actual), fmt.Sprintf("%s：样本%.4g/百字，稳定区间%.4g~%.4g/百字", m.key, m.target, m.target*0.8, m.target*1.3), nil})
		}
	}
	// Include local bursts even when a window mean happens to be on target.
	if localBursts && len(texts) > 0 && b.Quant.MainParticle > 0 {
		last := Measure(texts[len(texts)-1], b)
		for _, v := range last.Particles {
			if v.Term == b.Identity.Particles.Main && v.Count >= 3 && v.Per100 > b.Quant.MainParticle*1.7 {
				deviations = append([]RollingIssue{{"style_burst", "style", fmt.Sprintf("最新段%s%d次，%.4g/百字，出现局部扎堆", v.Term, v.Count, v.Per100), "下一段恢复自然分布，不补偿、不禁用该语气词", []int{r.Edges[len(r.Edges)-1].Index}}}, deviations...)
			}
		}
	}
	// A bounded correction budget: at most three style issues, not every metric.
	r.Issues = append(r.Issues, deviations[:min(3, len(deviations))]...)
	return r
}
