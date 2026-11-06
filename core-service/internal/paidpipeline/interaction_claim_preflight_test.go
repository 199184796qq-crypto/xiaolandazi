package paidpipeline

import (
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/messagecaptain"
	"testing"
	"time"
)

func TestPreflightClaimStageAndPaidStageOwnership(t *testing.T) {
	for _, tc := range []struct{ name, stage, status, epoch, bound, want string }{
		{"pending claim", "claim", "pending", "current", "", ""},
		{"pending model", "model", "pending", "current", "", "decision_changed"},
		{"pending tts", "tts", "pending", "current", "", "decision_changed"},
		{"claimed claim", "claim", "claimed", "current", "", "decision_changed"},
		{"claimed model", "model", "claimed", "current", "", ""},
		{"claimed tts", "tts", "claimed", "current", "", ""},
		{"claim missing epoch", "claim", "pending", "", "", "run_changed"},
		{"claim old epoch", "claim", "pending", "old", "", "run_changed"},
		{"tts old epoch", "tts", "claimed", "old", "", "run_changed"},
		{"bound tts", "tts", "claimed", "current", "voice", "decision_changed"},
		{"removed claim", "claim", "removed", "current", "", "decision_changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			p, q, _ := reportProcessor(t, &now, 24)
			i := q.Enqueue(29, agentdecision.Candidate{Topic: "claim-contract", Question: "欢迎", MissionKind: "welcome_named", TTLSeconds: 30}).Item
			if tc.status == "claimed" {
				q.ClaimNext(29)
			}
			if tc.bound != "" {
				q.BindPlayback(29, i.ID, tc.bound)
			}
			if tc.status == "removed" {
				q.Remove(29, i.ID)
			}
			epoch := tc.epoch
			if epoch == "current" {
				epoch = p.cutEpoch(29)
			}
			raw, _ := json.Marshal(map[string]any{"room_id": 29, "tenant_id": 14, "plan_id": 4, "epoch": epoch, "decision_id": i.ID, "duration_ms": 3000, "stage": tc.stage})
			err := p.preflightAudio(messagecaptain.Message{RoomID: 29, RunID: epoch, Payload: raw})
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
