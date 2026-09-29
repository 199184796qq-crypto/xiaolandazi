package agentrouting

import (
	"encoding/json"
	"fmt"
	"strings"
)

const ConfigKey = "agent.routing.live_room"

type MatchRule struct {
	ExactAny    []string `json:"exact_any,omitempty"`
	ContainsAny []string `json:"contains_any,omitempty"`
	PrefixAny   []string `json:"prefix_any,omitempty"`
}

type IntentRule struct {
	Description string    `json:"description,omitempty"`
	Match       MatchRule `json:"match"`
}

type Config struct {
	SchemaVersion       int                   `json:"schema_version"`
	FallbackIntent      string                `json:"fallback_intent"`
	ModelEnabled        bool                  `json:"model_enabled"`
	MinModelConfidence  string                `json:"min_model_confidence"`
	AllowNaturalActions map[string]bool       `json:"allow_natural_actions"`
	Priority            []string              `json:"priority"`
	ClassifierPrompt    string                `json:"classifier_prompt"`
	Intents             map[string]IntentRule `json:"intents"`
}

func Default() Config {
	return Config{
		SchemaVersion:      1,
		FallbackIntent:     "chat",
		ModelEnabled:       true,
		MinModelConfidence: "medium",
		AllowNaturalActions: map[string]bool{
			"adopt":     true,
			"execution": true,
		},
		Priority:         []string{"adopt", "test", "learning", "execution"},
		ClassifierPrompt: "结合最近对话、当前工作模式、是否存在学习候选以及用户当前输入，判断这一轮属于 chat、learning、test、execution、adopt 中哪一种。chat 是普通聊天或业务咨询，不写长期记忆；learning 是用户正在纠正、补充、替换或继续打磨当前候选；test 是用户要求验证或模拟测试；execution 是用户要求现在替他回答真实问题或执行正式直播回答；adopt 是用户明确确认采用当前学习成果、保存发布或按当前候选生效。上下文优先于单句字面，短句也可能是在继续回答上一轮纠正。模糊时优先 chat，禁止把普通聊天误写成长期记忆。",
		Intents: map[string]IntentRule{
			"adopt": {
				Description: "确认采用当前学习成果并触发真实保存/发布动作",
				Match: MatchRule{ExactAny: []string{
					"采用", "保存", "发布", "保存发布", "保存并发布", "保存采用", "保存并采用",
					"就按这个", "按这个来", "用这个", "就这样", "确定采用",
				}},
			},
			"test": {
				Description: "测试或验证当前候选，不写入正式记忆",
				Match: MatchRule{ContainsAny: []string{
					"测试下", "测试一下", "你测试", "帮我测试", "试试看", "你试试", "试一下", "测一下", "验证一下", "验证下",
				}},
			},
			"learning": {
				Description: "纠正、补充或继续打磨智能体学习候选",
				Match: MatchRule{ContainsAny: []string{
					"改成", "改为", "换成", "改一下", "修改一下", "纠正一下", "修正一下", "重新改", "重新写", "重新说",
					"不要说", "别说", "统一说", "应该说", "要说成", "说成", "删掉", "去掉", "加上", "补充一下",
					"太官方", "太生硬", "太啰嗦", "不自然", "不准确", "这个说法不对", "这个回答不对", "这样不对",
					"保留好的", "好的保留", "保留正确", "纠正错误", "只改错误", "其他不变", "其余不变",
					"再自然一点", "再口语一点", "再简短一点", "再亲切一点", "再直接一点", "再柔和一点",
				}},
			},
			"execution": {
				Description: "现在替操作者回答真实弹幕，或把操作者指定的话送去直播间播出",
				Match: MatchRule{ContainsAny: []string{
					"帮我回答这条", "替我回答这条", "回答这条弹幕", "回复这条弹幕", "帮我回复这条",
					"给他回复", "给她回复", "给这个用户回复", "直接回答这个问题", "抢答这条",
					"帮我说", "替我说", "帮我播", "替我播", "让直播间说", "让主播说", "让智能体说",
					"直播间说", "直播间播", "主播说一句", "智能体说一句", "直接说", "直接播",
					"说一句", "播一句", "念一下", "念下", "读一下", "读下", "播一下", "播下",
					"直接打断说", "直接打断播",
				}},
			},
		},
	}
}

