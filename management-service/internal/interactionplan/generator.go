// Package interactionplan is the single content protocol for live interaction.
// Identity, ordering, addressing and playback are deliberately outside the model.
package interactionplan

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

const Protocol = "interaction_v2"

type Scope struct{ Tenant, Room, Session, Plan, Version int64 }
type Snapshot struct {
	Scope                               Scope
	Prompt, P0, P0Markdown, Fingerprint string
	FrozenAt                            time.Time
	Voice                               model.VoiceProfile
	Style                               model.LiveAgentPlanAnchorStyleProfile
}
type Turn struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
type ContextMessage struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at,omitempty"`
}
type Input struct {
	Template        string           `json:"template,omitempty"`
	Kind            string           `json:"kind"`
	Correction      string           `json:"correction,omitempty"`
	Text            string           `json:"text"`
	Original        string           `json:"original,omitempty"`
	Clarification   string           `json:"clarification,omitempty"`
	Recent          []Turn           `json:"recent,omitempty"`
	Context         []ContextMessage `json:"context,omitempty"`
	CurrentMainline string           `json:"current_mainline,omitempty"`
	EngagementToken string           `json:"engagement_token,omitempty"`
	EventCount      int              `json:"event_count,omitempty"`
	AllowClarify    bool             `json:"allow_clarify"`
}
type Intent struct {
	Relation       string `json:"dialogue_relation,omitempty"`
	Relevant       bool   `json:"relevant"`
	Question       bool   `json:"question"`
	Meaning        string `json:"meaning"`
	Type           string `json:"type,omitempty"`
	Priority       string `json:"priority,omitempty"`
	PriorityReason string `json:"priority_reason,omitempty"`
	NeedClarify    bool   `json:"need_clarify"`
	Clarify        string `json:"clarify,omitempty"`
	Continue       bool   `json:"continue_dialogue,omitempty"`
	Command        string `json:"engagement_command,omitempty"`
}

// Envelope is created by the program, never by the model. It travels in the
// existing purpose_knowledge field so the queue retains one exact task/result.
type Envelope struct {
	Template    string    `json:"template,omitempty"`
	EventCount  int       `json:"event_count,omitempty"`
	Protocol    string    `json:"protocol"`
	Task        string    `json:"task"`
	Epoch       string    `json:"epoch"`
	Revision    uint64    `json:"revision"`
	Tenant      int64     `json:"tenant_id"`
	Room        int64     `json:"room_id"`
	Session     int64     `json:"session_id"`
	Plan        int64     `json:"plan_id"`
	Fingerprint string    `json:"fingerprint"`
	Body        string    `json:"body"`
	Intent      Intent    `json:"intent"`
	ExpiresAt   time.Time `json:"expires_at"`
}
type Complete func(context.Context, agentgateway.Request) (agentgateway.Response, error)

func NewSnapshot(scope Scope, prompt, p0, md string) (Snapshot, error) {
	if scope.Tenant <= 0 || scope.Room <= 0 || scope.Plan <= 0 || strings.TrimSpace(prompt) == "" {
		return Snapshot{}, errors.New("当前方案尚未配置方案提示词，互动暂不可用")
	}
	if strings.TrimSpace(p0) == "" || strings.TrimSpace(md) == "" {
		return Snapshot{}, errors.New("互动完整P0不可用")
	}
	if _, err := contentguard.Compile(md); err != nil {
		return Snapshot{}, err
	}
	h := sha256.Sum256([]byte(fmt.Sprint(scope) + "\x00" + prompt + "\x00" + p0 + "\x00" + md))
	return Snapshot{Scope: scope, Prompt: prompt, P0: p0, P0Markdown: md, Fingerprint: fmt.Sprintf("%x", h), FrozenAt: time.Now().UTC()}, nil
}

type Cache struct {
	mu    sync.Mutex
	items map[Scope]Snapshot
}

var Frozen Cache

func (c *Cache) Put(s Snapshot) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[Scope]Snapshot{}
	}
	if old, ok := c.items[s.Scope]; ok {
		return old
	}
	for key := range c.items {
		// An old in-flight session must never evict the newer live snapshot.
		if key.Tenant == s.Scope.Tenant && key.Room == s.Scope.Room && key.Session > s.Scope.Session {
			return s
		}
		if key.Tenant == s.Scope.Tenant && key.Room == s.Scope.Room && key.Session != s.Scope.Session {
			delete(c.items, key)
		}
	}
	c.items[s.Scope] = s
	return s
}
func (c *Cache) Get(scope Scope) (Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.items[scope]
	return s, ok
}
func (c *Cache) ForSession(tenant, room, session int64) (Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, s := range c.items {
		if k.Tenant == tenant && k.Room == room && k.Session == session {
			return s, true
		}
	}
	return Snapshot{}, false
}

