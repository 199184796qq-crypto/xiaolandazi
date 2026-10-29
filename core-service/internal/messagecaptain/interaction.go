package messagecaptain

import (
	"encoding/json"
	"livecompanion/core/internal/model"
)

const (
	CutRequested        = "interaction.cut.requested"
	CutOffered          = "mainline.cut.offered"
	InteractionPrepared = "interaction.audio.prepared"
	MainlineStarted     = "mainline.audio.started"
	PurposeResolved     = "interaction.purpose.resolved"
	ReturnBridgeReady   = "interaction.return.bridge.ready"
	ReturnBridgeStarted = "interaction.return.bridge.started"
	ReturnBridgeStaged  = "interaction.return.bridge.staged"
)

// The request contains scheduling intent, never the viewers to be greeted.
type CutRequest struct {
	RequestID string `json:"request_id"`
	Epoch     string `json:"epoch"`
	LeadMS    int    `json:"lead_ms"`
}
type CutOffer struct {
	PreparationWindow bool   `json:"preparation_window,omitempty"`
	RequestID         string `json:"request_id"`
	Epoch             string `json:"epoch"`
	CutMS             int    `json:"cut_ms"`
	LeadMS            int    `json:"lead_ms"`
}
type Preparation struct {
	DecisionID string `json:"decision_id"`
	ElapsedMS  int    `json:"elapsed_ms"`
}

type PurposeResult struct {
	Knowledge          json.RawMessage `json:"knowledge,omitempty"`
	Event              model.RoomEvent `json:"event"`
	Epoch              string          `json:"epoch"`
	Question           bool            `json:"question"`
	Relevant           bool            `json:"relevant"`
	Topic              string          `json:"topic"`
	PurposeID          string          `json:"purpose_id,omitempty"`
	Source             string          `json:"source"`
	Ambiguous          bool            `json:"ambiguous,omitempty"`
	Clarify            string          `json:"clarify,omitempty"`
	Options            []string        `json:"options,omitempty"`
	ClarificationReply bool            `json:"clarification_reply,omitempty"`
}
