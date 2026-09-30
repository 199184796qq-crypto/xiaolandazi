package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/speechruntime"
	"livecompanion/core/internal/strategycenter"
)

type roomAudioInteractionInput struct {
	DecisionID           string `json:"decision_id"`
	MissionID            string `json:"mission_id,omitempty"`
	SessionID            string `json:"session_id,omitempty"`
	Action               string `json:"action"`
	AudioURL             string `json:"audio_url"`
	Question             string `json:"question,omitempty"`
	ReplyText            string `json:"reply_text,omitempty"`
	Topic                string `json:"topic,omitempty"`
	InterruptStrategy    string `json:"interrupt_strategy,omitempty"`
	ResumeStrategy       string `json:"resume_strategy,omitempty"`
	BridgeText           string `json:"bridge_text,omitempty"`
	HumanizationStrategy string `json:"humanization_strategy,omitempty"`
	HumanizationKind     string `json:"humanization_kind,omitempty"`
	HumanizationApplied  bool   `json:"humanization_applied,omitempty"`
	SwitchAtMS           int    `json:"switch_at_ms,omitempty"`
	ForceAfterRest       bool   `json:"force_after_rest,omitempty"`
}

func estimatedInteractionDurationMS(text string) int {
	runes := utf8.RuneCountInString(strings.TrimSpace(text))
	if runes <= 0 {
		return 4000
	}
	ms := runes * 230
	if ms < 3000 {
		ms = 3000
	}
	if ms > 60000 {
		ms = 60000
	}
	return ms
}

func safePointTopicOverlap(point audioout.ProgramSafePoint, topic string) bool {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic == "" {
		return false
	}
	for _, value := range point.Topics {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && (value == topic || strings.Contains(value, topic) || strings.Contains(topic, value)) {
			return true
		}
	}
	return false
}

func resumeCandidatesForInteraction(program audioout.RoomProgramSnapshot, cutMS, estimatedMS int, topic string) []string {
	candidates := []string{"DIRECT", "BRIDGE"}
	overlaps := 0
	for _, point := range effectiveRoomProgramSafePoints(program) {
		if point.CutMS <= cutMS {
			continue
		}
		if safePointTopicOverlap(point, topic) {
			overlaps++
		}
	}
	if overlaps >= 1 {
		candidates = append(candidates, "FUSION_SKIP")
	}
	if overlaps >= 2 {
		candidates = append(candidates, "CROSS_RESUME")
	}
	if estimatedMS >= 15000 {
		candidates = append(candidates, "RE_ANCHOR")
	}
	if estimatedMS >= 25000 {
		candidates = append(candidates, "SWITCH_PLAN")
	}
	return candidates
}

func resumeOffsetForStrategy(program audioout.RoomProgramSnapshot, cutMS int, strategy, topic string) (int, string) {
	strategy = strings.ToUpper(strings.TrimSpace(strategy))
	if strategy == "DIRECT" || strategy == "BRIDGE" || strategy == "" {
		return cutMS, "same_safe_boundary"
	}
	points := effectiveRoomProgramSafePoints(program)
	if strategy == "SWITCH_PLAN" && program.Task != nil && program.Task.DurationMS > cutMS {
		return program.Task.DurationMS, "advance_to_next_mainline_track"
	}
	targetLead := 6500
	if strategy == "CROSS_RESUME" {
		targetLead = 16000
	} else if strategy == "RE_ANCHOR" {
		targetLead = 11000
	}
	best := 0
	bestDistance := int(^uint(0) >> 1)
	for _, point := range points {
		if point.CutMS <= cutMS {
			continue
		}
		if (strategy == "FUSION_SKIP" || strategy == "CROSS_RESUME") && safePointTopicOverlap(point, topic) {
			continue
		}
		distance := point.CutMS - cutMS - targetLead
		if distance < 0 {
			distance = -distance
		}
		if best == 0 || distance < bestDistance {
			best = point.CutMS
			bestDistance = distance
		}
	}
	if best > cutMS {
		return best, "next_safe_semantic_entry"
	}
	return cutMS, "fallback_same_safe_boundary"
}

type resumeDedupDecision struct {
	Triggered        bool
	OriginalStrategy string
	FinalStrategy    string
	OriginalMS       int
	FinalMS          int
	OriginalPointID  string
	FinalPointID     string
	Score            float64
	Reason           string
	ReplyTail        string
	OriginalPreview  string
	FinalPreview     string
	SkippedPoints    int
}

func normalizeResumeCompareText(value string) []rune {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	for _, filler := range []string{
		"家人们", "老乡", "宝子", "亲", "朋友", "大家", "咱们", "我们", "这个", "这款", "现在", "刚才", "继续", "直播间", "一下", "就是", "可以", "的话", "这里", "大家看", "给大家",
	} {
		value = strings.ReplaceAll(value, filler, "")
	}
	out := make([]rune, 0, len([]rune(value)))
	for _, r := range value {
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, r)
		}
	}
	return out
}

func resumeNGramSet(runes []rune, size int) map[string]struct{} {
	result := map[string]struct{}{}
	if size <= 0 || len(runes) < size {
		return result
	}
	for i := 0; i+size <= len(runes); i++ {
		result[string(runes[i:i+size])] = struct{}{}
	}
	return result
}

