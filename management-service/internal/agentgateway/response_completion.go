package agentgateway

import "strings"

// CompletedNormally requires positive supplier evidence. An absent reason is
// unknown, not proof that a response reached its natural end.
func (r Response) CompletedNormally() bool {
	switch strings.ToLower(strings.TrimSpace(r.FinishReason)) {
	case "stop", "end_turn", "stop_sequence", "completed":
		return true
	}
	return false
}
