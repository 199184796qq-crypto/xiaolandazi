package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
)

func welcomeV2(item decisionItem) (bool, bool) {
	if _, ok := interactionplan.Decode(item.PurposeKnowledge); !ok {
		return false, false
	}
	var m struct {
		Welcome bool      `json:"welcome_quota"`
		Ready   time.Time `json:"welcome_ready_at"`
	}
	if json.Unmarshal(item.PurposeKnowledge, &m) != nil {
		return false, false
	}
	return m.Welcome, !m.Ready.IsZero()
}

func welcomeExecutionDeadline(item decisionItem, deadline time.Time) time.Time {
	if welcome, ready := welcomeV2(item); welcome && !ready {
		return deadline.Add(30 * time.Second)
	}
	return deadline
}

func (w *Worker) admitWelcomeV2(ctx context.Context, session model.LiveRuntimeSession, item *decisionItem) error {
	welcome, _ := welcomeV2(*item)
	if !welcome {
		return nil
	}
	resp, err := w.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodPost, fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/welcome-ready", session.RoomID, item.ID), tenantQuery(session.TenantID), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("欢迎成品入队失败 HTTP %d", resp.StatusCode)
	}
	var ack struct {
		Expires   time.Time       `json:"expires_at"`
		Knowledge json.RawMessage `json:"knowledge"`
	}
	if json.NewDecoder(resp.Body).Decode(&ack) != nil {
		return fmt.Errorf("欢迎入队回执无效")
	}
	old, ok := interactionplan.Decode(item.PurposeKnowledge)
	next, valid := interactionplan.Decode(ack.Knowledge)
	var marker struct {
		Welcome bool      `json:"welcome_quota"`
		Ready   time.Time `json:"welcome_ready_at"`
	}
	json.Unmarshal(ack.Knowledge, &marker)
	if !ok || !valid || !marker.Welcome || marker.Ready.IsZero() || !ack.Expires.Equal(marker.Ready.Add(30*time.Second)) || !w.now().Before(ack.Expires) || !next.ExpiresAt.Equal(ack.Expires) || old.Task != next.Task || old.Epoch != next.Epoch || old.Revision != next.Revision || old.Tenant != next.Tenant || old.Room != next.Room || old.Session != next.Session || old.Plan != next.Plan || old.Fingerprint != next.Fingerprint || old.Body != next.Body {
		return fmt.Errorf("欢迎入队身份或期限变化")
	}
	item.ExpiresAt = ack.Expires
	item.PurposeKnowledge = ack.Knowledge
	return nil
}
