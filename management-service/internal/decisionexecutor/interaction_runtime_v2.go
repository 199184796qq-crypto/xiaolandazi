package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/addressing"
	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechmission"
	"net/http"
	"strings"
	"time"
)

func (w *Worker) isInteractionV2(item *decisionItem, session model.LiveRuntimeSession) bool {
	if item == nil || strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
		return false
	}
	if _, ok := interactionplan.Decode(item.PurposeKnowledge); ok {
		return true
	}
	// Real stores expose the already-deployed plan-prompt asset. Pure legacy test
	// doubles do not accidentally enable a production runtime without this source.
	_, ok := w.store.(interface {
		GetLiveAgentPlanPrompt(context.Context, int64, int64) (string, time.Time, error)
	})
	return ok && session.ID > 0
}

type v2TaskFailure struct{ error }

func (e v2TaskFailure) Unwrap() error { return e.error }

func (w *Worker) withdrawUnstartedV2(session model.LiveRuntimeSession, id string) {
	if w.core == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	path := fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions", session.RoomID)
	resp, err := w.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodGet, path, tenantQuery(session.TenantID), nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var state struct {
		Queue []struct {
			ID       string `json:"id"`
			Playback string `json:"playback_task_id"`
		} `json:"queue"`
	}
	if json.NewDecoder(resp.Body).Decode(&state) != nil {
		return
	}
	for _, item := range state.Queue {
		if item.ID != id {
			continue
		}
		if item.Playback != "" {
			return
		}
		deleted, err := w.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodDelete, path+"/"+id, tenantQuery(session.TenantID), nil)
		if err == nil {
			deleted.Body.Close()
		}
		return
	}
}
func (w *Worker) v2Address(ctx context.Context, s model.LiveRuntimeSession, item *decisionItem, snapshot interactionplan.Snapshot) string {
	// 没有可点名的观众时也走完整流程，用群体词收尾。
	// Rules rewrite only these associated viewers. Grey or unsafe names use a
	// neutral form; no model maintains viewer indices or chooses another person.
	rules := w.nicknameRules()
	prefs, _ := w.store.GetRoomAddressingPreferences(ctx, s.TenantID, s.RoomID)
	allowed := func(text string) bool {
		for _, b := range prefs.BlockedTerms {
			if b != "" && strings.Contains(text, b) {
				return false
			}
		}
		return true
	}
	terms, _ := (addressing.AddressTermSources{Room: prefs.PreferredTerms, Style: anchorStyleAddressTerms(snapshot.Style)}).Resolve()
	// 个体称谓去掉「们」；成对词（哥姐、兄弟姐妹）现在允许接在单人后面，
	// 只排除群体专用词（各位、大家）。
	clean := make([]string, 0, len(terms))
	for _, term := range terms {
		term = addressing.SingularTerm(term)
		if term == "" || !allowed(term) || addressing.IsGroupOnlyTerm(term) {
			continue
		}
		clean = append(clean, term)
	}
	terms = clean
	// 逐个判：能念的直接用；灰色的先留着，攒成一批只问一次模型。
	type addressSlot struct {
		name string
		raw  string
		grey bool
	}
	slots := make([]addressSlot, 0, 3)
	for _, raw := range item.Nicknames {
		if len(slots) == 3 {
			break
		}
		d := rules.Check(raw)
		switch d.Status {
		case addressing.StatusSafe, addressing.StatusRewritten:
			// 纯英文不可念的名字在这里换成首字母，和 FormsFor 的规则保持一致。
			name := addressing.FormsFor(d.Text, "", nil).Base
			if name != "" && allowed(name) {
				slots = append(slots, addressSlot{name: name, raw: raw})
			}
		case addressing.StatusGrey:
			slots = append(slots, addressSlot{grey: true, raw: raw})
		}
	}
	// 灰色昵称：只有「去掉英文数字后还剩两个汉字」的才值得问模型，
	// 纯符号/纯字母直接走群体词，不花这次调用。
	greyIdx := make([]int, 0, len(slots))
	greyNames := make([]string, 0, len(slots))
	for i, slot := range slots {
		if slot.grey && addressing.WorthAsking(slot.raw) {
			greyIdx = append(greyIdx, i)
			greyNames = append(greyNames, slot.raw)
		}
	}
	if len(greyNames) > 0 && w.agent != nil {
		if resolved := w.resolveGreyAddresses(ctx, s, item, greyNames, rules); len(resolved) == len(greyNames) {
			for i, base := range resolved {
				if base != "" && allowed(base) {
					slots[greyIdx[i]] = addressSlot{name: base, raw: greyNames[i]}
				}
			}
		}
	}
	var names []string
	for _, slot := range slots {
		if slot.name != "" {
			names = append(names, slot.name)
		}
	}
	// 一个可点名的都没有时，一律用群称呼：送过来的是这一批人，
	// 被灰色判定拒掉也还是这批，不改成「这位朋友」。
	// 群体也掷同一颗骰子：40% 不加称呼直接念正文，60% 加群体称谓。
	group := addressing.GroupLabelsFrom(terms)
	address := ""
	if len(group) > 0 && addressing.WithTitle(w.addressRand()) {
		address = group[0]
	}
	if len(names) > 0 {
		parts := make([]string, 0, len(names))
		if len(names) == 1 {
			// 单人：裸称 40%、带称谓 60%。
			name := names[0]
			label := name
			// 按最终可念称呼计数：缩写后超过 5 个字不加称谓。
			if addressing.WithTitle(w.addressRand()) && len(terms) > 0 && !addressing.HasAddressSuffix(name) && len([]rune(name)) <= 5 {
				label = name + terms[0]
			}
			parts = append(parts, label)
		} else {
			// 多人：三种形态各三分之一——群体称呼 / 裸称名单 / 每个名字带称谓。
			switch w.addressRand().Intn(3) {
			case 0:
				if len(group) > 0 {
					address = group[0]
				}
			case 1:
				parts = append(parts, names...)
			default:
				for _, name := range names {
					label := name
					// 多人逐名追加也按最终可念称呼的 5 字上限。
					if len(terms) > 0 && !addressing.HasAddressSuffix(name) && len([]rune(name)) <= 5 {
						label = name + terms[0]
					}
					parts = append(parts, label)
				}
			}
		}
		if len(parts) > 0 {
			address = strings.Join(parts, "、")
		}
	}
	if !allowed(address) {
		address = ""
	}
	item.PromptNicknames = append([]string(nil), names...)
	item.PromptAddressPhrase = address
	item.resolvedAddress = address
	plan := speechmission.AddressingPlan{Mode: "SINGLE", Candidate: address, SelectedNames: names, Optional: false}
	if len(item.Nicknames) > 1 {
		plan.Mode = "MIXED"
	}
	if len(names) == 0 {
		plan.Mode = "GROUP"
		plan.GroupLabel = address
	}
	item.resolvedAddressingPlan = plan
	w.updateMission(item, func(m *speechmission.Mission) { m.Addressing = plan })
	return address
}
func (w *Worker) generateInteractionV2(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem) (string, error) {
	e, prepared := interactionplan.Decode(item.PurposeKnowledge)
	freeze := interactionplan.Freeze
	if prepared {
		freeze = interactionplan.FreezePrepared
	}
	snapshot, err := freeze(ctx, w.store, session)
	if err != nil {
		return "", err
	}
	complete := func(callCtx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		return w.completeV2RecordedModel(callCtx, session, snapshot, req, item.ID+":"+req.Stage)
	}
	if prepared {
		if e.Tenant != session.TenantID || e.Room != session.RoomID || e.Session != session.ID || e.Plan != snapshot.Scope.Plan || e.Fingerprint != snapshot.Fingerprint {
			return "", fmt.Errorf("互动正文与当前场次或方案不匹配")
		}
		if !e.ExpiresAt.IsZero() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, e.ExpiresAt)
			defer cancel()
		}
	} else {
		// Explicit/manual requests also use this content protocol. Automatic events
		// already have body_ready and never call the intent/body models a second time.
		input := interactionplan.Input{Kind: item.MissionKind, Text: primaryQuestion(*item), AllowClarify: true, EventCount: item.MissionEventCount, CurrentMainline: item.CurrentMainline}
		if isTimeReport(*item) {
			// The timestamp belongs to this model request, not to the task's
			// creation or a later TTS retry. Ordinary interactions omit it.
			at := item.TimeReportGeneratedAt.In(beijingZone)
			input.ReportClock = at.Format("2006-01-02 15:04")
			input.ReportPeriod = timeOfDay(at)
		}
		intent, body, err := interactionplan.Generate(interactionplan.WithParseRequest(ctx, item.ID), snapshot, input, complete)
		if err != nil {
			return "", err
		}
		e = interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: item.ID, Epoch: "manual", Tenant: session.TenantID, Room: session.RoomID, Session: session.ID, Plan: snapshot.Scope.Plan, Fingerprint: snapshot.Fingerprint, Intent: intent, Body: body, ExpiresAt: item.ExpiresAt}
		item.PurposeKnowledge, _ = json.Marshal(e)
	}
	if !e.Intent.Relevant || strings.TrimSpace(e.Body) == "" {
		return "", fmt.Errorf("本条互动无需口播")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	item.EmotionDisabled = true
	if enabled, ok := snapshot.Voice.Config["emotion_enabled"].(bool); ok {
		item.EmotionDisabled = !enabled
	}
	item.SelectedResume = "DIRECT"
	item.SelectedInterrupt = "hard_cut"
	item.BridgeText = ""
	item.AppliedStrategyStages = []string{"plan_prompt_body_ready", "late_address", "address_join"}
	// Preserve the existing order decision/dedupe. The real bell is appended in
	// the audio layer after the final TTS, never spoken as an extra word.
	w.markOrderBell(item, e.Intent.Type)
	address := w.v2Address(ctx, session, item, snapshot)
	text, err := interactionplan.Join(ctx, snapshot, address, e.Body, complete)
	if err != nil {
		return "", err
	}
	w.transitionMission(item, speechmission.StateGeneratingText, "interaction_v2_body_joined", "已选中提前正文，完成后置称呼与连接，进入统一整段TTS")
	w.updateMission(item, func(m *speechmission.Mission) {
		m.GeneratedText = text
		m.Resume.Strategy = "DIRECT"
		m.Resume.BridgeText = ""
	})
	return text, nil
}

