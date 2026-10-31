package decisionexecutor

import (
	"context"
	"fmt"
	"strings"

	"livecompanion/management/internal/addressing"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechmission"
)

func (w *Worker) generateBehaviorTemplate(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem, e interactionplan.Envelope) (string, error) {
	s, err := interactionplan.Freeze(ctx, w.store, session)
	if err != nil {
		return "", err
	}
	if e.Tenant != session.TenantID || e.Room != session.RoomID || e.Session != session.ID || e.Plan != s.Scope.Plan || e.Fingerprint != s.Fingerprint || e.ExpiresAt.IsZero() || !w.now().Before(e.ExpiresAt) {
		return "", fmt.Errorf("行为模板场次、方案或期限已变化")
	}
	kind := map[string]string{"reply_like": "like", "reply_follow": "follow", "conversion_signal": "order"}[item.MissionKind]
	body, err := interactionplan.BehaviorBody(kind, e.Template, e.EventCount)
	if err != nil || body != e.Body {
		return "", fmt.Errorf("行为模板内容或人数变化")
	}
	prefs, err := w.store.GetRoomAddressingPreferences(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", err
	}
	allowed := func(v string) bool {
		for _, b := range prefs.BlockedTerms {
			if b != "" && strings.Contains(v, b) {
				return false
			}
		}
		return true
	}
	terms, _ := (addressing.AddressTermSources{Room: prefs.PreferredTerms, Style: anchorStyleAddressTerms(s.Style)}).Resolve()
	group := "朋友们"
	for _, v := range addressing.GroupLabelsFrom(terms) {
		if allowed(v) {
			group = v
			break
		}
	}
	var names []string
	if e.Template != "order_count_v1" {
		rules := w.nicknameRules()
		for _, raw := range item.Nicknames {
			d := rules.Check(raw)
			if d.Status != addressing.StatusSafe && d.Status != addressing.StatusRewritten {
				continue
			}
			name := addressing.FormsFor(d.Text, "", nil).Base
			if name != "" && allowed(name) {
				names = append(names, name)
			}
			if len(names) == 3 {
				break
			}
		}
	}
	address := group
	if len(names) > 0 {
		address = strings.Join(names, "、")
	}
	if !allowed(address) {
		return "", fmt.Errorf("行为称呼被规则拦截")
	}
	text := ""
	switch e.Template {
	case "like_v1":
		text = fmt.Sprintf("谢谢%s的点赞支持。", address)
	case "follow_v1":
		text = fmt.Sprintf("谢谢%s的关注，欢迎常来。", address)
	case "order_single_v1":
		text = fmt.Sprintf("感谢%s下单支持。", address)
	case "order_count_v1":
		text = fmt.Sprintf("感谢%s支持，刚刚又有%d位朋友说已经下单。", group, e.EventCount)
	}
	if err = interactionplan.Validate(s, text); err != nil {
		return "", err
	}
	item.EmotionDisabled = true
	item.SelectedResume = "DIRECT"
	item.SelectedInterrupt = "hard_cut"
	item.BridgeText = ""
	item.PromptNicknames = names
	item.PromptAddressPhrase = address
	item.resolvedAddress = address
	item.AppliedStrategyStages = []string{"program_behavior_template", "program_address", "whole_utterance_tts"}
	mode := "GROUP"
	if len(names) == 1 {
		mode = "SINGLE"
	} else if len(names) > 1 {
		mode = "MIXED"
	}
	w.updateMission(item, func(m *speechmission.Mission) {
		m.GeneratedText = text
		m.Addressing = speechmission.AddressingPlan{Mode: mode, Candidate: address, SelectedNames: names, Optional: false}
		m.Resume.Strategy = "DIRECT"
	})
	return text, nil
}