func longestCommonResumeRunes(left, right []rune) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	previous := make([]int, len(right)+1)
	best := 0
	for i := 1; i <= len(left); i++ {
		current := make([]int, len(right)+1)
		for j := 1; j <= len(right); j++ {
			if left[i-1] == right[j-1] {
				current[j] = previous[j-1] + 1
				if current[j] > best {
					best = current[j]
				}
			}
		}
		previous = current
	}
	return best
}

func resumeTextSimilarity(left, right string) float64 {
	l := normalizeResumeCompareText(left)
	r := normalizeResumeCompareText(right)
	if len(l) < 2 || len(r) < 2 {
		return 0
	}
	leftBigrams := resumeNGramSet(l, 2)
	rightBigrams := resumeNGramSet(r, 2)
	shared := 0
	for gram := range leftBigrams {
		if _, ok := rightBigrams[gram]; ok {
			shared++
		}
	}
	minimum := len(leftBigrams)
	if len(rightBigrams) < minimum {
		minimum = len(rightBigrams)
	}
	containment := 0.0
	if minimum > 0 {
		containment = float64(shared) / float64(minimum)
	}
	longest := longestCommonResumeRunes(l, r)
	longestScore := float64(longest) / float64(minInt(len(l), len(r)))
	// A stable four-character factual phrase such as “非转基因” is already
	// audible repetition in a live room even when the surrounding sentence differs.
	phraseScore := 0.0
	if longest >= 6 {
		phraseScore = 0.72
	} else if longest >= 5 {
		phraseScore = 0.62
	} else if longest >= 4 {
		phraseScore = 0.52
	}
	return maxFloat(containment, longestScore, phraseScore)
}

func maxFloat(values ...float64) float64 {
	best := 0.0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
}

func lastResumeSentences(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" || limit <= 0 {
		return nil
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune("。！？!?；;\n", r)
	})
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			clean = append(clean, part)
		}
	}
	if len(clean) <= limit {
		return clean
	}
	return clean[len(clean)-limit:]
}

func resumePointAtOrAfter(program audioout.RoomProgramSnapshot, offsetMS int) (audioout.ProgramSafePoint, bool) {
	points := effectiveRoomProgramSafePoints(program)
	for _, point := range points {
		if point.CutMS == offsetMS {
			return point, true
		}
	}
	return audioout.ProgramSafePoint{}, false
}

type semanticResumeBreakpoint struct {
	CutAfterSegmentID       string
	OriginalResumeSegmentID string
	CoveredSegmentIDs       []string
	PlannedResumeSegmentID  string
	SkipCount               int
}

func semanticResumeBreakpointFor(program audioout.RoomProgramSnapshot, cutMS, resumeMS int) semanticResumeBreakpoint {
	result := semanticResumeBreakpoint{}
	if cutMS < 0 {
		cutMS = 0
	}
	if resumeMS < cutMS {
		resumeMS = cutMS
	}
	for _, segment := range program.Timeline {
		if segment.EndMS == cutMS && strings.TrimSpace(segment.SegmentID) != "" {
			result.CutAfterSegmentID = strings.TrimSpace(segment.SegmentID)
		}
		if result.OriginalResumeSegmentID == "" && segment.StartMS >= cutMS && strings.TrimSpace(segment.SegmentID) != "" {
			result.OriginalResumeSegmentID = strings.TrimSpace(segment.SegmentID)
		}
		if result.PlannedResumeSegmentID == "" && segment.StartMS >= resumeMS && strings.TrimSpace(segment.SegmentID) != "" {
			result.PlannedResumeSegmentID = strings.TrimSpace(segment.SegmentID)
		}
		if segment.StartMS >= cutMS && segment.StartMS < resumeMS && strings.TrimSpace(segment.SegmentID) != "" {
			result.CoveredSegmentIDs = append(result.CoveredSegmentIDs, strings.TrimSpace(segment.SegmentID))
		}
	}
	if result.CutAfterSegmentID == "" {
		if point, ok := resumePointAtOrAfter(program, cutMS); ok {
			result.CutAfterSegmentID = strings.TrimSpace(point.SentenceID)
		}
	}
	result.SkipCount = len(result.CoveredSegmentIDs)
	return result
}

func resumePreview(program audioout.RoomProgramSnapshot, point audioout.ProgramSafePoint) string {
	parts := make([]string, 0, 2)
	if preview := strings.TrimSpace(point.NextPreview); preview != "" {
		parts = append(parts, preview)
	}
	for index, segment := range program.Timeline {
		if segment.StartMS != point.CutMS {
			continue
		}
		if text := strings.TrimSpace(segment.Text); text != "" && (len(parts) == 0 || text != parts[0]) {
			parts = append(parts, text)
		}
		if index+1 < len(program.Timeline) {
			if next := strings.TrimSpace(program.Timeline[index+1].Text); next != "" {
				parts = append(parts, next)
			}
		}
		break
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, " ")
}