// Persist the actual joining request and outcome as well as the early content
// calls. The plan identity is the frozen task contract, not a later selection.
func (w *Worker) completeV2RecordedModel(ctx context.Context, session model.LiveRuntimeSession, snapshot interactionplan.Snapshot, req agentgateway.Request, key string) (agentgateway.Response, error) {
	source, ok := w.store.(interactionExchangeStore)
	if !ok {
		return w.completePaidModel(ctx, session, req, key)
	}
	id, err := source.BeginModelExchange(ctx, appdb.ModelExchangeScope{TenantID: session.TenantID, RoomID: session.RoomID, PlanID: snapshot.Scope.Plan, RunID: session.ExternalID}, req.Stage, "", req)
	if err != nil {
		return agentgateway.Response{}, err
	}
	started := time.Now()
	response, callErr := w.completePaidModel(ctx, session, req, key)
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	input, output, cached := response.InputTokens, response.OutputTokens, response.CachedInputTokens
	if err := source.CompleteModelExchange(persist, id, response, response.Text, time.Since(started).Milliseconds(), &input, &output, &cached, callErr); err != nil {
		return agentgateway.Response{}, err
	}
	return response, callErr
}
func (w *Worker) v2Voice(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (model.VoiceProfile, bool, error) {
	e, ok := interactionplan.Decode(item.PurposeKnowledge)
	if !ok {
		return model.VoiceProfile{}, false, nil
	}
	s, ok := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	if !ok || s.Fingerprint != e.Fingerprint || e.Tenant != session.TenantID || e.Room != session.RoomID || e.Session != session.ID || e.Plan != s.Scope.Plan {
		return model.VoiceProfile{}, true, fmt.Errorf("互动正文冻结身份不匹配，停止送播")
	}
	voice := s.Voice
	// Freeze content and its plan/version, but resolve the actually audible host
	// at synthesis time. A pending switch or read failure must never fall back
	// to the opening voice. Do not read a later author's published version here.
	if source, ok := w.store.(runtimeVoiceSource); ok {
		identity, err := source.RuntimeVoiceIdentity(ctx, session.TenantID, session.RoomID, s.Scope.Version)
		if err != nil {
			return model.VoiceProfile{}, true, err
		}
		if strings.TrimSpace(identity.VoiceID) != "" {
			if strings.TrimSpace(identity.Model) == "" {
				return model.VoiceProfile{}, true, fmt.Errorf("本场实际主播声音缺少模型身份")
			}
			voice = runtimeVoiceProfile(identity)
		}
	}
	// Speed is a live control; copy its map rather than mutate the frozen body.
	if rates, ok := w.store.(planVoiceRateSource); ok {
		if settings, err := rates.GetPlanVoiceRate(ctx, session.TenantID, s.Scope.Plan); err == nil && settings.Rate >= 0.5 && settings.Rate <= 2 {
			config := make(map[string]any, len(voice.Config))
			for k, v := range voice.Config {
				config[k] = v
			}
			voice.Config = config
			voice.Config["rate"] = settings.Rate
		}
	}
	return voice, true, nil
}

func hasInteractionV2Body(item decisionItem) bool {
	_, ok := interactionplan.Decode(item.PurposeKnowledge)
	return ok
}

// SimulatePreparedInteractionV2 uses exactly the selected-body/address/join
// implementation, with no TTS, live queue or live billing scope.
func (w *Worker) SimulatePreparedInteractionV2(ctx context.Context, tenant, room int64, raw json.RawMessage, nickname string) (SimulationOutput, error) {
	e, ok := interactionplan.Decode(raw)
	if !ok {
		return SimulationOutput{}, fmt.Errorf("测试正文协议无效")
	}
	item := decisionItem{ID: e.Task, PurposeKnowledge: raw, MissionKind: "reply_question", BusinessCoreOnly: true, ExpiresAt: e.ExpiresAt}
	if strings.TrimSpace(nickname) != "" {
		item.Nicknames = []string{nickname}
	}
	text, err := w.generateInteractionV2(context.WithValue(ctx, workerPaidOptOutKey{}, true), model.LiveRuntimeSession{TenantID: tenant, RoomID: room}, &item)
	return SimulationOutput{Reply: text, ExecutionMode: interactionplan.Protocol}, err
}

func (w *Worker) interactionV2Memory(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	if retryableTimeReport(item) {
		return w.interactionMemoryFingerprint(ctx, session)
	}
	if e, ok := interactionplan.Decode(item.PurposeKnowledge); ok {
		return e.Fingerprint, nil
	}
	return w.interactionMemoryFingerprint(ctx, session)
}
