package anchorcycle

import (
	"errors"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/contentguard"
)

func (s *State) scanP0(p *contentguard.Policy, text string) contentguard.Report {
	report := p.Scan(text, s.Config.ComplianceScope)
	if report.Passed || len(s.P0Allowances) == 0 {
		return report
	}
	allowed := map[string]bool{}
	for _, item := range s.P0Allowances {
		key := strings.TrimSpace(item.MatchKey)
		if key == "" {
			key = p.MatchKey(item.MatchedText)
		}
		if item.RuleID != "" && key != "" {
			allowed[item.RuleID+"\x00"+key] = true
		}
	}
	blocked := report.Hits[:0]
	for _, hit := range report.Hits {
		if allowed[hit.RuleID+"\x00"+p.MatchKey(hit.Text)] {
			continue
		}
		blocked = append(blocked, hit)
	}
	report.Hits = blocked
	report.Passed = report.Error == "" && len(blocked) == 0
	return report
}

type P0Skip struct {
	Task      Task        `json:"task"`
	Phase     string      `json:"phase"`
	Cycle     int         `json:"cycle_number"`
	Attempts  []Candidate `json:"attempts"`
	Reason    string      `json:"reason"`
	CreatedAt string      `json:"created_at"`
}

func (s *State) InstallP0(md, source string) error {
	p, err := contentguard.Compile(md)
	if err != nil {
		return err
	}
	if s.Config.ComplianceScope == "" {
		s.Config.ComplianceScope = p.Data.Execution.DefaultScope
	}
	if s.Config.ComplianceScope != p.Data.Execution.DefaultScope {
		return errors.New("P0审核范围由服务端母版决定，不能通过请求降低")
	}
	s.P0PolicyMD, s.P0PolicySource, s.P0MaxRewrites = md, source, p.Data.Execution.MaxRewrites
	return nil
}

// A completed text review follows the unchanged candidate through adoption.
// Audio consumers verify this receipt's identity without scanning content again.
func (s *State) candidateP0Input(c Candidate) string {
	return ProductDigest(struct {
		Policy, Scope, Raw, Output, PatchError string
		Allowances                             []P0Allowance
	}{s.P0PolicyMD, s.Config.ComplianceScope, c.Text, c.OutputText, c.P0PatchError, s.P0Allowances})
}
func (s *State) CheckP0Candidate(c *Candidate) error {
	if s.P0PolicyMD == "" {
		return nil
	} // Isolated legacy unit fixtures only.
	identity := s.candidateP0Input(*c)
	if c.P0Report != nil && c.P0CheckedInputSHA == identity {
		return nil
	}
	p, err := contentguard.Compile(s.P0PolicyMD)
	if err != nil {
		return err
	}
	r := s.scanP0(p, spokenCandidate(*c))
	if c.P0PatchError != "" {
		r.Passed = false
		r.Error = c.P0PatchError
	}
	c.P0Report = &r
	c.P0CheckedInputSHA = identity
	c.P0ApprovedTextSHA = ""
	if r.Passed {
		c.P0ApprovedTextSHA = ProductDigest(spokenCandidate(*c))
	}
	return nil
}
func (s *State) CheckApprovedText(c Candidate, text string) error {
	if c.P0Report == nil || !c.P0Report.Passed || c.P0ApprovedTextSHA == "" || c.P0ApprovedTextSHA != ProductDigest(text) || c.P0CheckedInputSHA != s.candidateP0Input(c) {
		return errors.New("文字审核回执与当前话术不一致，请回文字阶段审核")
	}
	return nil
}

// The final text receipt is reused by adoption and audio. A contextual pass is
// scoped to this exact candidate, never a global allowance for a word like 最.
func (s *State) ApplyP0Context(c *Candidate, review contentguard.ContextReview) error {
	if c.P0Report == nil || c.P0CheckedInputSHA != s.candidateP0Input(*c) {
		return errors.New("语境复核候选正文或规则已经变化")
	}
	r, err := contentguard.ResolveContext(*c.P0Report, spokenCandidate(*c), review)
	if err != nil {
		return err
	}
	c.P0Report = &r
	c.P0ApprovedTextSHA = ""
	if r.Passed {
		c.P0ApprovedTextSHA = ProductDigest(spokenCandidate(*c))
	}
	return nil
}

// Production content protocol: one paragraph, one verdict, program-owned IDs.
func (s *State) ApplyP0ContextContent(c *Candidate, review contentguard.ContextReview) error {
	if c.P0Report == nil || c.P0CheckedInputSHA != s.candidateP0Input(*c) {
		return errors.New("语境复核候选正文或规则已经变化")
	}
	r, err := contentguard.ResolveContextContent(*c.P0Report, spokenCandidate(*c), review)
	if err != nil {
		return err
	}
	c.P0Report = &r
	c.P0ApprovedTextSHA = ""
	if r.Passed {
		c.P0ApprovedTextSHA = ProductDigest(spokenCandidate(*c))
	}
	return nil
}

func (s *State) CheckP0Voice(text string) error {
	if s.P0PolicyMD == "" {
		return errors.New("本场未加载P0规则，不能输出声音，请开始新场")
	}
	p, err := contentguard.Compile(s.P0PolicyMD)
	if err != nil {
		return err
	}
	r := s.scanP0(p, text)
	if !r.Passed {
		return errors.New("声音正文P0检查未通过，不调用TTS或复用声音")
	}
	return nil
}