type Source interface {
	GetPublishedLiveAgentPlanVersionForRoom(context.Context, int64, int64) (model.LiveAgentPlanVersion, error)
}
type promptSource interface {
	GetLiveAgentPlanPrompt(context.Context, int64, int64) (string, time.Time, error)
}

func Freeze(ctx context.Context, source Source, session model.LiveRuntimeSession) (Snapshot, error) {
	if s, ok := Frozen.ForSession(session.TenantID, session.RoomID, session.ID); ok && session.ID > 0 {
		if strings.TrimSpace(s.Prompt) == "" {
			return s, errors.New("本场冻结的方案提示词为空，请配置后在新场使用")
		}
		return s, nil
	}
	published, err := source.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return Snapshot{}, err
	}
	prompts, ok := source.(promptSource)
	if !ok {
		return Snapshot{}, errors.New("方案提示词读取未接通")
	}
	prompt, _, err := prompts.GetLiveAgentPlanPrompt(ctx, session.TenantID, published.PlanID)
	if err != nil {
		return Snapshot{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		if session.ID > 0 {
			Frozen.Put(Snapshot{Scope: Scope{session.TenantID, session.RoomID, session.ID, published.PlanID, published.ID}, FrozenAt: time.Now().UTC()})
		}
		return Snapshot{}, errors.New("当前方案未配置方案提示词，互动保持主线并跳过")
	}
	p0, err := policy.LoadGeneration()
	if err != nil {
		return Snapshot{}, err
	}
	md, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if path := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_P0_POLICY_PATH")); path != "" {
		md, err = os.ReadFile(path)
	}
	if err != nil {
		return Snapshot{}, err
	}
	s, err := NewSnapshot(Scope{session.TenantID, session.RoomID, session.ID, published.PlanID, published.ID}, prompt, p0.PromptText, string(md))
	if err != nil {
		return s, err
	}
	identity := published.VoiceIdentity
	rate := identity.Rate
	if rates, ok := source.(interface {
		GetPlanVoiceRate(context.Context, int64, int64) (model.PlanVoiceRate, error)
	}); ok {
		if settings, err := rates.GetPlanVoiceRate(ctx, session.TenantID, published.PlanID); err == nil && settings.Rate >= 0.5 && settings.Rate <= 2 {
			rate = settings.Rate
		}
	}
	if styles, ok := source.(interface {
		GetLiveAnchorStyleForPlan(context.Context, int64, int64) (model.LiveAnchorStyle, error)
	}); ok {
		if style, err := styles.GetLiveAnchorStyleForPlan(ctx, session.TenantID, published.PlanID); err == nil {
			s.Style = style.Profile
		}
	}
	if rate < 0.5 || rate > 2 {
		rate = 1
	}
	s.Voice = model.VoiceProfile{Name: identity.Name, Provider: identity.Provider, VoiceID: identity.VoiceID, CloneStatus: "ready", Config: map[string]any{"source": identity.Source, "target_model": identity.Model, "rate": rate, "identity_version": identity.Version, "binding_id": identity.BindingID, "emotion_enabled": identity.EmotionEnabled != nil && *identity.EmotionEnabled}}
	voiceJSON, _ := json.Marshal(struct {
		Voice model.VoiceProfile
		Style model.LiveAgentPlanAnchorStyleProfile
	}{s.Voice, s.Style})
	s.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(append([]byte(s.Fingerprint), voiceJSON...)))
	if session.ID > 0 {
		s = Frozen.Put(s)
	}
	return s, nil
}

// OutputContract 由程序在提交提示词时追加，保证格式与字段稳定；
// 方案提示词本身只写业务，不需要维护输出格式。
const OutputContract = `【输出格式】
一行一个字段，用中文冒号，不要 JSON、不要代码块、不要解释：
意图：观众这句话最可能在问什么
类型：只能是 提问、下单意向、催单、投诉、闲聊、刷屏 里的一个
歧义：是或否
澄清句：歧义为是时写一句反问，否则留空
回答：可直接口播的正文，不写昵称和"这位朋友"这类称呼`