func resumeDuplicateScore(replyText, preview string) (float64, string) {
	tails := lastResumeSentences(replyText, 2)
	if len(tails) == 0 || strings.TrimSpace(preview) == "" {
		return 0, ""
	}
	best := 0.0
	bestTail := ""
	for _, tail := range tails {
		if score := resumeTextSimilarity(tail, preview); score > best {
			best = score
			bestTail = tail
		}
	}
	if len(tails) == 2 {
		combined := strings.Join(tails, " ")
		if score := resumeTextSimilarity(combined, preview); score > best {
			best = score
			bestTail = combined
		}
	}
	return best, bestTail
}

func applyResumeDedupGate(program audioout.RoomProgramSnapshot, replyText string, originalMS int, strategy string) resumeDedupDecision {
	strategy = strings.ToUpper(strings.TrimSpace(strategy))
	decision := resumeDedupDecision{
		OriginalStrategy: strategy,
		FinalStrategy:    strategy,
		OriginalMS:       originalMS,
		FinalMS:          originalMS,
	}
	point, ok := resumePointAtOrAfter(program, originalMS)
	if !ok {
		return decision
	}
	decision.OriginalPointID = point.ID
	decision.FinalPointID = point.ID
	preview := resumePreview(program, point)
	decision.OriginalPreview = preview
	score, tail := resumeDuplicateScore(replyText, preview)
	decision.Score = score
	decision.ReplyTail = tail
	if score < 0.5 {
		return decision
	}
	decision.Triggered = true
	decision.Reason = "reply_tail_overlaps_resume_preview"

	points := effectiveRoomProgramSafePoints(program)
	sort.SliceStable(points, func(i, j int) bool { return points[i].CutMS < points[j].CutMS })
	skipped := 0
	for _, candidate := range points {
		if candidate.CutMS <= originalMS {
			continue
		}
		skipped++
		candidatePreview := resumePreview(program, candidate)
		candidateScore, _ := resumeDuplicateScore(replyText, candidatePreview)
		if strings.TrimSpace(candidatePreview) == "" || candidateScore < 0.42 {
			decision.FinalMS = candidate.CutMS
			decision.FinalPointID = candidate.ID
			decision.FinalPreview = candidatePreview
			decision.SkippedPoints = skipped
			if strategy != "SWITCH_PLAN" {
				if skipped >= 2 || strategy == "CROSS_RESUME" {
					decision.FinalStrategy = "CROSS_RESUME"
				} else {
					decision.FinalStrategy = "FUSION_SKIP"
				}
			}
			return decision
		}
	}

	if program.Task != nil && program.Task.DurationMS > originalMS {
		decision.FinalMS = program.Task.DurationMS
		decision.FinalPointID = "TRACK_END"
		decision.FinalPreview = ""
		decision.SkippedPoints = skipped
		decision.FinalStrategy = "SWITCH_PLAN"
		decision.Reason = "reply_tail_overlaps_remaining_track"
	}
	return decision
}

func nextRoomProgramSafeCut(program audioout.RoomProgramSnapshot, currentMS int, maxWait time.Duration) (int, bool) {
	if currentMS < 0 {
		currentMS = 0
	}
	maxWaitMS := int(maxWait.Milliseconds())
	if maxWaitMS <= 0 {
		maxWaitMS = 35000
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 1, maxWaitMS, minInt(maxWaitMS, 12000)); ok {
		return point.CutMS, true
	}
	// Development/test WAVs still use the legacy static map.
	if len(program.SafePoints) == 0 && len(program.Timeline) == 0 {
		if point, exists := devMainlineNextSafePoint(currentMS, maxWait); exists {
			return point.CutMS, true
		}
	}
	return 0, false
}

func plannedRoomProgramSafeCut(program audioout.RoomProgramSnapshot, currentMS int) (int, bool) {
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 30000, 55000, 42000); ok {
		return point.CutMS, true
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 24000, 50000, 36000); ok {
		return point.CutMS, true
	}
	return 0, false
}

func requestedRoomProgramSafeCut(program audioout.RoomProgramSnapshot, requestedMS, currentMS int, maxWait time.Duration) (int, bool) {
	if requestedMS <= currentMS || requestedMS <= 0 {
		return 0, false
	}
	maxWaitMS := int(maxWait.Milliseconds())
	if maxWaitMS <= 0 {
		maxWaitMS = 35000
	}
	if requestedMS-currentMS > maxWaitMS {
		return 0, false
	}
	for _, point := range effectiveRoomProgramSafePoints(program) {
		if point.CutMS == requestedMS {
			return requestedMS, true
		}
	}
	return 0, false
}

func resolveRoomProgramSafeCutAfterGeneration(program audioout.RoomProgramSnapshot, preferredMS, currentMS int) (int, bool) {
	if preferredMS > 0 {
		lead := preferredMS - currentMS
		if lead >= 4000 && lead <= 33000 {
			if cut, ok := requestedRoomProgramSafeCut(program, preferredMS, currentMS, 33*time.Second); ok {
				return cut, true
			}
		}
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 5000, 30000, 12000); ok {
		return point.CutMS, true
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 3000, 33000, 10000); ok {
		return point.CutMS, true
	}
	return 0, false
}

