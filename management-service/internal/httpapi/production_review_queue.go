package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/model"
	"log"
	"time"
)

var productionReviewSlots = make(chan struct{}, 16)

// Recording is off the critical audio path. It never grants approval or stops playback.
func (s *Server) queueReviewWrite(md, source string, event model.P0AuditEvent) {
	if s == nil || s.store == nil {
		return
	}
	select {
	case productionReviewSlots <- struct{}{}:
	default:
		log.Printf("review recording deferred tenant=%d room=%d candidate=%s", event.TenantID, event.RoomID, event.CandidateID)
		return
	}
	go func() {
		defer func() { <-productionReviewSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		p, err := contentguard.Compile(md)
		if err == nil {
			err = s.store.SaveP0PolicyVersion(ctx, p.SHA256, p.Data.PolicyVersion, source, md)
		}
		if err == nil {
			err = s.store.SaveP0AuditEvent(ctx, event)
		}
		if err != nil {
			log.Printf("review recording unavailable tenant=%d room=%d candidate=%s", event.TenantID, event.RoomID, event.CandidateID)
		}
	}()
}
func (s *Server) queueP0Review(state *anchorcycle.State, c anchorcycle.Candidate) {
	if c.P0Report == nil {
		return
	}
	s.queueReviewWrite(state.P0PolicyMD, state.P0PolicySource, p0AuditEvent(state, c))
}
func (s *Server) queueGenerationSkip(state *anchorcycle.State, stage, reason string, ids ...string) {
	if s == nil || s.store == nil || state.P0PolicyMD == "" {
		return
	}
	p, err := contentguard.Compile(state.P0PolicyMD)
	if err != nil {
		return
	}
	task := state.Task()
	text := ""
	raw := ""
	id := "skip-" + anchorcycle.ID()
	if len(state.GenerationSkips) > 0 {
		item := state.GenerationSkips[len(state.GenerationSkips)-1]
		if item.Stage == stage {
			task = item.Task
			if len(item.Attempts) > 0 {
				c := item.Attempts[len(item.Attempts)-1]
				text, raw = c.Text, c.RawText
			}
		}
	}
	if len(ids) > 0 {
		id = "skip-" + ids[0]
		for _, segment := range state.Accepted {
			if segment.Selected.ID == ids[0] {
				task = segment.Task
				text = segment.Selected.Text
				raw = segment.Selected.RawText
				break
			}
		}
	}
	sessionID := int64(0)
	if state.Delivery != nil {
		sessionID = state.Delivery.RuntimeSessionID
	}
	report, _ := json.Marshal(map[string]any{"kind": "generation_skip", "stage": stage, "reason": reason, "review_passed": nil, "runtime_session_id": sessionID})
	event := model.P0AuditEvent{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID, CandidateID: id, RuleSHA256: p.SHA256, Phase: state.Phase, TaskKey: task.Key, Decision: "skipped", Text: text, RawResponse: raw, Report: report}
	s.queueReviewWrite(state.P0PolicyMD, state.P0PolicySource, event)
}
