package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"log"
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
	Index   int    `json:"index"`
	RawName string `json:"raw_name"`
	Chars   int    `json:"char_count"`
	Digits  int    `json:"max_continuous_digits"`
}

func (item decisionItem) promptNicknames() []string {
	if item.PromptNicknames != nil {
		return item.PromptNicknames
	}
	return item.Nicknames
}
func nicknameInput(index int, name string) nicknamePlanInput {
	run, longest := 0, 0
	for _, r := range name {
		if unicode.IsDigit(r) {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return nicknamePlanInput{Index: index, RawName: name, Chars: utf8.RuneCountInString(name), Digits: longest}
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
	packet, _ := json.Marshal(map[string]any{"names": pending, "preferred_terms": prefs.PreferredTerms, "blocked_terms": prefs.BlockedTerms})
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	response, e := w.agent.Complete(callCtx, agentgateway.Request{Stage: "interaction_addressing_plan", Messages: []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: string(packet)}}, ResponseFormat: agentgateway.ResponseJSON, EnableThinking: false, MaxTokens: 1200, Timeout: 8 * time.Second})
	var aliases []string
	if e == nil {
		aliases, e = decodeNicknamePlan(response.Text, len(pending))
	}
	if e != nil {
		log.Printf("nickname plan fallback tenant=%d room=%d decision=%s reason=planning_unavailable", s.TenantID, s.RoomID, item.ID)
		return
	}
	w.addressMu.Lock()
	defer w.addressMu.Unlock()
	if len(w.nicknameAliases)+len(aliases) > 512 {
		w.nicknameAliases = map[string]nicknameAliasCacheEntry{}
	}
	for i, alias := range aliases {
		pos := positions[i]
		item.PromptNicknames[pos] = alias
		w.nicknameAliases[keys[pos]] = nicknameAliasCacheEntry{Address: alias, Expires: now.Add(30 * time.Minute)}
	}
}