// stripFormatSection 去掉方案提示词里自带的【输出格式要求】段，
// 避免和程序追加的契约冲突。
func stripFormatSection(prompt string) string {
	var lines []string
	skip := false
	for _, line := range strings.Split(prompt, "\n") {
		title := strings.TrimSpace(line)
		if title == "【输出格式要求】" || title == "【输出格式】" {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(title, "【") {
			skip = false
		}
		if !skip {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
func request(s Snapshot, template, stage string, payload any, format agentgateway.ResponseFormat) (agentgateway.Request, error) {
	md, err := interactionpolicy.ReadOptional(template)
	if err != nil {
		return agentgateway.Request{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return agentgateway.Request{}, err
	}
	// 方案提示词里的输出格式段是给人看的规范，每次调用都带既费 token 又和本次
	// 任务的输出契约冲突，拼装时直接剥掉。完整冻结P0仍作为公共内容约束携带。
	// 任务模板为空时不再附加任何指令，模型只按方案提示词回答。
	prompt := stripFormatSection(s.Prompt) + "\n\n" + OutputContract
	messages := []agentgateway.Message{{Role: "system", Content: s.P0 + "\n\n本场冻结的方案提示词：\n" + prompt}}
	if task := strings.TrimSpace(string(md)); task != "" {
		messages = append(messages, agentgateway.Message{Role: "system", Content: task})
	}
	messages = append(messages, agentgateway.Message{Role: "user", Content: string(raw)})
	return agentgateway.Request{Stage: stage, ResponseFormat: format, EnableThinking: false, MaxTokens: 900, Timeout: 12 * time.Second, CacheSystemPrompt: true,
		Messages: messages}, nil
}

// kvParse 解析「键：值」行，支持中英文冒号。互动模型按 KV 输出，
// 程序不再依赖 JSON：模型一旦不守 JSON 整条就会作废。
// normalizeType 把模型给的类型收敛到受控词表；不认识的一律按提问处理，
// 既不报错也不丢互动。
func normalizeType(v string) string {
	v = strings.TrimSpace(v)
	switch v {
	case "提问", "下单意向", "催单", "投诉", "闲聊", "主动互动", "刷屏":
		return v
	}
	switch {
	case strings.Contains(v, "主动互动") || strings.Contains(v, "口令参与") || strings.Contains(v, "互动参与"):
		return "主动互动"
	case strings.Contains(v, "下单") || strings.Contains(v, "购买") || strings.Contains(v, "意向"):
		return "下单意向"
	case strings.Contains(v, "催"):
		return "催单"
	case strings.Contains(v, "投诉") || strings.Contains(v, "抱怨"):
		return "投诉"
	case strings.Contains(v, "刷") || strings.Contains(v, "无效") || strings.Contains(v, "无意义"):
		return "刷屏"
	case strings.Contains(v, "聊") || strings.Contains(v, "寒暄"):
		return "闲聊"
	}
	return "提问"
}
func kvParse(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "`"))
		if line == "" {
			continue
		}
		idx := strings.IndexAny(line, "：:")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		sep := 1
		if strings.HasPrefix(line[idx:], "：") {
			sep = len("：")
		}
		val := strings.TrimSpace(line[idx+sep:])
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func kvBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "是", "true", "yes", "y", "1", "有":
		return true
	}
	return false
}

func kvPick(kv map[string]string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := kv[k]; ok {
			return v, true
		}
	}
	return "", false
}
func Generate(ctx context.Context, s Snapshot, in Input, complete Complete) (Intent, string, error) {
	batch, err := GenerateBatch(ctx, s, []BatchItem{{ID: "U01", Input: in}}, complete)
	if err != nil {
		return Intent{}, "", err
	}
	item := batch.Items[0]
	return item.Intent, item.Body, item.Err
}

// parseSingleContent is shared by the one-person and multi-person contracts.
func parseSingleContent(s Snapshot, in Input, kv map[string]string) (Intent, string, error) {
	var intent Intent
	var err error
	meaning, ok := kvPick(kv, "意图")
	if !ok || strings.TrimSpace(meaning) == "" {
		return intent, "", errors.New("互动结果不是 KV 格式")
	}
	// 母版要求「意图｜置信度｜建议」写在一行，分级只取第一段，其余留在原始回复里入库。
	if i := strings.IndexAny(meaning, "｜|"); i > 0 {
		meaning = meaning[:i]
	}
	intent.Meaning = strings.TrimSpace(meaning)
	intent.Relevant = true
	intent.Question = true
	if v, ok := kvPick(kv, "类型", "type"); ok {
		intent.Type = normalizeType(v)
	}
	if intent.Type == "" {
		intent.Type = "提问"
	}
	intent.Question = intent.Type != "闲聊" && intent.Type != "刷屏" && intent.Type != "主动互动"
	intent.PriorityReason = strings.TrimSpace(kv["优先原因"])
	intent.Priority = "普通"
	if (kv["优先程度"] == "较高" || kv["优先程度"] == "紧急") && intent.PriorityReason != "" {
		intent.Priority = kv["优先程度"]
	}
	if intent.Type == "刷屏" {
		intent.Relevant = false
		return intent, "", nil
	}
	if intent.Type == "主动互动" {
		if in.Kind == "dialogue" {
			return intent, "", errors.New("对话回复不能按主动互动丢弃")
		}
		if in.Kind != "semantic" && in.Kind != "urgent" && in.Kind != "" {
			return intent, "", errors.New("事件播报任务不能标记为观众参与弹幕")
		}
		intent.Priority = "普通"
		return intent, "", nil
	}
	intent.Relation = strings.TrimSpace(kv["对话关系"])
	intent.Continue = kvBool(kv["继续对话"])
	if v, ok := kvPick(kv, "歧义"); ok {
		intent.NeedClarify = kvBool(v)
	}
	if in.Kind != "guide" {
		intent.Command = ""
	}
	if intent.NeedClarify {
		if !in.AllowClarify {
			return intent, "", errors.New("本任务不允许重复澄清")
		}
		clarify, _ := kvPick(kv, "澄清句")
		clarify = strings.TrimSpace(clarify)
		if clarify == "" {
			return intent, "", errors.New("澄清内容为空")
		}
		// 歧义时只播澄清句，不播回答。
		intent.Clarify = withoutAddress(clarify)
		if err = Validate(s, intent.Clarify); err != nil {
			return intent, "", err
		}
		return intent, intent.Clarify, nil
	}
	answer, _ := kvPick(kv, "回答")
	body := strings.TrimSpace(answer)
	if body == "" {
		return intent, "", errors.New("互动结果缺少回答")
	}
	body = withoutAddress(body)
	if err = Validate(s, body); err != nil {
		return intent, "", err
	}
	return intent, body, nil
}

// Remove standalone vocatives before freezing the body. Only entire address
// clauses are removed; occurrences inside facts or an ordinary sentence stay.
var vocative = regexp.MustCompile(`(^|[，,。！？!？；;：:])\s*(这位朋友|各位朋友|朋友们|家人们|各位老乡|老乡们|老乡|家人|几位朋友|这位老乡)\s*[，,！!：:]\s*`)
var answerLabel = regexp.MustCompile(`(?m)^(?:回答|答复|正文)\s*[：:]\s*`)
var analysisLabel = regexp.MustCompile(`(?m)^(?:意图|置信度|歧义|候选|澄清句|建议)\s*[：:]`)

func spokenBody(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```"), "```"))
	// 模型按 KV 输出时先取正文行，避免把意图、歧义等分析行当成口播内容。
	if v, ok := kvPick(kvParse(text), "回答", "正文", "body", "答复", "reply"); ok && strings.TrimSpace(v) != "" {
		text = strings.TrimSpace(v)
	}
	// 不再兼容 JSON：模型回 JSON 视为格式错误，由上层重试或跳过。
	if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
		return "", errors.New("互动正文不是 KV 格式")
	}
	if label := answerLabel.FindStringIndex(text); label != nil {
		if label[0] == 0 || analysisLabel.MatchString(text[:label[0]]) {
			text = strings.TrimSpace(text[label[1]:])
		}
	}
	if analysisLabel.MatchString(text) {
		return "", errors.New("互动正文仍混入分析字段，跳过本任务")
	}
	return text, nil
}