// deleteRepair 程序删词兜底：删除命中的违规词，删后文本非空且再过 P0 通过才返回修复结果。
// 用于「局部重写耗尽后仍违规」时，替代直接跳过，把违规词直接删掉保住这段话。
func (s *State) deleteRepair(c Candidate) (string, bool) {
	if c.P0Report == nil || len(c.P0Report.Hits) == 0 || s.P0PolicyMD == "" {
		return "", false
	}
	text := spokenCandidate(c)
	runes := []rune(text)
	hits := append([]contentguard.Hit{}, c.P0Report.Hits...)
	sort.Slice(hits, func(i, j int) bool { return hits[i].Start > hits[j].Start })
	for _, h := range hits {
		if h.Start < 0 || h.End > len(runes) || h.Start >= h.End {
			continue
		}
		runes = append(runes[:h.Start], runes[h.End:]...)
	}
	repaired := cleanAfterDelete(string(runes))
	if repaired == "" {
		return "", false
	}
	p, err := contentguard.Compile(s.P0PolicyMD)
	if err != nil {
		return "", false
	}
	if r := s.scanP0(p, repaired); !r.Passed {
		return "", false
	}
	return repaired, true
}

// cleanAfterDelete 清理删词后残留的首部标点/空格。
func cleanAfterDelete(s string) string {
	runes := []rune(strings.TrimSpace(s))
	cut := 0
	for _, r := range runes {
		if strings.ContainsRune("，。！？、；： \t\n", r) {
			cut++
			continue
		}
		break
	}
	if cut > 0 {
		runes = runes[cut:]
	}
	return strings.TrimSpace(string(runes))
}

func (s *State) rejectP0Candidate(c Candidate) {
	c.ContentIssue = "p0_blocked"
	s.Attempts = append(s.Attempts, c)
	if s.P0RewritesUsed < s.P0MaxRewrites {
		r := contentguard.PlanRepair(c.Text, *c.P0Report, s.P0RewritesUsed+1)
		s.P0PendingRepair = &r
		return
	}
	task := s.Task()
	s.P0Skipped = append(s.P0Skipped, P0Skip{Task: task, Phase: s.Phase, Cycle: s.CycleNumber, Attempts: append([]Candidate{}, s.Attempts...), Reason: "p0_rewrite_exhausted", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if s.P0SkippedKeys == nil {
		s.P0SkippedKeys = map[string]bool{}
	}
	if s.PriceHook != nil {
		s.P0SkippedKeys[s.PriceHook.Task.Key] = true
		s.Warn("P0优先，未完成的塑价承接已记录跳过，不伪装成已兑现")
	} else if s.Phase != "closing" {
		s.P0SkippedKeys[task.Key] = true
	}
	s.PriceHook = nil
	s.P0PendingRepair = nil
	s.PendingOpeningHook = nil
	s.Attempts = []Candidate{}
	s.P0RewritesUsed = 0
	s.P0ConsecutiveSkips++
	s.PendingFeedback = ""
	s.FeedbackPending = false
	p, _ := contentguard.Compile(s.P0PolicyMD)
	if s.P0ConsecutiveSkips >= p.Data.Execution.MaxConsecutiveSkips && s.Surface != "formal_live" {
		s.Status = "compliance_blocked"
		s.Error = "连续P0跳过已达到母版保护上限，保留已采纳稿并暂停"
		return
	}
	// Skip is a scheduler decision, not an adopted paragraph: no body quota,
	// Use count, hook-ID memory or macro-window change.
	s.Cursor++
	if s.Phase == "closing" {
		if !s.Config.Rolling || (s.Config.MaxCycles > 0 && s.CycleNumber >= s.Config.MaxCycles) {
			s.Phase = "ended"
			s.Status = "completed"
			return
		}
		s.CycleNumber++
		s.BodyCount = 0
		s.planBodyTarget()
		s.Phase = "opening"
	}
	if s.Config.Rolling {
		s.chooseNext()
		return
	}
	for s.Cursor < len(s.Queue) && s.P0SkippedKeys[s.Queue[s.Cursor].Key] {
		s.Cursor++
	}
	if s.Cursor >= len(s.Queue) {
		s.Phase = "closing"
		return
	}
	for _, prior := range s.Accepted[max(0, len(s.Accepted)-s.Config.PointGap):] {
		if prior.Phase != "closing" && prior.Task.SellingPoint == s.Queue[s.Cursor].SellingPoint {
			s.Status = "spacing_blocked"
			s.Error = "P0跳过后固定队列无法满足卖点避重间隔，已暂停"
			return
		}
	}
}

// rejectReviewCandidate 最终复核判违规时的局部重写，复用 P0 的重写链路（P0PendingRepair）。
// 最终复核只有 reason 文字、无词级命中，交给重写模型按 reason 定位并只改违规句段。
// 重写次数与 P0 共用 P0RewritesUsed/P0MaxRewrites 预算，耗尽后由调用方跳过。
func (s *State) rejectReviewCandidate(c Candidate, reason string) {
	c.ContentIssue = "final_review_blocked"
	s.Attempts = append(s.Attempts, c)
	r := contentguard.PlanReviewRepair(c.Text, reason, s.P0RewritesUsed+1)
	s.P0PendingRepair = &r
}

func (s *State) P0RulesJSON() (string, error) {
	p, err := contentguard.Compile(s.P0PolicyMD)
	if err != nil {
		return "", err
	}
	return p.JSON(), nil
}