func resolveQuickRoomProgramSafeCut(program audioout.RoomProgramSnapshot, preferredMS, currentMS int) (int, bool) {
	if preferredMS > 0 {
		lead := preferredMS - currentMS
		if lead >= 3000 && lead <= 24000 {
			if cut, ok := requestedRoomProgramSafeCut(program, preferredMS, currentMS, 24*time.Second); ok {
				return cut, true
			}
		}
	}
	// 抢答也必须等到完整句末再切。至少预留约 3 秒，让前端能够明确标出
	// “停止前”这一句；避免 TTS 刚准备好就立刻硬切，用户只听到突然停止。
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 3000, 16000, 5500); ok {
		return point.CutMS, true
	}
	if point, ok := chooseRoomProgramSafePointWindow(program, currentMS, 1500, 24000, 6500); ok {
		return point.CutMS, true
	}
	return 0, false
}

func chooseRoomProgramSafePointWindow(
	program audioout.RoomProgramSnapshot,
	currentMS, minLeadMS, maxLeadMS, targetLeadMS int,
) (audioout.ProgramSafePoint, bool) {
	durationMS := 0
	if program.Task != nil {
		durationMS = program.Task.DurationMS
	}
	points := effectiveRoomProgramSafePoints(program)
	candidates := make([]audioout.ProgramSafePoint, 0, len(points))
	for _, point := range points {
		lead := point.CutMS - currentMS
		if lead < minLeadMS || lead > maxLeadMS {
			continue
		}
		if durationMS > 0 && point.CutMS >= durationMS {
			continue
		}
		candidates = append(candidates, point)
	}
	for _, grade := range []string{"A", "B", "C"} {
		best, ok := bestRoomProgramSafePoint(candidates, grade, currentMS, targetLeadMS)
		if ok {
			return best, true
		}
	}
	return bestRoomProgramSafePoint(candidates, "", currentMS, targetLeadMS)
}

func effectiveRoomProgramSafePoints(program audioout.RoomProgramSnapshot) []audioout.ProgramSafePoint {
	if len(program.SafePoints) > 0 {
		return program.SafePoints
	}
	points := make([]audioout.ProgramSafePoint, 0, len(program.Timeline))
	for index, segment := range program.Timeline {
		if !segment.SafeCut || segment.EndMS <= segment.StartMS {
			continue
		}
		nextPreview := ""
		if index+1 < len(program.Timeline) {
			nextPreview = strings.TrimSpace(program.Timeline[index+1].Text)
		}
		points = append(points, audioout.ProgramSafePoint{
			ID:          fmt.Sprintf("LEGACY-SP%03d", len(points)+1),
			CutMS:       segment.EndMS,
			Score:       90,
			Grade:       "B",
			Kind:        "SENTENCE",
			SentenceID:  segment.SegmentID,
			LeftPreview: strings.TrimSpace(segment.Text),
			NextPreview: nextPreview,
		})
	}
	return points
}

func bestRoomProgramSafePoint(
	candidates []audioout.ProgramSafePoint,
	grade string,
	currentMS, targetLeadMS int,
) (audioout.ProgramSafePoint, bool) {
	var best audioout.ProgramSafePoint
	bestDistance := int(^uint(0) >> 1)
	found := false
	for _, point := range candidates {
		if grade != "" && strings.ToUpper(strings.TrimSpace(point.Grade)) != grade {
			continue
		}
		distance := point.CutMS - currentMS - targetLeadMS
		if distance < 0 {
			distance = -distance
		}
		if !found || distance < bestDistance || (distance == bestDistance && point.Score > best.Score) {
			best = point
			bestDistance = distance
			found = true
		}
	}
	return best, found
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) scheduleRoomAudioInteractionCompletion(roomID int64, task audioout.SpeechTask, decisionID string, waitForMainline bool) {
	if roomID <= 0 || strings.TrimSpace(task.ID) == "" || task.DurationMS <= 0 {
		return
	}
	startedAt := task.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	// Receiver COMPLETED/FAILED events normally close the interaction.
	// This timer is only a fallback for a receiver that disappears, so leave
	// enough grace for buffering/startup delay before force-completing it.
	deadline := startedAt.Add(time.Duration(task.DurationMS+4000) * time.Millisecond)
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		if s.speechRuntime == nil {
			return
		}
		snapshot, err := s.speechRuntime.Snapshot(roomID)
		if err != nil || snapshot.Interrupt.SpeechTaskID != task.ID {
			return
		}
		if snapshot.Interrupt.Status != speechruntime.StatusPlaying && snapshot.Interrupt.Status != speechruntime.StatusReady {
			return
		}
		status := speechruntime.StatusCompleted
		if waitForMainline {
			status = speechruntime.StatusReturning
		}
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:          speechruntime.TrackInterrupt,
			Status:         status,
			Text:           snapshot.Interrupt.Text,
			QuestionText:   snapshot.Interrupt.QuestionText,
			ReplyText:      snapshot.Interrupt.ReplyText,
			ResumeStrategy: snapshot.Interrupt.ResumeStrategy,
			BridgeText:     snapshot.Interrupt.BridgeText,
			BridgeUsed:     snapshot.Interrupt.BridgeUsed,
			Source:         snapshot.Interrupt.Source,
			AudioURL:       snapshot.Interrupt.AudioURL,
			DecisionID:     snapshot.Interrupt.DecisionID,
			MissionID:      snapshot.Interrupt.MissionID,
			SpeechTaskID:   snapshot.Interrupt.SpeechTaskID,
			SwitchAtMS:     snapshot.Interrupt.SwitchAtMS,
			DurationMS:     snapshot.Interrupt.DurationMS,
			StartedAt:      snapshot.Interrupt.StartedAt,
		})
		if strings.TrimSpace(decisionID) == "" {
			return
		}
		if waitForMainline {
			s.completeInteractionAfterMainlineResume(roomID, task.ID, decisionID)
		} else if s.agentDecisions != nil {
			_, _ = s.agentDecisions.Complete(roomID, decisionID)
		}
	})
}

