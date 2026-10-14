package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"log"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type nicknameAliasCacheEntry struct {
	Address string
	Expires time.Time
}
type nicknamePlanInput struct {
	Index   int    `json:"-"` // Historical fixture only; never sent to the model.
	RawName string `json:"raw_name"`
	Chars   int    `json:"char_count"`
	Digits  int    `json:"max_continuous_digits"`
	Masked  bool   `json:"masked,omitempty"`
}

func (item decisionItem) promptNicknames() []string {
	names := item.Nicknames
	if item.PromptNicknames != nil {
		names = item.PromptNicknames
	}
	// 兜底：上游任何环节丢了昵称，也绝不返回空，保证所有互动都有可播称呼。
	if len(names) == 0 {
		return []string{"这位朋友"}
	}
	result := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		safe := safeNicknameAddress(name)
		if safe == "" || seen[safe] {
			continue
		}
		seen[safe] = true
		result = append(result, safe)
		if len(result) >= maxPromptNicknames {
			break
		}
	}
	if len(result) == 0 {
		return []string{"这位朋友"}
	}
	return result
}

// maxPromptNicknames 是单次互动最多点名的观众数量；超过时只点前几个，
// 其余由生成模型用"你们几位"等群体称呼覆盖（见母版"多人合并"规则）。
const maxPromptNicknames = 3

// safeNicknameAddress 程序硬过滤昵称（确定性，不调模型）：
// 纯数字/4 位以上连续数字、明确引流/联系方式、明确侮辱词 → 降级"这位朋友"；
// 掩码昵称 → 只取星号前可读前缀。
// 语义层面（命令式/谐音/阴阳怪气/隐含侮辱）由 planNicknameAddresses 调模型判断，
// 不在本函数处理，避免程序词表误伤正常昵称。
func safeNicknameAddress(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if longestContinuousDigits(name) >= 4 {
		return "这位朋友"
	}
	if containsUnsafeNicknameMarker(name) {
		return "这位朋友"
	}
	return nicknameModelName(name)
}

// unsafeNicknameMarkers 是程序可确定的硬违规昵称词元（引流/联系方式/明确侮辱）。
var unsafeNicknameMarkers = []string{
	"微信", "加我", "加v", "加V", "vx", "VX", "wx", "WX", "手机", "电话",
	"qq", "QQ", "抖音号", "快手号", "公众号", "微博", "扫码", "加好友",
	"滚", "骗子", "假货", "垃圾", "拉黑", "滚出",
}

func containsUnsafeNicknameMarker(name string) bool {
	for _, m := range unsafeNicknameMarkers {
		if strings.Contains(name, m) {
			return true
		}
	}
	return false
}

