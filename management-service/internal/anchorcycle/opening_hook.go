package anchorcycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Hook templates are project prompt mothers, not mutable room configuration.
// The selected request snapshot and adopted-ID memory belong to this Redis run.
type OpeningHookTemplate struct {
	ID       string   `json:"id"`
	Kind     string   `json:"类型"`
	Priority string   `json:"优先级"`
	Template string   `json:"模板"`
	Hard     []string `json:"硬槽"`
	Soft     []string `json:"软槽"`
}

type OpeningHookRules struct {
	FullTypes        []string `json:"首场类型"`
	FullPreferred    []string `json:"首场优先类型"`
	ReentryTypes     []string `json:"循环类型"`
	ReentryPreferred []string `json:"循环优先类型"`
	RecentCount      *int     `json:"最近避重次数"`
	CheckNumbers     *bool    `json:"硬槽数字校验"`
	CheckForbidden   *bool    `json:"主播禁用词硬扫"`
}

type OpeningHookBinding struct {
	Source   string `json:"来源"`
	Field    string `json:"字段,omitempty"`
	Category string `json:"分类,omitempty"`
	Key      string `json:"键包含,omitempty"`
	Pattern  string `json:"提取正则,omitempty"`
}

type OpeningHookLibrary struct {
	Rules    OpeningHookRules              `json:"选择规则"`
	Bindings map[string]OpeningHookBinding `json:"槽位来源"`
	Hooks    []OpeningHookTemplate         `json:"钩子库"`
}

type OpeningHook struct {
	ID                 string            `json:"id"`
	Kind               string            `json:"type"`
	Template           string            `json:"template"`
	FilledTemplate     string            `json:"filled_template"`
	HardValues         map[string]string `json:"hard_values"`
	SoftValues         map[string]string `json:"soft_values"`
	Source             string            `json:"source"`
	RecentCount        int               `json:"recent_count"`
	CheckNumbers       bool              `json:"check_numbers"`
	CheckForbidden     bool              `json:"check_forbidden"`
	RequiredQuantities []string          `json:"required_quantities,omitempty"`
}

// OpeningSelection 是首场开场「两次调用」中第一次选材的模型输出。
// 卖点/开场方式由程序按 RunID 轮换决定，角度由模型在该卖点内挑一个。
// LinkKey 记录选定卖点绑定的商品链接（商品专属卖点才有，公共卖点为空），
// Common 标记该卖点是否公共卖点（门店/停车/环境等，跨商品共享，事实不绑单一 LinkKey）。
// 第二次写话术据此分流：商品专属按 LinkKey 筛事实，公共卖点放开 branding 事实。
type OpeningSelection struct {
	SellingPoint string `json:"卖点"`
	Angle        string `json:"角度"`
	OpeningMode  string `json:"开场方式"`
	LinkKey      string `json:"链接,omitempty"`
	Common       bool   `json:"公共,omitempty"`
}

var hookSlot = regexp.MustCompile(`\{([^{}]+)\}`)
var hookJSONBlock = regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")

