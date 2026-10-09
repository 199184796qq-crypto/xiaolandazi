package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/core/internal/messagecaptain"
)

func (s *Server) MessageCaptain() *messagecaptain.Captain { return s.messages }

func (s *Server) handleCutRequest(ctx context.Context, m messagecaptain.Message) error {
	if m.Kind != messagecaptain.CutRequested {
		return nil
	}
	var request messagecaptain.CutRequest
	if err := json.Unmarshal(m.Payload, &request); err != nil {
		return err
	}
	if request.RequestID == "" || request.Epoch == "" || request.LeadMS < 5000 || request.LeadMS > 30000 {
		return nil
	}
	state := s.audioDevState()
	if state == nil || state.client == nil {
		return nil
	}
	program, err := state.client.ProgramSnapshot(ctx, m.RoomID)
	if err != nil || !program.Running || program.Suspended || program.WaitingForContent || program.Task == nil {
		return nil
	}
	// Reuse the established safe points and grading; announcing a point does
	// not arm a cut or pause the mainline while audio is still being prepared.
	plan, _, ok := preparationContext(program, program.CurrentMS)
	if !ok && !program.Sequential {
		if point, exists := chooseRoomProgramSafePointWindow(program, program.CurrentMS, request.LeadMS-3000, 35000, request.LeadMS); exists {
			plan = interactionCutPlan{CutMS: point.CutMS, LeadMS: point.CutMS - program.CurrentMS}
			ok = true
		}
	}
	if !ok {
		return nil
	}
	raw, _ := json.Marshal(messagecaptain.CutOffer{RequestID: request.RequestID, Epoch: request.Epoch, CutMS: plan.CutMS, LeadMS: plan.LeadMS, PreparationWindow: true})
	s.messages.Publish(ctx, messagecaptain.Message{Kind: messagecaptain.CutOffered, RoomID: m.RoomID, RunID: program.ProgramID, TaskID: program.Task.ID, OccurredAt: m.OccurredAt, Payload: raw})
	return nil
}