func DefaultJSON() string {
	raw, _ := json.MarshalIndent(Default(), "", "  ")
	return string(raw)
}

func Parse(raw string) (Config, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &cfg); err != nil {
		return Config{}, fmt.Errorf("路由配置不是有效 JSON: %w", err)
	}
	if cfg.SchemaVersion != 1 {
		return Config{}, fmt.Errorf("schema_version 目前只支持 1")
	}
	cfg.FallbackIntent = NormalizeIntent(cfg.FallbackIntent)
	if cfg.FallbackIntent != "chat" && cfg.FallbackIntent != "learning" && cfg.FallbackIntent != "test" {
		return Config{}, fmt.Errorf("fallback_intent 只能是 chat、learning 或 test")
	}
	if !validConfidence(cfg.MinModelConfidence) {
		return Config{}, fmt.Errorf("min_model_confidence 只能是 high、medium 或 low")
	}
	if cfg.ModelEnabled && strings.TrimSpace(cfg.ClassifierPrompt) == "" {
		return Config{}, fmt.Errorf("model_enabled=true 时 classifier_prompt 不能为空")
	}
	if len(cfg.Priority) == 0 {
		return Config{}, fmt.Errorf("priority 不能为空")
	}
	seen := map[string]bool{}
	for _, rawIntent := range cfg.Priority {
		intent := NormalizeIntent(rawIntent)
		if intent == "chat" || intent == "" {
			return Config{}, fmt.Errorf("priority 只能包含 learning、test、execution、adopt")
		}
		if seen[intent] {
			return Config{}, fmt.Errorf("priority 中存在重复意图 %s", intent)
		}
		seen[intent] = true
		if _, ok := cfg.Intents[intent]; !ok {
			return Config{}, fmt.Errorf("priority 中的意图 %s 缺少 intents 配置", intent)
		}
	}
	for intent := range cfg.Intents {
		normalized := NormalizeIntent(intent)
		if normalized == "" || normalized == "chat" {
			return Config{}, fmt.Errorf("intents 中存在不支持的意图 %s", intent)
		}
	}
	if cfg.AllowNaturalActions == nil {
		cfg.AllowNaturalActions = map[string]bool{}
	}
	return cfg, nil
}

func NormalizeIntent(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chat", "conversation":
		return "chat"
	case "learning", "learn", "edit", "correction":
		return "learning"
	case "test", "preview":
		return "test"
	case "execution", "execute", "answer", "operation":
		return "execution"
	case "adopt", "accept", "publish", "save":
		return "adopt"
	default:
		return ""
	}
}

func NormalizeText(value string) string {
	return strings.NewReplacer(
		" ", "", "\t", "", "\r", "", "\n", "",
		"，", "", ",", "", "。", "", ".", "", "！", "", "!", "", "？", "", "?", "",
		"：", "", ":", "", "；", "", ";", "",
	).Replace(strings.TrimSpace(value))
}

func Match(cfg Config, message string) string {
	message = NormalizeText(message)
	if message == "" {
		return ""
	}
	for _, rawIntent := range cfg.Priority {
		intent := NormalizeIntent(rawIntent)
		rule, ok := cfg.Intents[intent]
		if !ok {
			continue
		}
		for _, pattern := range rule.Match.ExactAny {
			if candidate := NormalizeText(pattern); candidate != "" && message == candidate {
				return intent
			}
		}
		for _, pattern := range rule.Match.PrefixAny {
			if candidate := NormalizeText(pattern); candidate != "" && strings.HasPrefix(message, candidate) {
				return intent
			}
		}
		for _, pattern := range rule.Match.ContainsAny {
			if candidate := NormalizeText(pattern); candidate != "" && strings.Contains(message, candidate) {
				return intent
			}
		}
	}
	return ""
}

func NaturalActionAllowed(cfg Config, intent string) bool {
	return cfg.AllowNaturalActions[NormalizeIntent(intent)]
}

func ConfidenceAtLeast(actual, minimum string) bool {
	rank := map[string]int{"low": 1, "medium": 2, "high": 3}
	return rank[strings.ToLower(strings.TrimSpace(actual))] >= rank[strings.ToLower(strings.TrimSpace(minimum))]
}

func Format(cfg Config) string {
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	return string(raw)
}

func validConfidence(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high", "medium", "low":
		return true
	default:
		return false
	}
}
