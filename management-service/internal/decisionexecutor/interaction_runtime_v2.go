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
	"math/rand"
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
	if len(item.Nicknames) == 0 {
		return ""
	}
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
	clean := terms[:0]
	for _, term := range terms {
		if allowed(term) && !addressing.IsPluralTerm(term) && !strings.Contains(term, "哥姐") && !strings.Contains(term, "兄弟姐妹") {
			clean = append(clean, term)
		}
	}
	terms = clean
	var names []string
	for _, name := range item.Nicknames {
		d := rules.Check(name)
		if d.Status == addressing.StatusSafe || d.Status == addressing.StatusRewritten {
			if d.Text != "" && allowed(d.Text) {
				names = append(names, d.Text)
			}
		}
		if len(names) == 3 {
			break
		}
	}
	address := "这位朋友"
	if len(item.Nicknames) > 1 {
		address = "几位朋友"
	}
	if len(names) > 0 {
		// Program-owned forms keep the selected viewers and frozen plan's address
		// style. The joining model cannot choose another person.
		forms := make([]addressing.FormSet, 0, len(names))
		for _, name := range names {
			forms = append(forms, addressing.FormsFor(name, "", terms))
		}
		mode := "single"
		if len(forms) > 1 {
			mode = "multi"
		}
		address = addressing.BuildPhrase(addressing.PhraseInput{Mode: mode, Scene: "answer", People: forms, Terms: terms, GroupLabels: addressing.GroupLabelsFrom(terms)}, rand.New(rand.NewSource(int64(len(item.ID)))))
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
	snapshot, err := interactionplan.Freeze(ctx, w.store, session)
	if err != nil {
		return "", err
	}
	e, prepared := interactionplan.Decode(item.PurposeKnowledge)
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
		intent, body, err := interactionplan.Generate(ctx, snapshot, interactionplan.Input{Kind: item.MissionKind, Text: primaryQuestion(*item), AllowClarify: true, EventCount: item.MissionEventCount, CurrentMainline: item.CurrentMainline}, complete)
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
func (w *Worker) v2Voice(session model.LiveRuntimeSession, item decisionItem) (model.VoiceProfile, bool) {
	e, ok := interactionplan.Decode(item.PurposeKnowledge)
	if !ok {
		return model.VoiceProfile{}, false
	}
	s, ok := interactionplan.Frozen.ForSession(session.TenantID, session.RoomID, session.ID)
	if !ok || s.Fingerprint != e.Fingerprint {
		return model.VoiceProfile{}, false
	}
	return s.Voice, true
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
	if e, ok := interactionplan.Decode(item.PurposeKnowledge); ok {
		return e.Fingerprint, nil
	}
	return w.interactionMemoryFingerprint(ctx, session)
}
