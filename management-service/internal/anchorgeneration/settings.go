package anchorgeneration

import (
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"strings"
)

// Settings stores references to encrypted model profiles, never API keys.
type Settings struct {
	InteractionProfileID  string             `json:"interaction_profile_id,omitempty"`
	MainlineBufferSeconds int                `json:"mainline_buffer_seconds"`
	SafetyProfileID       string             `json:"safety_profile_id"`
	StyleProfileID        string             `json:"style_profile_id"`
	ScorecardProfileID    string             `json:"scorecard_profile_id"`
	SellingPointProfileID string             `json:"selling_point_profile_id"`
	Cycle                 anchorcycle.Config `json:"cycle"`
}

func Default() Settings {
	return Settings{MainlineBufferSeconds: 120, Cycle: anchorcycle.Config{Region: "cn-beijing", Threshold: 40, MaxRetries: 3, MaxChars: 300, PointGap: 2, CombinationGap: 8, Rolling: true, RepairPunctuation: true, BodyMin: 2, BodyMax: 6}}
}

// The feedback transport follows the mainline text connection. Legacy Agent
// runs use the interaction connection, then their existing credential connection.
// Keep FeedbackID in the wire format for old clients and saved snapshots only.
func (s *Settings) NormalizeSharedFeedback() {
	switch {
	case strings.TrimSpace(s.Cycle.TextProfileID) != "":
		s.Cycle.FeedbackID = s.Cycle.TextProfileID
	case strings.TrimSpace(s.InteractionProfileID) != "":
		s.Cycle.FeedbackID = s.InteractionProfileID
	case strings.TrimSpace(s.Cycle.CredentialID) != "":
		s.Cycle.FeedbackID = s.Cycle.CredentialID
	}
}

// Unified connections own model selection; historical role fields remain wire
// compatible but cannot override the operator's two central choices.
func (s *Settings) ApplyModelRoles(systemID, speechID string) {
	if systemID == "" || speechID == "" {
		return
	}
	s.StyleProfileID, s.ScorecardProfileID, s.SellingPointProfileID, s.SafetyProfileID = systemID, systemID, systemID, systemID
	s.InteractionProfileID, s.Cycle.TextProfileID, s.Cycle.FeedbackID = speechID, speechID, speechID
}

func (s Settings) Validate() error {
	s.NormalizeSharedFeedback()
	if s.MainlineBufferSeconds < 20 || s.MainlineBufferSeconds > 600 {
		return errors.New("主线缓存时长须为20～600秒")
	}
	generatorID := s.Cycle.TextProfileID
	if generatorID == "" {
		generatorID = s.Cycle.CredentialID
	}
	for _, id := range []string{s.StyleProfileID, s.ScorecardProfileID, s.SellingPointProfileID, generatorID, s.Cycle.FeedbackID} {
		if strings.TrimSpace(id) == "" || len(id) > 128 {
			return errors.New("请配置全部模型角色对应的有效模型配置")
		}
	}
	if s.Cycle.TextProfileID == "" && (strings.TrimSpace(s.Cycle.WorkspaceID) == "" || strings.TrimSpace(s.Cycle.AgentID) == "") {
		return errors.New("请配置阿里 Workspace 与母版 Agent")
	}
	for _, id := range []string{s.Cycle.WorkspaceID, s.Cycle.AgentID, s.Cycle.EnvironmentID, s.Cycle.Region} {
		if len(id) > 160 {
			return errors.New("阿里资源 ID 太长")
		}
	}
	if s.Cycle.TTS != nil {
		return errors.New("声音使用方案已选声音，系统设置不保存房间声音")
	}
	if s.Cycle.Threshold < 0 || s.Cycle.Threshold > 100 || s.Cycle.MaxRetries < 0 || s.Cycle.MaxRetries > 3 || s.Cycle.MaxChars < 80 || s.Cycle.MaxChars > 1000 || s.Cycle.BodyMin < 2 || s.Cycle.BodyMax < s.Cycle.BodyMin || s.Cycle.BodyMax > 6 || s.Cycle.PointGap < 0 || s.Cycle.PointGap > 100 || s.Cycle.CombinationGap < 0 || s.Cycle.CombinationGap > 100 || s.Cycle.MaxCycles < 0 || s.Cycle.MaxCycles > 10000 {
		return errors.New("生成调度参数不在允许范围内")
	}
	return nil
}
