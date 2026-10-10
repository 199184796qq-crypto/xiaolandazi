package model

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AnchorRecommendation struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	Introduction   string          `json:"introduction"`
	Summary        string          `json:"summary"`
	AvatarType     string          `json:"avatar_type"`
	AvatarURL      string          `json:"avatar_url,omitempty"`
	VersionNo      int64           `json:"version_no"`
	Status         string          `json:"status"`
	SourceTenantID int64           `json:"source_tenant_id,omitempty"`
	SourceStyleID  int64           `json:"source_style_id,omitempty"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Snapshot       LiveAnchorStyle `json:"-"`
}

type AnchorRecommendationSource struct {
	ID          int64  `json:"id"`
	TenantID    int64  `json:"tenant_id"`
	TenantName  string `json:"tenant_name"`
	Name        string `json:"name"`
	Description string `json:"description"`
	AvatarType  string `json:"avatar_type"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	HasStyle    bool   `json:"has_style"`
}

func HasReusableAnchorProfile(p LiveAgentPlanAnchorStyleProfile) bool {
	return len(p.Dimensions) > 0 || len(p.ReusableRules) > 0 || (p.Delivery != nil && strings.TrimSpace(p.Delivery.MetaPromptMD) != "")
}

// Exclude identifiers, counters and sample/training records from the portable
// version. Later source edits never overwrite published or imported copies.
func AnchorRecommendationSHA(s LiveAnchorStyle) string {
	plugins := make([]map[string]any, 0, len(s.PluginSettings))
	for _, p := range s.PluginSettings {
		if p.Parameters == nil {
			p.Parameters = map[string]any{}
		}
		plugins = append(plugins, map[string]any{"id": p.PluginID, "version": p.PluginVersion, "enabled": p.Enabled, "parameters": p.Parameters})
	}
	raw, _ := json.Marshal(map[string]any{"name": s.Name, "description": s.Description, "avatar_type": s.AvatarType, "avatar": s.AvatarURL, "profile": s.Profile, "plugins": plugins})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