func (s *Server) completeInteractionAfterMainlineResume(roomID int64, taskID, decisionID string) {
	if roomID <= 0 || strings.TrimSpace(taskID) == "" || strings.TrimSpace(decisionID) == "" {
		return
	}
	go func() {
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()

		for {
			if s.speechRuntime == nil || s.roomAudio == nil {
				return
			}
			speech, err := s.speechRuntime.Snapshot(roomID)
			if err != nil || speech.Interrupt.SpeechTaskID != taskID || speech.Interrupt.DecisionID != decisionID {
				return
			}
			if speech.Interrupt.Status == speechruntime.StatusCompleted || speech.Interrupt.Status == speechruntime.StatusFailed {
				return
			}
			engine := s.roomAudio.Snapshot(roomID)
			if engine.Phase == roomaudio.PhaseMainline && engine.ActiveSource == roomaudio.SourceMainline {
				_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
					Track:          speechruntime.TrackInterrupt,
					Status:         speechruntime.StatusCompleted,
					Text:           speech.Interrupt.Text,
					QuestionText:   speech.Interrupt.QuestionText,
					ReplyText:      speech.Interrupt.ReplyText,
					ResumeStrategy: speech.Interrupt.ResumeStrategy,
					BridgeText:     speech.Interrupt.BridgeText,
					BridgeUsed:     speech.Interrupt.BridgeUsed,
					Source:         speech.Interrupt.Source,
					AudioURL:       speech.Interrupt.AudioURL,
					DecisionID:     speech.Interrupt.DecisionID,
					MissionID:      speech.Interrupt.MissionID,
					SpeechTaskID:   speech.Interrupt.SpeechTaskID,
					SwitchAtMS:     speech.Interrupt.SwitchAtMS,
					DurationMS:     speech.Interrupt.DurationMS,
					StartedAt:      speech.Interrupt.StartedAt,
				})
				if s.agentDecisions != nil {
					_, _ = s.agentDecisions.Complete(roomID, decisionID)
				}
				log.Printf("interaction lifecycle completed after mainline resume room=%d task=%s decision=%s", roomID, taskID, decisionID)
				return
			}

			select {
			case <-deadline.C:
				log.Printf("interaction lifecycle still returning after timeout room=%d task=%s decision=%s phase=%s source=%s", roomID, taskID, decisionID, engine.Phase, engine.ActiveSource)
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Server) dispatchRoomAudioInteraction(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	room, err := s.rooms.Get(r.Context(), &tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(room.Status), "live") {
		writeError(w, http.StatusConflict, "直播间当前未开播")
		return
	}
	if s.events != nil {
		stats, statsErr := s.events.GetSessionStats(r.Context(), roomID)
		if statsErr != nil {
			writeError(w, http.StatusInternalServerError, "read session state failed")
			return
		}
		if stats.ResumePending {
			writeError(w, http.StatusConflict, "请先选择直播续接方式")
			return
		}
	}
	if s.agentWork == nil || !s.agentWork.IsWorking(roomID) {
		writeError(w, http.StatusConflict, "直播搭子尚未工作")
		return
	}

	var input roomAudioInteractionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.DecisionID = strings.TrimSpace(input.DecisionID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	input.AudioURL = strings.TrimSpace(input.AudioURL)
	input.Question = strings.TrimSpace(input.Question)
	input.ReplyText = strings.TrimSpace(input.ReplyText)
	input.MissionID = strings.TrimSpace(input.MissionID)
	if input.MissionID == "" {
		input.MissionID = input.DecisionID
	}
	input.Topic = strings.TrimSpace(input.Topic)
	input.InterruptStrategy = strings.ToLower(strings.TrimSpace(input.InterruptStrategy))
	input.ResumeStrategy = strings.ToUpper(strings.TrimSpace(input.ResumeStrategy))
	input.BridgeText = strings.TrimSpace(input.BridgeText)
	if input.SwitchAtMS < 0 {
		input.SwitchAtMS = 0
	}
	if input.DecisionID == "" || input.AudioURL == "" {
		writeError(w, http.StatusBadRequest, "decision_id and audio_url are required")
		return
	}
	if input.Action != "quick" {
		input.Action = "answer"
	}
	if s.agentDecisions == nil {
		writeError(w, http.StatusServiceUnavailable, "agent decision queue is not configured")
		return
	}
	decision, exists := s.agentDecisions.Get(roomID, input.DecisionID)
	if !exists || decision.Status != "CLAIMED" {
		writeError(w, http.StatusConflict, "待打断任务已被移除或不再执行")
		return
	}
	if s.speechRuntime != nil {
		if speech, speechErr := s.speechRuntime.Snapshot(roomID); speechErr == nil && speechSnapshotBusy(speech) {
			writeError(w, http.StatusConflict, "上一条TTS尚未播放完成，请继续排队")
			return
		}
	}

	controlMode := s.agentWork.Mode(roomID) == agentwork.ModeControl
	state := s.audioDevState()
	if !controlMode && (state == nil || state.client == nil || !state.client.Enabled()) {
		writeError(w, http.StatusServiceUnavailable, "主线播音调度尚未配置")
		return
	}
	if controlMode && s.audioHub == nil {
		writeError(w, http.StatusServiceUnavailable, "Core声音广播尚未配置")
		return
	}
	var program audioout.RoomProgramSnapshot
	var switchAtMS *int
	var resumeOffsetMS *int
	resumeMode := "DIRECT"
	bridgeUsed := false
	resumeDedupTriggered := false
	resumeDuplicateScore := 0.0
	strategySignals := strategycenter.Signals{}
	if s.brain != nil {
		if view, viewErr := s.brain.Snapshot(roomID); viewErr == nil {
			strategySignals.Entries30s = view.Intelligence.Entries30s
			strategySignals.Likes30s = view.Intelligence.Likes30s
			strategySignals.Follows30s = view.Intelligence.Follows30s
			strategySignals.Heat = view.Intelligence.Heat
		}
	}
	interruptStrategy := input.InterruptStrategy
	interruptStrategySource := "management"
	knownInterrupt := false
	switch interruptStrategy {
	case "read_comment_softly", "hard_cut", "ask_controller", "thinking_pause", "repeat_confirm":
		knownInterrupt = true
	}
	if !knownInterrupt || (s.strategyPolicies != nil && !s.strategyPolicies.Allowed(tenantID, "interrupt", interruptStrategy)) {
		interruptStrategy = ""
		interruptStrategySource = "core_fallback"
	}
	if interruptStrategy == "" && s.strategyPolicies != nil {
		selected := s.strategyPolicies.Pick(
			tenantID,
			"interrupt",
			[]string{"read_comment_softly", "hard_cut", "ask_controller", "thinking_pause", "repeat_confirm"},
			strategySignals,
			input.DecisionID,
		)
		interruptStrategy = selected.Key
		log.Printf("core interrupt strategy room=%d decision=%s selected=%s source=%s requested=%s candidates=%v roll=%d/%d", roomID, input.DecisionID, selected.Key, interruptStrategySource, input.InterruptStrategy, selected.Candidates, selected.Roll, selected.Total)
	} else if interruptStrategy != "" {
		log.Printf("core interrupt strategy room=%d decision=%s selected=%s source=%s requested=%s", roomID, input.DecisionID, interruptStrategy, interruptStrategySource, input.InterruptStrategy)
	}
	if !controlMode {
		program, err = state.client.ProgramSnapshot(r.Context(), roomID)
		if err != nil || !program.Running || program.Task == nil {
			writeError(w, http.StatusConflict, "当前没有可插播的主线音频")
			return
		}
		currentMS := program.CurrentMS
		if currentMS <= 0 {
			currentMS = program.Task.StartMS
		}
		cutMS := 0
		exists := false
		cutAction := input.Action
		if interruptStrategy == "hard_cut" {
			cutAction = "quick"
		}
		if cutAction == "answer" {
			cutMS, exists = resolveRoomProgramSafeCutAfterGeneration(program, input.SwitchAtMS, currentMS)
			if !exists && input.ForceAfterRest {
				cutMS, exists = nextRoomProgramSafeCut(program, currentMS, 10*time.Minute)
				if !exists && program.Task.DurationMS > currentMS {
					cutMS = program.Task.DurationMS
					exists = true
				}
				if exists {
					log.Printf("interaction forced dispatch after rest room=%d decision=%s current_ms=%d switch_ms=%d", roomID, input.DecisionID, currentMS, cutMS)
				}
			}
		} else {
			cutMS, exists = resolveQuickRoomProgramSafeCut(program, input.SwitchAtMS, currentMS)
		}
		if cutAction == "answer" {
			if !exists {
				writeError(w, http.StatusConflict, "当前35秒内没有安全句末切点，继续排队")
				return
			}
			value := cutMS
			switchAtMS = &value
			resumeOffsetMS = &value
		} else if exists {
			// 抢答也在完整句末切换；切点同时用于停止主线和插播后的恢复位置。
			value := cutMS
			switchAtMS = &value
			resumeOffsetMS = &value
		} else if program.Task.DurationMS > 1 {
			// 已处于本稿最后语义段时，抢答后直接跨到下一稿，避免回到半句和残留 1ms 杂音。
			value := program.Task.DurationMS
			resumeOffsetMS = &value
		}
		if switchAtMS != nil && resumeOffsetMS != nil && s.strategyPolicies != nil {
			estimatedMS := estimatedInteractionDurationMS(input.ReplyText)
			candidates := resumeCandidatesForInteraction(program, *switchAtMS, estimatedMS, input.Topic)
			requested := strings.ToUpper(strings.TrimSpace(input.ResumeStrategy))
			requestedAllowed := false
			for _, candidate := range candidates {
				if strings.EqualFold(candidate, requested) && s.strategyPolicies.Allowed(tenantID, "resume", candidate) {
					requestedAllowed = true
					break
				}
			}
			selected := s.strategyPolicies.Pick(tenantID, "resume", candidates, strategySignals, input.DecisionID+":resume")
			if requestedAllowed {
				selected = s.strategyPolicies.Pick(tenantID, "resume", []string{requested}, strategySignals, input.DecisionID+":resume")
			}
			// BRIDGE means the bridge sentence is integrated into the generated
			// answer audio. Do not report/use a bridge picked only after TTS was
			// already generated, because that would still sound like a hard cut.
			if strings.EqualFold(selected.Key, "BRIDGE") && !strings.EqualFold(requested, "BRIDGE") {
				fallback := make([]string, 0, len(candidates))
				for _, candidate := range candidates {
					if !strings.EqualFold(candidate, "BRIDGE") {
						fallback = append(fallback, candidate)
					}
				}
				selected = s.strategyPolicies.Pick(tenantID, "resume", fallback, strategySignals, input.DecisionID+":resume:fallback")
			}
			if selected.Key != "" {
				bridgeUsed = strings.EqualFold(selected.Key, "BRIDGE") && strings.EqualFold(requested, "BRIDGE")
				resumeTo, reason := resumeOffsetForStrategy(program, *switchAtMS, selected.Key, input.Topic)
				dedup := applyResumeDedupGate(program, input.ReplyText, resumeTo, selected.Key)
				resumeDedupTriggered = dedup.Triggered
				resumeDuplicateScore = dedup.Score
				resumeMode = dedup.FinalStrategy
				*resumeOffsetMS = dedup.FinalMS
				if dedup.Triggered {
					log.Printf(
						"core resume dedup room=%d decision=%s original_policy=%s original_ms=%d original_point=%s duplicate_score=%.3f duplicate_reason=%s reply_tail=%q original_preview=%q final_policy=%s final_ms=%d final_point=%s skipped_points=%d final_preview=%q",
						roomID,
						input.DecisionID,
						selected.Key,
						resumeTo,
						dedup.OriginalPointID,
						dedup.Score,
						dedup.Reason,
						dedup.ReplyTail,
						dedup.OriginalPreview,
						dedup.FinalStrategy,
						dedup.FinalMS,
						dedup.FinalPointID,
						dedup.SkippedPoints,
						dedup.FinalPreview,
					)
				}
				log.Printf(
					"core resume strategy room=%d decision=%s interrupt_ms=%d candidates=%v weights=%v original_policy=%s resume_policy=%s bridge_used=%t resume_from_ms=%d original_resume_to_ms=%d resume_to_ms=%d original_safe_point=%s safe_point=%s reason=%s dedup_triggered=%t duplicate_score=%.3f",
					roomID,
					input.DecisionID,
					estimatedMS,
					candidates,
					selected.Candidates,
					selected.Key,
					resumeMode,
					bridgeUsed,
					*switchAtMS,
					resumeTo,
					*resumeOffsetMS,
					dedup.OriginalPointID,
					dedup.FinalPointID,
					reason,
					dedup.Triggered,
					dedup.Score,
				)
			}
		}
		if switchAtMS != nil {
			log.Printf(
				"core audio interaction plan room=%d action=%s current_ms=%d switch_ms=%d lead_ms=%d",
				roomID, input.Action, currentMS, *switchAtMS, *switchAtMS-currentMS,
			)
		}
	}

	source := "manual_answer"
	label := "回答"
	if !controlMode && resumeMode == "DIRECT" {
		resumeMode = "SAFE"
	}
	if !bridgeUsed {
		input.BridgeText = ""
	}
	if input.Action == "quick" {
		source = "manual_quick"
		label = "抢答"
		resumeMode = "HARD"
	}
	if s.speechRuntime != nil {
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:          speechruntime.TrackInterrupt,
			Status:         speechruntime.StatusReady,
			Text:           input.ReplyText,
			QuestionText:   input.Question,
			ReplyText:      input.ReplyText,
			ResumeStrategy: resumeMode,
			BridgeText:     input.BridgeText,
			BridgeUsed:     bridgeUsed,
			Source:         source,
			AudioURL:       input.AudioURL,
			DecisionID:     input.DecisionID,
			MissionID:      input.MissionID,
			SwitchAtMS:     switchAtMS,
		})
	}

	var task audioout.SpeechTask
	if controlMode {
		sessionID := input.SessionID
		if sessionID == "" {
			sessionID = fmt.Sprintf("control-%d", roomID)
		}
		hubTask, hubErr := s.audioHub.CreateExternalTask(
			r.Context(), roomID, sessionID, fmt.Sprintf("%s · %s", label, input.Topic), input.AudioURL,
		)
		err = hubErr
		if hubErr == nil {
			task = audioout.SpeechTask{
				ID:         hubTask.ID,
				RoomID:     hubTask.RoomID,
				SessionID:  hubTask.SessionID,
				Kind:       hubTask.Kind,
				Label:      hubTask.Label,
				AudioURL:   hubTask.AudioURL,
				MimeType:   hubTask.MimeType,
				DurationMS: hubTask.DurationMS,
				StartMS:    hubTask.StartMS,
				Sequence:   hubTask.Sequence,
				StartedAt:  hubTask.StartedAt,
				CreatedAt:  hubTask.CreatedAt,
			}
		}
	} else {
		callbackBase := state.callbackBase
		if callbackBase == "" {
			callbackBase = "http://127.0.0.1:8081"
		}
		callbackURL := callbackBase + "/internal/v1/audio/events"
		var snapshot audioout.RoomProgramSnapshot
		snapshot, err = state.client.InsertTestProgramInteraction(r.Context(), audioout.InsertInteractionInput{
			RoomID:         roomID,
			SessionID:      program.Task.SessionID,
			Label:          fmt.Sprintf("%s · %s", label, input.Topic),
			AudioURL:       input.AudioURL,
			Text:           strings.TrimSpace(input.ReplyText),
			CallbackURL:    callbackURL,
			ResumeOffsetMS: resumeOffsetMS,
			SwitchAtMS:     switchAtMS,
		})
		if err == nil && snapshot.Task != nil {
			task = *snapshot.Task
		}
	}
	if err != nil {
		if s.speechRuntime != nil {
			_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
				Track:          speechruntime.TrackInterrupt,
				Status:         speechruntime.StatusFailed,
				Text:           input.ReplyText,
				QuestionText:   input.Question,
				ReplyText:      input.ReplyText,
				ResumeStrategy: resumeMode,
				BridgeText:     input.BridgeText,
				BridgeUsed:     bridgeUsed,
				Source:         source,
				AudioURL:       input.AudioURL,
				DecisionID:     input.DecisionID,
				MissionID:      input.MissionID,
			})
		}
		writeError(w, http.StatusBadGateway, "提交插播失败: "+err.Error())
		return
	}
	if strings.TrimSpace(task.ID) == "" {
		writeError(w, http.StatusBadGateway, "播音分发层未返回任务")
		return
	}

	if state != nil {
		state.mu.Lock()
		state.interactions[task.ID] = &audioInteractionMeta{
			RoomID:               roomID,
			MissionID:            input.MissionID,
			HumanizationStrategy: input.HumanizationStrategy,
			HumanizationKind:     input.HumanizationKind,
			HumanizationApplied:  input.HumanizationApplied,
			Topic:                input.Topic,
			ResumeMode:           resumeMode,
			BridgeText:           input.BridgeText,
			BridgeUsed:           bridgeUsed,
			DecisionID:           input.DecisionID,
			QuestionText:         input.Question,
			ReplyText:            input.ReplyText,
			Source:               source,
			AudioURL:             input.AudioURL,
			WaitForMainline:      !controlMode,
		}
		state.mu.Unlock()
	}

	if s.speechRuntime != nil {
		startedAt := task.StartedAt
		if startedAt.IsZero() {
			startedAt = time.Now().UTC()
		}
		durationMS := task.DurationMS
		if durationMS <= 0 {
			durationMS = estimatedInteractionDurationMS(input.ReplyText)
		}
		_, _ = s.speechRuntime.Update(roomID, speechruntime.UpdateInput{
			Track:          speechruntime.TrackInterrupt,
			Status:         speechruntime.StatusPlaying,
			Text:           input.ReplyText,
			QuestionText:   input.Question,
			ReplyText:      input.ReplyText,
			ResumeStrategy: resumeMode,
			BridgeText:     input.BridgeText,
			BridgeUsed:     bridgeUsed,
			Source:         source,
			AudioURL:       input.AudioURL,
			DecisionID:     input.DecisionID,
			MissionID:      input.MissionID,
			SwitchAtMS:     switchAtMS,
			SpeechTaskID:   task.ID,
			DurationMS:     durationMS,
			StartedAt:      &startedAt,
		})
	}
	s.scheduleRoomAudioInteractionCompletion(roomID, task, input.DecisionID, !controlMode)
	actualResumeSegmentID := ""
	actualSkipCount := 0
	if !controlMode && switchAtMS != nil && resumeOffsetMS != nil {
		breakpoint := semanticResumeBreakpointFor(program, *switchAtMS, *resumeOffsetMS)
		actualResumeSegmentID = breakpoint.PlannedResumeSegmentID
		actualSkipCount = breakpoint.SkipCount
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mission_id":        input.MissionID,
		"dispatched":        true,
		"action":            input.Action,
		"switch_at_ms":      switchAtMS,
		"resume_offset_ms":  resumeOffsetMS,
		"resume_strategy":   resumeMode,
		"resume_segment_id": actualResumeSegmentID,
		"actual_skip_count": actualSkipCount,
		"bridge_used":       bridgeUsed,
		"dedup_triggered":   resumeDedupTriggered,
		"duplicate_score":   resumeDuplicateScore,
		"task":              task,
	})
}
