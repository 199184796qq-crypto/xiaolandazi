package httpapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

// Changes to expression instructions, safety policy, facts or voice identity
// create a new namespace. Old manifests are never relabelled as current facts.
// Context is hashed in full: conservative misses are preferable to stale facts.
func recordingCurrentCacheIdentity(version model.LiveAgentPlanVersion, texts []string, policy *anchorcycle.State) (string, string) {
	voice := version.VoiceIdentity
	if voice.Rate == 0 {
		voice.Rate = 1
	}
	voiceJSON, _ := json.Marshal(voice)
	voiceKey := recordingcache.VoiceKey("recording-voice-v3", string(voiceJSON), "instruction-v1")
	policyMD := ""
	if policy != nil {
		policyMD = policy.P0PolicyMD
	}
	contextCopy := make(map[string]any, len(version.GenerationContext))
	for key, value := range version.GenerationContext {
		contextCopy[key] = value
	}
	if script, ok := recordingScriptFromContext(version.GenerationContext); ok {
		type tabContent struct {
			ID   string
			Text string
		}
		tabs := make([]tabContent, 0, len(script.Tabs))
		for _, tab := range script.Tabs {
			tabs = append(tabs, tabContent{tab.TabID, tab.Text})
		}
		sort.Slice(tabs, func(i, j int) bool { return tabs[i].ID < tabs[j].ID })
		// Title, pinned position and visual ordering cannot change facts/audio.
		contextCopy["recording_script"] = tabs
	}
	contextJSON, _ := json.Marshal(contextCopy)
	orderedTexts := append([]string(nil), texts...)
	sort.Strings(orderedTexts)
	contentKey := recordingcache.VoiceKey("recording-content-v3", recordingcache.ScriptHash(orderedTexts), string(contextJSON), policyMD)
	return voiceKey, contentKey
}

func recordingStableTabIDs(version model.LiveAgentPlanVersion, texts []string) []string {
	ids := make([]string, 0, len(texts))
	if script, ok := recordingScriptFromContext(version.GenerationContext); ok {
		for _, tab := range script.Tabs {
			if strings.TrimSpace(tab.Text) != "" {
				ids = append(ids, tab.TabID)
			}
		}
	}
	if len(ids) != len(texts) {
		ids = make([]string, len(texts))
		for i := range texts {
			// Legacy single-box plans have no tab ID. Their slot is stable in
			// this tenant/room/plan scope; body edits are source revisions.
			ids[i] = recordingcache.VoiceKey("legacy-tab-slot-v1", fmt.Sprint(i))
		}
	}
	return ids
}

// Inspect the actual report and its binding, not just CheckP0Candidate's error.
func approvedRecordingSpoken(state *anchorcycle.State, text string) (string, error) {
	if state == nil {
		return "", fmt.Errorf("话术审核状态不可用")
	}
	candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: strings.TrimSpace(text), OutputText: strings.TrimSpace(text)}
	if err := state.CheckP0Candidate(&candidate); err != nil {
		return "", err
	}
	if candidate.P0Report == nil || !candidate.P0Report.Passed {
		return "", errRecordingOriginalRejected
	}
	if err := state.CheckApprovedText(candidate, candidate.OutputText); err != nil {
		return "", err
	}
	return candidate.OutputText, nil
}

func recordingUnitTexts(units []recordingReadyUnit) []string {
	texts := make([]string, len(units))
	for i, unit := range units {
		texts[i] = unit.Text
	}
	return texts
}
