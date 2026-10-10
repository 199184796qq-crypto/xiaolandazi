package httpapi

import (
	"context"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentstrategy"
	"log"
)

func (s *Server) skipExactDuplicateCandidate(ctx context.Context, state *anchorcycle.State, candidate anchorcycle.Candidate) (*anchorcycle.State, error) {
	state.Attempts = append(state.Attempts, candidate)
	state.SkipGeneration("consecutive_exact_duplicate", "与上一段完全相同，保留上一段，继续生成下一任务")
	s.queueGenerationSkip(state, "consecutive_exact_duplicate", "与上一段完全相同，保留上一段，继续生成下一任务")
	log.Printf("mainline omitted before TTS room=%d candidate=%s reason=consecutive_exact_duplicate", state.RoomID, candidate.ID)
	return state, s.anchorCycles.Save(ctx, state)
}

func cleanCycleConsecutiveSpeech(state *anchorcycle.State, candidate *anchorcycle.Candidate) {
	original := candidate.OutputText
	text, removed := contentstrategy.RemoveConsecutiveSpeech(original, state.Config.AllowConsecutiveRepeat)
	if len(state.Accepted) > 0 {
		previous := state.Accepted[len(state.Accepted)-1].Selected
		var count int
		text, count = contentstrategy.RemoveRepeatedEntry(cycleVoiceText(previous), text, state.Config.AllowConsecutiveRepeat)
		removed += count
	}
	candidate.OutputText = text
	candidate.ExactDuplicateRemovals += removed
	if text != original {
		candidate.CacheID = ""
	}
}