func ParseOpeningHooks(md string) (*OpeningHookLibrary, error) {
	blocks := hookJSONBlock.FindAllStringSubmatch(md, -1)
	if len(blocks) != 1 {
		return nil, errors.New("开场钩子母版须包含且只包含一个json代码块")
	}
	var library OpeningHookLibrary
	decoder := json.NewDecoder(strings.NewReader(blocks[0][1]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&library); err != nil {
		return nil, fmt.Errorf("开场钩子库结构错误: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("开场钩子库包含多余JSON内容")
	}
	if len(library.Hooks) == 0 || len(library.Hooks) > 200 {
		return nil, errors.New("开场钩子库须有1到200个模板")
	}
	if library.Rules.RecentCount == nil || *library.Rules.RecentCount < 0 || *library.Rules.RecentCount > 30 || library.Rules.CheckNumbers == nil || library.Rules.CheckForbidden == nil || len(library.Rules.FullTypes) == 0 || len(library.Rules.ReentryTypes) == 0 {
		return nil, errors.New("钩子选择规则须声明首场/循环类型、0到30次避重、数字校验和主播禁用词硬扫开关")
	}
	for key, binding := range library.Bindings {
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("钩子槽位来源名称为空")
		}
		switch binding.Source {
		case "当前卖点":
			if binding.Field != "selling_point" && binding.Field != "angle" && binding.Field != "target_audience" && binding.Field != "pain_point" {
				return nil, fmt.Errorf("槽位%s的卖点字段不支持", key)
			}
		case "事实条目":
			if binding.Category == "" {
				return nil, fmt.Errorf("槽位%s未指定事实分类", key)
			}
		case "有效福利":
			if binding.Field == "" {
				return nil, fmt.Errorf("槽位%s未指定福利字段", key)
			}
		default:
			return nil, fmt.Errorf("槽位%s的数据来源不支持", key)
		}
		if binding.Pattern != "" {
			pattern, err := regexp.Compile(binding.Pattern)
			if err != nil || pattern.NumSubexp() != 1 {
				return nil, fmt.Errorf("槽位%s提取正则须合法且只有一个捕获组", key)
			}
		}
	}
	ids := map[string]bool{}
	kinds := map[string]bool{}
	for _, h := range library.Hooks {
		if strings.TrimSpace(h.ID) == "" || len(h.ID) > 100 || ids[h.ID] || strings.TrimSpace(h.Template) == "" || len([]rune(h.Template)) > 300 {
			return nil, errors.New("开场钩子ID须唯一，模板不得为空或超过300字")
		}
		ids[h.ID] = true
		if strings.TrimSpace(h.Kind) == "" {
			return nil, fmt.Errorf("钩子%s类型为空", h.ID)
		}
		kinds[h.Kind] = true
		if h.Priority != "首场/重钩子" && h.Priority != "循环/轻钩子" {
			return nil, fmt.Errorf("钩子%s的适用开场不支持", h.ID)
		}
		slots := map[string]bool{}
		for _, key := range h.Hard {
			binding, exists := library.Bindings[key]
			if !exists || binding.Source == "当前卖点" || slots[key] {
				return nil, fmt.Errorf("钩子%s硬槽须绑定事实来源且不得重复: %s", h.ID, key)
			}
			slots[key] = true
		}
		for _, key := range h.Soft {
			if _, exists := library.Bindings[key]; !exists || slots[key] {
				return nil, fmt.Errorf("钩子%s软槽未绑定来源或重复: %s", h.ID, key)
			}
			slots[key] = true
		}
		used := map[string]bool{}
		for _, match := range hookSlot.FindAllStringSubmatch(h.Template, -1) {
			if !slots[match[1]] {
				return nil, fmt.Errorf("钩子%s的槽位未声明: %s", h.ID, match[1])
			}
			used[match[1]] = true
		}
		if len(used) != len(slots) || strings.Contains(hookSlot.ReplaceAllString(h.Template, ""), "{") || strings.Contains(hookSlot.ReplaceAllString(h.Template, ""), "}") {
			return nil, fmt.Errorf("钩子%s槽位声明与模板不一致", h.ID)
		}
	}
	for _, group := range []struct {
		allowed, preferred []string
	}{{library.Rules.FullTypes, library.Rules.FullPreferred}, {library.Rules.ReentryTypes, library.Rules.ReentryPreferred}} {
		for _, kind := range group.allowed {
			if !kinds[kind] {
				return nil, fmt.Errorf("选择规则类型%s没有模板", kind)
			}
		}
		for _, kind := range group.preferred {
			if !containsHookType(group.allowed, kind) {
				return nil, errors.New("优先类型须在允许类型中")
			}
		}
	}
	return &library, nil
}

func containsHookType(kinds []string, kind string) bool {
	for _, value := range kinds {
		if value == kind {
			return true
		}
	}
	return false
}

// SelectOpeningHook does not consume an ID. Retries retain their request
// snapshot; only AddCandidate adoption records the ID in the recent queue.
func (s *State) SelectOpeningHook(library *OpeningHookLibrary, values map[string]string, source string) error {
	if s.Phase != "opening" || s.PendingOpeningHook != nil {
		return nil
	}
	allowed, preferred := library.Rules.ReentryTypes, library.Rules.ReentryPreferred
	if s.OpeningCount == 0 {
		allowed, preferred = library.Rules.FullTypes, library.Rules.FullPreferred
	}
	recentIDs := []string{}
	for _, segment := range s.Accepted {
		if segment.OpeningHook != nil {
			recentIDs = append(recentIDs, segment.OpeningHook.ID)
		}
	}
	if len(recentIDs) == 0 {
		recentIDs = append(recentIDs, s.RecentOpeningHookIDs...)
	}
	recentIDs = recentIDs[max(0, len(recentIDs)-*library.Rules.RecentCount):]
	eligible := []OpeningHook{}
	for _, h := range library.Hooks {
		if !containsHookType(allowed, h.Kind) {
			continue
		}
		recent := false
		for _, id := range recentIDs {
			if id == h.ID {
				recent = true
			}
		}
		if recent {
			continue
		}
		choice := OpeningHook{ID: h.ID, Kind: h.Kind, Template: h.Template, FilledTemplate: h.Template, Source: source, RecentCount: *library.Rules.RecentCount, CheckNumbers: *library.Rules.CheckNumbers, CheckForbidden: *library.Rules.CheckForbidden, HardValues: map[string]string{}, SoftValues: map[string]string{}}
		complete := true
		for _, key := range h.Hard {
			value := strings.TrimSpace(values[key])
			if value == "" {
				complete = false
				break
			}
			choice.HardValues[key] = value
			choice.FilledTemplate = strings.ReplaceAll(choice.FilledTemplate, "{"+key+"}", value)
		}
		for _, key := range h.Soft {
			value := strings.TrimSpace(values[key])
			if value == "" {
				complete = false
				break
			}
			choice.SoftValues[key] = value
		}
		if complete {
			choice.RequiredQuantities = hookQuantity.FindAllString(choice.FilledTemplate, -1)
			eligible = append(eligible, choice)
		}
	}
	if len(eligible) == 0 {
		return fmt.Errorf("没有满足本轮资料且避开最近%d次的开场钩子，请补充钩子母版或所需资料", *library.Rules.RecentCount)
	}
	for _, kind := range preferred {
		choices := []OpeningHook{}
		for _, h := range eligible {
			if h.Kind == kind {
				choices = append(choices, h)
			}
		}
		if len(choices) > 0 {
			eligible = choices
			break
		}
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(s.RunID + ":opening:" + strconv.Itoa(s.OpeningCount)))
	choice := eligible[int(hash.Sum32()%uint32(len(eligible)))]
	s.PendingOpeningHook = &choice
	return nil
}

var hookQuantity = regexp.MustCompile(`[0-9零〇一二三四五六七八九十百千万两]+(?:\.[0-9]+)?(?:斤|两|年|升|块|元|天|小时|分钟|%)`)

// This is a limited returned-value check, not a semantic/factual judge. Only
// quantities in hard source values are locked; other stages are not inspected.
func (s *State) openingHookProblem(text string) string {
	if s.Phase != "opening" || s.PendingOpeningHook == nil || !s.PendingOpeningHook.CheckNumbers {
		return ""
	}
	actual := map[string]bool{}
	for _, q := range hookQuantity.FindAllString(text, -1) {
		actual[normalizeHookQuantity(q)] = true
	}
	for _, q := range s.PendingOpeningHook.RequiredQuantities {
		// A hard hook slot cannot require a quantity forbidden by P0. Only
		// waive that conflicting slot, not all remaining opening checks.
		if s.P0PolicyMD != "" && s.CheckP0Voice(q) != nil {
			continue
		}
		if !actual[normalizeHookQuantity(q)] {
			return "opening_hook_hard_value_missing"
		}
	}
	return ""
}

var hookQuantityParts = regexp.MustCompile(`^([0-9零〇一二三四五六七八九十百千万两]+(?:\.[0-9]+)?)(斤|两|年|升|块|元|天|小时|分钟|%)$`)

func normalizeHookQuantity(value string) string {
	parts := hookQuantityParts.FindStringSubmatch(value)
	if len(parts) != 3 || strings.ContainsAny(parts[1], "0123456789.") {
		return value
	}
	chars := []rune(parts[1])
	if !strings.ContainsAny(parts[1], "十百千万") {
		out := ""
		for _, r := range chars {
			if r == '零' || r == '〇' {
				out += "0"
			} else if r == '两' {
				out += "2"
			} else {
				out += strconv.Itoa(strings.IndexRune("一二三四五六七八九", r)/3 + 1)
			}
		}
		return out + parts[2]
	}
	number, section, digit := 0, 0, 0
	for _, r := range chars {
		if r == '零' || r == '〇' {
			digit = 0
			continue
		}
		if r == '两' {
			digit = 2
			continue
		}
		if index := strings.IndexRune("一二三四五六七八九", r); index >= 0 {
			digit = index/3 + 1
			continue
		}
		unit := map[rune]int{'十': 10, '百': 100, '千': 1000, '万': 10000}[r]
		if unit == 10000 {
			section += digit
			number += section * unit
			section, digit = 0, 0
		} else {
			if digit == 0 {
				digit = 1
			}
			section += digit * unit
			digit = 0
		}
	}
	return strconv.Itoa(number+section+digit) + parts[2]
}
