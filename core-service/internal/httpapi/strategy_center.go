package httpapi

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/core/internal/strategycenter"
)

func (s *Server) strategyPolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("tenantID")), 10, 64)
	if err != nil || tenantID < 0 {
		writeError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	if s.strategyPolicies == nil {
		s.strategyPolicies = strategycenter.New()
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.strategyPolicies.Resolve(tenantID))
		return
	}
	var policy strategycenter.Policy
	if err := readJSON(w, r, &policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid strategy policy")
		return
	}
	policy = s.strategyPolicies.Put(tenantID, policy)
	log.Printf("strategy policy updated tenant=%d rules=%d addressing=%d mode=%s", tenantID, len(policy.Rules), len(policy.Addressing), policy.AddressingMode)
	writeJSON(w, http.StatusOK, policy)
}

type roomStrategySelectInput struct {
	Category    string                 `json:"category"`
	Candidates  []string               `json:"candidates,omitempty"`
	Seed        string                 `json:"seed,omitempty"`
	Topic       string                 `json:"topic,omitempty"`
	EstimatedMS int                    `json:"estimated_ms,omitempty"`
	Signals     strategycenter.Signals `json:"signals,omitempty"`
}

func (s *Server) selectRoomStrategy(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	var input roomStrategySelectInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid strategy select body")
		return
	}
	if s.strategyPolicies == nil {
		s.strategyPolicies = strategycenter.New()
	}
	if s.brain != nil {
		if view, err := s.brain.Snapshot(roomID); err == nil {
			if input.Signals.Entries30s == 0 {
				input.Signals.Entries30s = view.Intelligence.Entries30s
			}
			if input.Signals.Likes30s == 0 {
				input.Signals.Likes30s = view.Intelligence.Likes30s
			}
			if input.Signals.Follows30s == 0 {
				input.Signals.Follows30s = view.Intelligence.Follows30s
			}
			if strings.TrimSpace(input.Signals.Heat) == "" {
				input.Signals.Heat = view.Intelligence.Heat
			}
		}
	}
	if strings.EqualFold(strings.TrimSpace(input.Category), "resume") && len(input.Candidates) == 0 {
		input.Candidates = []string{"DIRECT", "BRIDGE"}
		if state := s.audioDevState(); state != nil && state.client != nil && state.client.Enabled() {
			if program, programErr := state.client.ProgramSnapshot(r.Context(), roomID); programErr == nil && program.Running && program.Task != nil {
				currentMS := program.CurrentMS
				if currentMS <= 0 {
					currentMS = program.Task.StartMS
				}
				cutMS := program.NextSafeCutMS
				if cutMS <= currentMS {
					if resolved, ok := resolveRoomProgramSafeCutAfterGeneration(program, 0, currentMS); ok {
						cutMS = resolved
					}
				}
				estimatedMS := input.EstimatedMS
				if estimatedMS <= 0 {
					estimatedMS = 10000
				}
				input.Candidates = resumeCandidatesForInteraction(program, cutMS, estimatedMS, input.Topic)
			}
		}
	}
	var selected strategycenter.Selection
	if strings.EqualFold(strings.TrimSpace(input.Category), "addressing") {
		selected = s.strategyPolicies.PickAddressing(tenantID, input.Seed)
	} else {
		selected = s.strategyPolicies.Pick(tenantID, input.Category, input.Candidates, input.Signals, input.Seed)
	}
	log.Printf("strategy select tenant=%d room=%d category=%s candidates=%v weights=%v selected=%s roll=%d/%d heat=%s entries30s=%d likes30s=%d", tenantID, roomID, selected.Category, input.Candidates, selected.Candidates, selected.Key, selected.Roll, selected.Total, input.Signals.Heat, input.Signals.Entries30s, input.Signals.Likes30s)
	writeJSON(w, http.StatusOK, selected)
}
