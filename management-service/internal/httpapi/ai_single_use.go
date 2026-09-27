package httpapi

import (
	"context"
	"log"
	"time"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const (
	aiSingleUsePayerCustomer = "customer_virtual_beans"
	aiSingleUsePayerCompany  = "company"
)

func aiSingleUsePayer(actor model.Actor) string {
	if actor.IsInternalStaff() || actor.IsAgentAdmin() {
		return aiSingleUsePayerCompany
	}
	return aiSingleUsePayerCustomer
}

func (s *Server) beginAISingleUse(
	ctx context.Context,
	actor model.Actor,
	roomID *int64,
	source string,
	metadata map[string]any,
) string {
	externalID, err := s.store.StartAISingleUseEvent(ctx, appdb.AISingleUseEventInput{
		ActorUserID: actor.UserID,
		TenantID:    actor.TenantID,
		RoomID:      roomID,
		Source:      source,
		PayerType:   aiSingleUsePayer(actor),
		QuotedBeans: 1,
		Metadata:    metadata,
		StartedAt:   time.Now().UTC(),
	})
	if err != nil {
		log.Printf("ai single-use start source=%s actor=%d: %v", source, actor.UserID, err)
		return ""
	}
	return externalID
}

func (s *Server) finishAISingleUse(
	ctx context.Context,
	externalID string,
	status string,
	provider string,
	modelName string,
	latencyMS int64,
	metadata map[string]any,
) {
	if externalID == "" {
		return
	}
	if err := s.store.FinishAISingleUseEvent(ctx, externalID, status, provider, modelName, latencyMS, metadata, time.Now().UTC()); err != nil {
		log.Printf("ai single-use finish id=%s status=%s: %v", externalID, status, err)
	}
}
