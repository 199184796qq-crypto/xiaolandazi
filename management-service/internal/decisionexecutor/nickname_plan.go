package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"livecompanion/management/internal/addressing"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type nicknameAliasCacheEntry struct {
	Address string
	Short   string
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
	// 只使用程序校验过的基础昵称；未规划时一律用中性代称，绝不回退原文昵称。
	if item.PromptNicknames == nil || len(item.PromptNicknames) == 0 {
		return []string{"这位朋友"}
	}
	names := item.PromptNicknames
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
	if containsSemanticAddressTerm(name) {
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

// semanticAddressTerms 是不能作为“称呼”的语义关系词。
// “叫我爸爸”不是昵称/称谓偏好，而是语义越界；无论用户怎么要求都不叫。
var semanticAddressTerms = []string{
	"爸爸", "妈妈", "爷爷", "奶奶", "外公", "外婆",
	"老公", "老婆", "主人",
}

func containsSemanticAddressTerm(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, term := range semanticAddressTerms {
		if strings.Contains(value, term) {
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

// addressSuffixTerms 是昵称末尾的称谓词表。命中时简称取「核心 1-2 字 + 该称谓」，
// 让简称既短又保留叫得出口的称呼（如「小陈哥」「刘老板」「王师傅」）。
// 末尾称谓只针对词表内的明确称谓，避免把「家」「的」等主题/虚词误判成称谓。
var addressSuffixTerms = []string{
	"老板", "老师", "师傅", "朋友", "同学", "美女", "帅哥",
	"哥", "姐", "弟", "妹", "叔", "姨", "爷", "婆", "嫂", "伯", "婶", "总", "导",
}

// nicknameAddressSuffix 返回昵称末尾命中的最长称谓词，无则空串。
func nicknameAddressSuffix(name string) string {
	name = strings.TrimSpace(name)
	best := ""
	for _, term := range addressSuffixTerms {
		if strings.HasSuffix(name, term) && utf8.RuneCountInString(term) > utf8.RuneCountInString(best) {
			best = term
		}
	}
	return best
}

// nicknameShorteningHint 根据昵称字数与末尾称谓，生成传给模型的简称约束。
// 程序只定「长度目标」，具体取哪个核心字由模型在约束内选择（程序取不准核心）。
func nicknameShorteningHint(name string) string {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if suffix := nicknameAddressSuffix(name); suffix != "" {
		return "末尾带称谓「" + suffix + "」，简称取核心 1-2 字再带上该称谓（如「小陈哥」「刘老板」）"
	}
	if n <= 4 {
		return "仅 " + strconv.Itoa(n) + " 字，直接使用全称，不要简称"
	}
	return "共 " + strconv.Itoa(n) + " 字，取最有辨识度的核心，最多 4 字（不机械截前 4 字）"
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
// planNicknameAddresses fills PromptNicknames (基础昵称) and PromptShortNames
// (1-2 字简称). The program owns compliance, identity and validation; the model
// is called at most once, only for names the program marked ambiguous.
func (w *Worker) planNicknameAddresses(ctx context.Context, s model.LiveRuntimeSession, item *decisionItem) {
	if item.PromptNicknames != nil || len(item.Nicknames) == 0 {
		return
	}
	rules := w.nicknameRules()
	item.PromptNicknames = make([]string, len(item.Nicknames))
	item.PromptShortNames = make([]string, len(item.Nicknames))
	for i := range item.PromptNicknames {
		item.PromptNicknames[i] = "这位朋友"
		item.PromptShortNames[i] = "朋友"
	}
	now := time.Now().UTC()
	if w.now != nil {
		now = w.now().UTC()
	}
	identity := ""
	if resolver, ok := w.agent.(interface {
		ModelRoleIdentity(context.Context, string) (string, error)
	}); ok {
		var e error
		identity, e = resolver.ModelRoleIdentity(ctx, "interaction_addressing_plan")
		if e != nil {
			return
		}
	}
	keys := make([]string, len(item.Nicknames))
	var grey []addressing.GreyCandidate
	var greyPositions []int
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
		keys[i] = nicknameCacheKey(s, identity, rules.Version, name)
		if cached, ok := w.nicknameAliases[keys[i]]; ok {
			item.PromptNicknames[i] = cached.Address
			item.PromptShortNames[i] = cached.Short
			continue
		}
		decision := rules.Check(name)
		switch decision.Status {
		case addressing.StatusSafe, addressing.StatusRewritten:
			item.PromptNicknames[i] = decision.Text
			item.PromptShortNames[i] = decision.Short
			w.cacheNickname(keys[i], decision.Text, decision.Short, now)
		case addressing.StatusGrey:
			grey = append(grey, addressing.GreyCandidate{Viewer: addressing.Viewer{UserID: strconv.Itoa(i), RawName: name}, Candidate: decision.Text})
			greyPositions = append(greyPositions, i)
		}
	}
	w.addressMu.Unlock()
	if len(grey) == 0 || w.agent == nil {
		if w.agent != nil {
			w.phraseAddress(ctx, s, item, rules)
		}
		return
	}
	system, user := addressing.GreyPrompt(grey, rules)
	response, err := w.completePaidModel(ctx, s, agentgateway.Request{
		Stage:          "interaction_addressing_plan",
		Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		EnableThinking: false,
		MaxTokens:      60*len(grey) + 80,
		Timeout:        12 * time.Second,
	}, item.ID+":nickname:0")
	if err != nil {
		log.Printf("nickname grey fallback tenant=%d room=%d decision=%s batch=%d reason=planning_unavailable", s.TenantID, s.RoomID, item.ID, len(grey))
		return
	}
	decisions, err := addressing.ParseGrey(response.Text, grey, rules)
	if err != nil {
		log.Printf("nickname grey invalid tenant=%d room=%d decision=%s reason=%v", s.TenantID, s.RoomID, item.ID, err)
		return
	}
	w.addressMu.Lock()
	for i, decision := range decisions {
		if !decision.OK {
			continue
		}
		pos := greyPositions[i]
		item.PromptNicknames[pos] = decision.Base
		item.PromptShortNames[pos] = decision.Short
		w.cacheNickname(keys[pos], decision.Base, decision.Short, now)
	}
	w.addressMu.Unlock()
	w.phraseAddress(ctx, s, item, rules)
}

// phraseAddress builds one spoken address sentence for this interaction. The
// program supplies names and forms; the model only arranges them.
func (w *Worker) phraseAddress(ctx context.Context, s model.LiveRuntimeSession, item *decisionItem, rules *addressing.Rules) {
	var people []string
	var forms []addressing.FormSet
	terms := []string{}
	if prefs, err := w.store.GetRoomAddressingPreferences(ctx, s.TenantID, s.RoomID); err == nil {
		terms = prefs.PreferredTerms
	}
	resolvedTerms, _ := addressing.AddressTermSources{Room: terms}.Resolve()
	groupLabels, _ := addressing.AddressTermSources{}.ResolveGroupLabels()
	for i, base := range item.PromptNicknames {
		if base == "" || base == "这位朋友" {
			continue
		}
		short := ""
		if i < len(item.PromptShortNames) {
			short = item.PromptShortNames[i]
		}
		people = append(people, base)
		forms = append(forms, addressing.FormsFor(base, short, resolvedTerms))
	}
	mode := "multi"
	switch len(forms) {
	case 0:
		mode = "group"
	case 1:
		mode = "single"
	}
	kind := strings.ToLower(strings.TrimSpace(item.MissionKind))
	scene := "answer"
	switch {
	case strings.HasPrefix(kind, "welcome"):
		scene = "greeting"
	case kind == "reply_like" || kind == "reply_follow" || kind == "conversion_signal":
		if len(forms) > 1 {
			scene = "thanks"
		}
	}
	in := addressing.PhraseInput{Mode: mode, Scene: scene, People: forms, Terms: resolvedTerms, GroupLabels: groupLabels}
	system, user := addressing.PhrasePrompt(in, nil)
	response, err := w.completePaidModel(ctx, s, agentgateway.Request{
		Stage:          "interaction_addressing_plan",
		Messages:       []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		EnableThinking: false,
		MaxTokens:      120,
		Timeout:        10 * time.Second,
	}, item.ID+":phrase")
	if err == nil {
		if phrase, parseErr := addressing.ParsePhrase(response.Text, in); parseErr == nil {
			item.PromptAddressPhrase = phrase
			return
		}
	}
	item.PromptAddressPhrase = fallbackAddressPhrase(in)
}

// fallbackAddressPhrase delegates to the shared addressing fallback.
func fallbackAddressPhrase(in addressing.PhraseInput) string {
	return addressing.FallbackPhrase(in)
}

// nicknameRules loads the global nickname rules once per worker process.
func (w *Worker) nicknameRules() *addressing.Rules {
	w.nicknameRulesOnce.Do(func() {
		w.nicknameRulesValue = addressing.LoadDefault()
	})
	if w.nicknameRulesValue == nil {
		defaults := addressing.DefaultRules()
		return &defaults
	}
	return w.nicknameRulesValue
}

func nicknameCacheKey(s model.LiveRuntimeSession, identity string, rulesVersion int, name string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(s.TenantID, ":", s.RoomID)+":"+identity+":"+strconv.Itoa(rulesVersion)+":"+name)))
}

// cacheNickname stores one validated address; the caller holds addressMu.
func (w *Worker) cacheNickname(key, base, short string, now time.Time) {
	if len(w.nicknameAliases) >= 512 {
		w.nicknameAliases = map[string]nicknameAliasCacheEntry{}
	}
	w.nicknameAliases[key] = nicknameAliasCacheEntry{Address: base, Short: short, Expires: now.Add(30 * time.Minute)}
}