func withoutAddress(text string) string {
	text = strings.TrimSpace(text)
	for {
		next := vocative.ReplaceAllString(text, "$1")
		if next == text {
			return text
		}
		text = strings.TrimSpace(next)
	}
}

func Validate(s Snapshot, text string) error {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 360 {
		return errors.New("互动正文为空或超过360字")
	}
	p, err := contentguard.Compile(s.P0Markdown)
	if err != nil {
		return err
	}
	if hits := p.Scan(text, "all").Hits; len(hits) > 0 {
		// 命中内容必须落日志，否则线上只看到「未通过」，查不出是哪条规则误伤。
		for _, hit := range hits {
			log.Printf("interaction p0 blocked rule=%s category=%s severity=%s matched=%q text=%q", hit.RuleID, hit.Category, hit.Severity, hit.Text, text)
		}
		return errors.New("互动正文未通过有效P0，保持主线并跳过本任务")
	}
	return nil
}

// Join asks for the complete joined text, but only an address plus punctuation
// may precede the frozen body. A failed/late/rewritten answer cannot replace it.
func Join(ctx context.Context, s Snapshot, address, body string, complete Complete) (string, error) {
	if err := Validate(s, body); err != nil {
		return "", err
	}
	address = strings.TrimSpace(address)
	// 正文开头自带的泛称先去掉，避免出现「小王，老乡们，……」这种叠称呼。
	body = strings.TrimSpace(withoutAddress(body))
	body = strings.TrimLeft(body, "，,。！!：:、；; \n\t")
	if address == "" {
		return body, nil
	}
	// 称呼固定在正文之前，由程序拼接，不再让模型改写：
	// 时序稳定（先称呼后内容），也不再为一次拼接多花一次模型调用。
	joined := address + "，" + body
	if err := Validate(s, joined); err != nil {
		return "", err
	}
	return joined, nil
}
func Decode(raw []byte) (Envelope, bool) {
	var e Envelope
	err := json.Unmarshal(raw, &e)
	return e, err == nil && e.Protocol == Protocol && e.Task != ""
}
