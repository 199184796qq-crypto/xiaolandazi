package anchorcycle

// PlaybackReceipt 是 Core 回传的播放回执，用于确认某段音频是否真的播完。
// 讲解业务已下线，但播放回执是与 Core 的通用协议字段，仍需保留。
type PlaybackReceipt struct {
	Sequence uint64 `json:"sequence"`
	TrackID  string `json:"track_id"`
	PlayedMS int64  `json:"played_ms"`
	Complete bool   `json:"complete"`
	Terminal bool   `json:"terminal"`
}

// ExpressionAudit 是「用户表达约束」复核回执：只记录这次约束审核是否通过、
// 命中了哪句、以及回执与文本/约束的绑定摘要，用来证明同一份文本确实审过。
// 讲解业务下线后仅保留表达约束这一路（原 LectureAudit 的讲解字段已删除）。
type ExpressionAudit struct {
	Passed       bool   `json:"passed"`
	Quote        string `json:"quote"`
	Reason       string `json:"reason"`
	Error        string `json:"error,omitempty"`
	TextSHA      string `json:"text_sha"`
	ScopeSHA     string `json:"scope_sha"`
	Model        string `json:"model,omitempty"`
	PromptSHA    string `json:"prompt_sha256,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
}

// ExpressionCandidateApproved 判断候选是否满足本场生效的用户表达约束：
// 必须有通过的回执，且回执绑定的文本与约束摘要都和当前候选一致。
func (s *State) ExpressionCandidateApproved(c Candidate) bool {
	if s.Config.SkipReview {
		return true
	}
	constraints := s.EffectiveExpressionConstraints()
	if len(constraints) == 0 {
		return true
	}
	a := c.ExpressionAudit
	if a == nil || !a.Passed || a.Error != "" {
		return false
	}
	return a.TextSHA == ProductDigest(spokenCandidate(c)) && a.ScopeSHA == ProductDigest(constraints)
}
