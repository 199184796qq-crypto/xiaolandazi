package anchorcycle

import (
	"strings"
	"time"
)

// Availability of the review service is separate from content eligibility.
// After three unavailable batches, wait for the operator to address the warning
// and restart the run. Do not keep paying for new drafts in an unavailable run.
type FinalReviewRecovery struct {
	Failures   int                  `json:"failures"`
	Paused     bool                 `json:"paused,omitempty"`
	RetryAfter time.Time            `json:"retry_after,omitempty"`
	Cooling    map[string]time.Time `json:"cooling,omitempty"`
	Kind       string               `json:"kind"`
	Reason     string               `json:"reason"`
}

func (s *State) FinalReviewReady(now time.Time) bool {
	r := s.FinalReviewRecovery
	return r == nil || (!r.Paused && !now.Before(r.RetryAfter))
}

func (s *State) resetFinalReviewRecovery() {
	if s.FinalReviewRecovery != nil {
		s.FinalReviewRecovery = nil
		s.Error = ""
	}
}

func (s *State) deferFinalReview(c Candidate, now time.Time) {
	task := s.Task()
	kind, reason := "unavailable", "最终文字审核未完成或回执已失效"
	if a := c.FinalContentAudit; a != nil {
		if a.Error != "" {
			reason = a.Error
		} else if a.Decision == "不确定" {
			kind, reason = "uncertain", "审核缺少信息，暂不能判断"
			if strings.TrimSpace(a.Reason) != "" {
				reason += "：" + a.Reason
			}
		} else if a.Decision == "有问题" && strings.TrimSpace(a.Reason) == "" {
			reason = "审核未提供具体退稿原因"
		}
	}
	s.Attempts = append(s.Attempts, c)
	s.GenerationSkips = append(s.GenerationSkips, GenerationSkip{Task: task, Stage: "final_content_review_" + kind, Reason: reason, Attempts: append([]Candidate(nil), s.Attempts...), CreatedAt: now.Format(time.RFC3339Nano)})
	if len(s.GenerationSkips) > 50 {
		s.GenerationSkips = s.GenerationSkips[len(s.GenerationSkips)-50:]
	}
	s.Attempts, s.P0PendingRepair, s.P0AuditPending = nil, nil, nil
	s.P0RewritesUsed = 0
	s.Status, s.Error, s.Busy = "ready", reason, false
	if s.FinalReviewRecovery == nil {
		s.FinalReviewRecovery = &FinalReviewRecovery{Cooling: map[string]time.Time{}}
	}
	r := s.FinalReviewRecovery
	if r.Cooling == nil {
		r.Cooling = map[string]time.Time{}
	}
	r.Failures++
	r.Kind, r.Reason = kind, reason
	r.RetryAfter = now.Add(time.Second * time.Duration(1<<min(r.Failures-1, 2)))
	for key, until := range r.Cooling {
		if !now.Before(until) {
			delete(r.Cooling, key)
		}
	}
	r.Cooling[task.Key] = now.Add(5 * time.Second)
	if r.Failures >= 3 {
		r.Paused = true
		s.Error = "连续三次未取得有效审核，新稿生成已暂停；已审核声音继续播放。请处理审核提示后重新开始本场。"
		if kind == "uncertain" {
			s.Error = "审核连续无法确定，请核对事实资料后重新开始；已审核声音继续播放。"
		}
		s.Warn(s.Error)
		return
	}
	s.Warn("本次稿件未采纳，保留角度与配额：" + reason)
	// Keep the pending product, point, public/product slot, and phase frozen.
	// Another angle of the same point may supply this slot; opening, closing,
	// and promised continuations stay on their task rather than advancing a cycle.
	if s.Config.Rolling && s.Phase == "body" && s.PriceHook == nil {
		for offset := 1; offset < len(s.Queue); offset++ {
			i := (s.TaskIndex + offset) % len(s.Queue)
			t := s.Queue[i]
			if t.Common == task.Common && t.SellingPoint == task.SellingPoint && s.productTaskEligible(t) && !s.P0SkippedKeys[t.Key] && !s.DuplicateSkippedKeys[t.Key] && !now.Before(r.Cooling[t.Key]) {
				s.TaskIndex = i
				return
			}
		}
		// A one-angle point waits for its temporary cooldown, never loses a slot.
		r.RetryAfter = r.Cooling[task.Key]
	}
}
