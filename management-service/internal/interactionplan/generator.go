// Package interactionplan is the single content protocol for live interaction.
// Identity, ordering, addressing and playback are deliberately outside the model.
package interactionplan

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
type Input struct {
	Kind            string `json:"kind"`
	Correction      string `json:"correction,omitempty"`
	Text            string `json:"text"`
	Original        string `json:"original,omitempty"`
	Clarification   string `json:"clarification,omitempty"`
	Recent          []Turn `json:"recent,omitempty"`
	CurrentMainline string `json:"current_mainline,omitempty"`
	EventCount      int    `json:"event_count,omitempty"`
	AllowClarify    bool   `json:"allow_clarify"`
}
type Intent struct {
	Relation    string `json:"dialogue_relation,omitempty"`
	Relevant    bool   `json:"relevant"`
	Question    bool   `json:"question"`
	Meaning     string `json:"meaning"`
	NeedClarify bool   `json:"need_clarify"`
	Clarify     string `json:"clarify,omitempty"`
	Continue    bool   `json:"continue_dialogue,omitempty"`
	Command     string `json:"engagement_command,omitempty"`
}

// Envelope is created by the program, never by the model. It travels in the
// existing purpose_knowledge field so the queue retains one exact task/result.
type Envelope struct {
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

func request(s Snapshot, template, stage string, payload any, format agentgateway.ResponseFormat) (agentgateway.Request, error) {
	md, err := interactionpolicy.Read(template)
	if err != nil {
		return agentgateway.Request{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return agentgateway.Request{}, err
	}
	return agentgateway.Request{Stage: stage, ResponseFormat: format, EnableThinking: false, MaxTokens: 900, Timeout: 12 * time.Second, CacheSystemPrompt: true,
		Messages: []agentgateway.Message{{Role: "system", Content: "完整有效P0（保留范围、条件与排除，优先于方案）：\n" + s.P0}, {Role: "system", Content: "本场冻结的方案提示词：\n" + s.Prompt}, {Role: "system", Content: string(md)}, {Role: "user", Content: string(raw)}}}, nil
}

func Generate(ctx context.Context, s Snapshot, in Input, complete Complete) (Intent, string, error) {
	var intent Intent
	if complete == nil {
		return intent, "", errors.New("互动模型未配置")
	}
	// No nickname or internal identity is included in any content request.
	if utf8.RuneCountInString(in.Text) > 2400 || len(in.Recent) > 5 {
		return intent, "", errors.New("互动输入超出范围")
	}
	req, err := request(s, "interaction-v2-intent.md", "interaction_purpose_resolution", in, agentgateway.ResponseJSON)
	if err != nil {
		return intent, "", err
	}
	result, err := complete(ctx, req)
	if err != nil {
		return intent, "", err
	}
	raw := strings.TrimSpace(result.Text)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	if err = json.Unmarshal([]byte(strings.TrimSpace(raw)), &intent); err != nil {
		return intent, "", fmt.Errorf("互动意图格式错误: %w", err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(strings.TrimSpace(raw)), &fields)
	if _, ok := fields["relevant"]; !ok {
		return intent, "", errors.New("互动意图缺少相关性判断")
	}
	if intent.Relevant {
		if _, ok := fields["question"]; !ok {
			return intent, "", errors.New("互动意图缺少问题判断")
		}
		if strings.TrimSpace(intent.Meaning) == "" {
			return intent, "", errors.New("互动意图缺少语义内容")
		}
	}
	if in.Kind != "guide" {
		intent.Command = ""
	}
	if !intent.Relevant {
		return intent, "", nil
	}
	if in.Kind == "dialogue" && intent.Relation == "new" {
		in.AllowClarify = true
		in.Original = ""
		in.Clarification = ""
		in.Recent = nil
	}
	if !in.AllowClarify {
		intent.NeedClarify = false
		intent.Clarify = ""
	}
	if intent.NeedClarify && strings.TrimSpace(intent.Clarify) == "" {
		return intent, "", errors.New("澄清内容为空")
	}
	if intent.NeedClarify {
		if err = Validate(s, intent.Clarify); err != nil {
			return intent, "", err
		}
		return intent, strings.TrimSpace(intent.Clarify), nil
	}
	req, err = request(s, "interaction-v2-body.md", "interaction_single_direct_answer", struct {
		Input  Input  `json:"input"`
		Intent Intent `json:"intent"`
	}{in, intent}, agentgateway.ResponseText)
	if err != nil {
		return intent, "", err
	}
	result, err = complete(ctx, req)
	if err != nil {
		return intent, "", err
	}
	body := strings.TrimSpace(result.Text)
	if err = Validate(s, body); err != nil {
		return intent, "", err
	}
	return intent, body, nil
}

func Validate(s Snapshot, text string) error {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 360 {
		return errors.New("互动正文为空或超过360字")
	}
	p, err := contentguard.Compile(s.P0Markdown)
	if err != nil {
		return err
	}
	if len(p.Scan(text, "all").Hits) > 0 {
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
	if address == "" {
		return body, nil
	}
	fallback := address + "，" + body
	if err := Validate(s, fallback); err != nil {
		return "", err
	}
	req, err := request(s, "interaction-v2-address-join.md", "interaction_single_greet", map[string]string{"address": address, "body": body}, agentgateway.ResponseText)
	if err != nil {
		return "", err
	}
	req.Timeout = 3 * time.Second
	req.MaxTokens = 600
	if complete != nil {
		joinCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		response, callErr := complete(joinCtx, req)
		text := strings.TrimSpace(response.Text)
		if callErr == nil && strings.HasSuffix(text, body) {
			prefix := strings.TrimSuffix(text, body)
			strip := func(x string) string { return strings.Trim(x, " ，,。！!：:、；;\n\t") }
			if strip(prefix) == address && Validate(s, text) == nil {
				return text, nil
			}
		}
	}
	return fallback, nil
}

func Decode(raw []byte) (Envelope, bool) {
	var e Envelope
	err := json.Unmarshal(raw, &e)
	return e, err == nil && e.Protocol == Protocol && e.Task != ""
}
