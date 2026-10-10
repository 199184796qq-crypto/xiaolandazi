package anchorcycle

import "time"

type GenerationSkip struct {
	Task      Task        `json:"task"`
	Stage     string      `json:"stage"`
	Reason    string      `json:"reason"`
	Attempts  []Candidate `json:"attempts,omitempty"`
	CreatedAt string      `json:"created_at"`
}

// Skip an unusable paragraph, preserving its trace without spending adoption quotas.
func (s *State) SkipGeneration(stage, reason string) {
	task := s.Task()
	s.GenerationSkips = append(s.GenerationSkips, GenerationSkip{Task: task, Stage: stage, Reason: reason, Attempts: append([]Candidate(nil), s.Attempts...), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if len(s.GenerationSkips) > 50 {
		s.GenerationSkips = s.GenerationSkips[len(s.GenerationSkips)-50:]
	}
	s.Warn("已跳过本段，继续下一任务：" + reason)
	s.Status, s.Error, s.Busy, s.Uncertain = "ready", "", false, false
	s.PriceHook = nil
	s.PendingOpeningHook = nil
	s.P0PendingRepair = nil
	s.P0AuditPending = nil
	s.Attempts = nil
	s.P0RewritesUsed = 0
	s.PendingFeedback = ""
	s.FeedbackPending = false
	if task.ProductMode == "lecture" {
		s.ReleaseLecture(true)
		return
	}
	s.Cursor++
	if s.Phase == "ended" || s.Phase == "closing" {
		s.Phase = "opening"
		s.CycleNumber++
		s.BodyCount = 0
		s.planBodyTarget()
	}
	if s.ProductFocus != nil && s.ProductFocus.AuxiliaryActive {
		s.ProductFocus.AuxiliaryActive = false
		s.Phase = "opening"
		s.BodyCount = 0
		s.planBodyTarget()
	}
	if s.P0SkippedKeys == nil {
		s.P0SkippedKeys = map[string]bool{}
	}
	s.P0SkippedKeys[task.Key] = true
	if s.Config.Rolling {
		s.chooseNext()
	} else if s.Cursor >= len(s.Queue) {
		s.Phase = "closing"
	}
}