func longestContinuousDigits(name string) int {
	longest, run := 0, 0
	for _, r := range name {
		if unicode.IsDigit(r) {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest
}

// Masked account identities stay in event records. Only the readable prefix
// reaches the addressing model; suffixes are never guessed or reconstructed.
func nicknameModelName(name string) string {
	at := strings.IndexAny(name, "*＊")
	if at < 0 {
		return name
	}
	prefix := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, name[:at])
	prefix = strings.Join(strings.Fields(prefix), " ")
	if prefix == "" {
		return "这位朋友"
	}
	return prefix
}

// Questions, context and verbatim requests may mention the event nickname too.
// Replace those references without changing the stored account identity.
func (item decisionItem) nicknamePromptText(text string) string {
	names := item.promptNicknames()
	positions := make([]int, 0, len(item.Nicknames))
	for i, name := range item.Nicknames {
		if strings.ContainsAny(name, "*＊") {
			positions = append(positions, i)
		}
	}
	sort.SliceStable(positions, func(i, j int) bool {
		return len(item.Nicknames[positions[i]]) > len(item.Nicknames[positions[j]])
	})
	for _, i := range positions {
		alias := nicknameModelName(item.Nicknames[i])
		if i < len(names) && strings.TrimSpace(names[i]) != "" {
			alias = names[i]
		}
		text = strings.ReplaceAll(text, item.Nicknames[i], alias)
	}
	return text
}

func (item decisionItem) nicknameSpeechText(text string) string {
	text = item.nicknamePromptText(text)
	// A model can still emit mask/Markdown characters. These are never spoken.
	return strings.NewReplacer("*", "", "＊", "").Replace(text)
}

func (item decisionItem) nicknamePromptItem() decisionItem {
	questions := make([]string, len(item.SampleQuestions))
	for i, question := range item.SampleQuestions {
		questions[i] = item.nicknamePromptText(question)
	}
	item.SampleQuestions = questions
	item.Title = item.nicknamePromptText(item.Title)
	item.Summary = item.nicknamePromptText(item.Summary)
	item.ReplyHint = item.nicknamePromptText(item.ReplyHint)
	item.OriginMainline = item.nicknamePromptText(item.OriginMainline)
	item.CurrentMainline = item.nicknamePromptText(item.CurrentMainline)
	item.ResumeMainline = item.nicknamePromptText(item.ResumeMainline)
	item.PreviewInstruction = item.nicknamePromptText(item.PreviewInstruction)
	return item
}
func nicknameInput(index int, name string) nicknamePlanInput {
	masked := strings.ContainsAny(name, "*＊")
	name = nicknameModelName(name)
	run, longest := 0, 0
	for _, r := range name {
		if unicode.IsDigit(r) {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return nicknamePlanInput{Index: index, RawName: name, Chars: utf8.RuneCountInString(name), Digits: longest, Masked: masked}
}
func decodeNicknamePlan(raw string, count int) ([]string, error) {
	var output struct {
		Aliases []struct {
			Index   int    `json:"index"`
			Address string `json:"address"`
		} `json:"aliases"`
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&output); e != nil {
		return nil, e
	}
	if d.Decode(new(any)) != io.EOF || len(output.Aliases) != count {
		return nil, fmt.Errorf("昵称规划数量或结构不匹配")
	}
	aliases := make([]string, count)
	for _, row := range output.Aliases {
		if row.Index < 0 || row.Index >= count || aliases[row.Index] != "" || strings.TrimSpace(row.Address) == "" || utf8.RuneCountInString(row.Address) > 24 {
			return nil, fmt.Errorf("昵称规划引用编号或字段无效")
		}
		aliases[row.Index] = strings.TrimSpace(row.Address)
	}
	return aliases, nil
}

func decodeNicknameAddress(raw string) (string, error) {
	address := strings.TrimSpace(raw)
	if strings.HasPrefix(address, "{") {
		var value struct {
			Address string `json:"address"`
		}
		if json.Unmarshal([]byte(address), &value) != nil {
			return "", fmt.Errorf("称呼内容未返回")
		}
		address = strings.TrimSpace(value.Address)
	}
	if address == "" || strings.ContainsAny(address, "\r\n") || utf8.RuneCountInString(address) > 24 {
		return "", fmt.Errorf("称呼内容为空或过长")
	}
	return nicknameModelName(address), nil
}

// A short, separate planning task uses the central system-model role. It
// chooses addresses through prompts, never a nickname keyword blacklist or a
// post-generation semantic veto. Failures use neutral addresses and continue.
func (w *Worker) planNicknameAddresses(ctx context.Context, s model.LiveRuntimeSession, item *decisionItem) {
	if item.PromptNicknames != nil || len(item.Nicknames) == 0 {
		return
	}
	item.PromptNicknames = make([]string, len(item.Nicknames))
	for i := range item.PromptNicknames {
		item.PromptNicknames[i] = "这位朋友"
	}
	mother, e := interactionpolicy.Read("interaction-nickname-plan.md")
	if e != nil || w.agent == nil {
		return
	}
	policy, e := interactionpolicy.Read("interaction-nickname-addressing.md")
	if e != nil {
		return
	}
	mother = append(append(policy, []byte("\n\n")...), mother...)
	p0, e := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if e != nil {
		return
	}
	if _, e = contentguard.Compile(string(p0)); e != nil {
		return
	}
	mother = append(append(p0, []byte("\n\n")...), mother...)
	identity := ""
	if resolver, ok := w.agent.(interface {
		ModelRoleIdentity(context.Context, string) (string, error)
	}); ok {
		identity, e = resolver.ModelRoleIdentity(ctx, "interaction_addressing_plan")
		if e != nil {
			return
		}
	}
	prefs, err := w.store.GetRoomAddressingPreferences(ctx, s.TenantID, s.RoomID)
	if err != nil {
		prefs = model.RoomAddressingPreferences{NamingPreference: "natural"}
	}
	prefJSON, _ := json.Marshal(prefs)
	now := time.Now().UTC()
	if w.now != nil {
		now = w.now().UTC()
	}
	keys := make([]string, len(item.Nicknames))
	pending := []nicknamePlanInput{}
	positions := []int{}
	w.addressMu.Lock()
	if w.nicknameAliases == nil {
		w.nicknameAliases = map[string]nicknameAliasCacheEntry{}
	}
	for key, entry := range w.nicknameAliases {
		if !entry.Expires.After(now) {
			delete(w.nicknameAliases, key)
		}
	}
	for i, name := range item.Nicknames {
		keys[i] = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(s.TenantID, ":", s.RoomID)+":"+identity+":"+string(mother)+":"+string(prefJSON)+":"+name)))
		if cached, ok := w.nicknameAliases[keys[i]]; ok {
			item.PromptNicknames[i] = cached.Address
			continue
		}
		if len(pending) < 16 {
			pending = append(pending, nicknameInput(len(pending), name))
			positions = append(positions, i)
		}
	}
	w.addressMu.Unlock()
	if len(pending) == 0 {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for i, name := range pending {
		// One nickname per call: failures never shift another person's alias.
		packet, _ := json.Marshal(map[string]any{"name": name, "preferred_terms": prefs.PreferredTerms, "blocked_terms": prefs.BlockedTerms})
		response, err := w.agent.Complete(callCtx, agentgateway.Request{Stage: "interaction_addressing_plan", Messages: []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: string(packet)}}, EnableThinking: false, MaxTokens: 160, Timeout: 8 * time.Second})
		alias := ""
		if err == nil {
			alias, err = decodeNicknameAddress(response.Text)
		}
		if err != nil {
			log.Printf("nickname plan fallback tenant=%d room=%d decision=%s reason=planning_unavailable", s.TenantID, s.RoomID, item.ID)
			continue
		}
		pos := positions[i]
		item.PromptNicknames[pos] = alias
		w.addressMu.Lock()
		if len(w.nicknameAliases) >= 512 {
			w.nicknameAliases = map[string]nicknameAliasCacheEntry{}
		}
		w.nicknameAliases[keys[pos]] = nicknameAliasCacheEntry{Address: alias, Expires: now.Add(30 * time.Minute)}
		w.addressMu.Unlock()
	}
}
