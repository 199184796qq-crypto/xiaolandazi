package anchorcycle

import (
	"errors"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/contentguard"
)

// BaseP0Allowances 是公共的安全语境排除项：只放行“第一”作为时间/序数
// 的普通用法，不放行“第一品牌、销量第一、全网第一”等宣传用法。
var BaseP0Allowances = []P0Allowance{
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一天", Decision: "基础排除：时间/序数用法"},
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一天开", Decision: "基础排除：首播时间"},
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一次", Decision: "基础排除：序数用法"},
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一口", Decision: "基础排除：序数用法"},
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一单", Decision: "基础排除：序数用法"},
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一轮", Decision: "基础排除：轮次用法"},
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一场", Decision: "基础排除：场次用法"},
	{RuleID: "l1.p0.absolute_claim", MatchedText: "第一步", Decision: "基础排除：步骤用法"},
}

func p0AllowanceMatches(item P0Allowance, hit contentguard.Hit, p *contentguard.Policy, original []rune) bool {
	key := strings.TrimSpace(item.MatchKey)
	if key == "" {
		key = p.MatchKey(item.MatchedText)
	}
	if key == "" {
		return false
	}
	if key == p.MatchKey(hit.Text) {
		return true
	}
	// P0 命中“第一”时，排除词可能是“第一天”等更长的上下文短语。
	// 必须从命中起点开始，整段完全等于排除词；不能只看附近是否包含。
	phrase := []rune(item.MatchedText)
	if len(phrase) == 0 || hit.Start < 0 || hit.Start+len(phrase) > len(original) {
		return false
	}
	if hit.End > hit.Start+len(phrase) {
		return false
	}
	return p.MatchKey(string(original[hit.Start:hit.Start+len(phrase)])) == key
}

func (s *State) scanP0(p *contentguard.Policy, text string) contentguard.Report {
	report := p.Scan(text, s.Config.ComplianceScope)
	if report.Passed {
		return report
	}
	allowances := append(append([]P0Allowance{}, BaseP0Allowances...), s.P0Allowances...)
	original := []rune(text)
	blocked := report.Hits[:0]
	for _, hit := range report.Hits {
		allowed := false
		for _, item := range allowances {
			if item.RuleID == hit.RuleID && p0AllowanceMatches(item, hit, p, original) {
				allowed = true
				break
			}
		}
		if !allowed {
			blocked = append(blocked, hit)
		}
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
	if s.Config.SkipReview {
		// 跳过审核开关：不扫描违规词，直接签发「通过」回执，让采纳与后续 TTS
		// 的 CheckApprovedText 门槛照常放行，全程零审核。
		s.passCandidateReview(c)
		return nil
	}
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

// passCandidateReview 跳过审核时给候选签发一份「通过」的空回执，兼容所有依赖
// P0Report.Passed / P0ApprovedTextSHA 的采纳与声音出口，无需改动下游调用点。
func (s *State) passCandidateReview(c *Candidate) {
	c.P0Report = &contentguard.Report{Passed: true, Hits: []contentguard.Hit{}}
	c.P0CheckedInputSHA = s.candidateP0Input(*c)
	c.P0ApprovedTextSHA = ProductDigest(spokenCandidate(*c))
}
func (s *State) CheckApprovedText(c Candidate, text string) error {
	if s.Config.SkipReview {
		return nil
	}
	if c.P0Report == nil || !c.P0Report.Passed || c.P0ApprovedTextSHA == "" || c.P0ApprovedTextSHA != ProductDigest(text) || c.P0CheckedInputSHA != s.candidateP0Input(c) {
		return errors.New("文字审核回执与当前话术不一致，请回文字阶段审核")
	}
	return nil
}

// The final text receipt is reused by adoption and audio. P0 语境复核已下线：
// semantic 命中的放行只靠 P0Allowances（用户已确认的风险卡），不再调模型判断。
func (s *State) CheckP0Voice(text string) error {
	if s.Config.SkipReview {
		return nil
	}
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
	task := s.Task()
	s.P0Skipped = append(s.P0Skipped, P0Skip{Task: task, Phase: s.Phase, Cycle: s.CycleNumber, Attempts: append([]Candidate{}, s.Attempts...), Reason: "p0_blocked", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
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

func (s *State) P0RulesJSON() (string, error) {
	p, err := contentguard.Compile(s.P0PolicyMD)
	if err != nil {
		return "", err
	}
	return p.JSON(), nil
}
