package model

import (
	"errors"
	"fmt"
	"time"
)

const (
	LiveContentAIPregenerated = "ai_pregenerated"
	LiveContentUserAudio      = "user_audio"
	LiveContentAIDynamic      = "ai_dynamic"
)

// LiveContentPolicy is controlled by system administrators and customer-authorized
// operators. Customer content drafts never override this execution policy.
type LiveContentPolicy struct {
	ContentMode          string `json:"content_mode"`
	AutoRefreshEnabled   bool   `json:"auto_refresh_enabled"`
	MainlineTTLSeconds   int    `json:"mainline_ttl_seconds"`
	FAQTTLSeconds        int    `json:"faq_ttl_seconds"`
	RefreshAheadSeconds  int    `json:"refresh_ahead_seconds"`
	ReplacementPercent   int    `json:"replacement_percent"`
	FAQVariantCount      int    `json:"faq_variant_count"`
	MinRepeatSeconds     int    `json:"min_repeat_seconds"`
	MinimumBufferSeconds int    `json:"minimum_buffer_seconds"`
}

type LiveContentPolicyRecord struct {
	Policy            LiveContentPolicy `json:"policy"`
	Overridden        bool              `json:"overridden"`
	Revision          int64             `json:"revision"`
	SystemRevision    int64             `json:"system_revision"`
	UpdatedAt         *time.Time        `json:"updated_at,omitempty"`
	DynamicAuthorized bool              `json:"dynamic_authorized"`
	RefreshStatus     map[string]any    `json:"refresh_status,omitempty"`
}

type LiveContentModeAccess struct {
	ContentMode         string   `json:"content_mode"`
	AvailableModes      []string `json:"available_modes"`
	DynamicAuthorized   bool     `json:"dynamic_authorized"`
	AuthorizationSource string   `json:"authorization_source"`
}

// Keep entitlement resolution separate from mode preference. Membership grants
// can be added here later without changing customer request payloads.
func ResolveLiveContentMode(selected string, dynamicAllowed bool, source string) LiveContentModeAccess {
	result := LiveContentModeAccess{ContentMode: selected, AvailableModes: []string{LiveContentAIPregenerated, LiveContentUserAudio}, DynamicAuthorized: dynamicAllowed, AuthorizationSource: "default"}
	if dynamicAllowed {
		result.AvailableModes = append(result.AvailableModes, LiveContentAIDynamic)
		result.AuthorizationSource = source
	}
	if selected != LiveContentUserAudio && selected != LiveContentAIPregenerated && !(selected == LiveContentAIDynamic && dynamicAllowed) {
		result.ContentMode = LiveContentAIPregenerated
	}
	return result
}

func DefaultLiveContentPolicy() LiveContentPolicy {
	return LiveContentPolicy{ContentMode: LiveContentAIPregenerated, AutoRefreshEnabled: true, MainlineTTLSeconds: 7200,
		FAQTTLSeconds: 7200, RefreshAheadSeconds: 600, ReplacementPercent: 25,
		FAQVariantCount: 3, MinRepeatSeconds: 1200, MinimumBufferSeconds: 1800}
}

func (p LiveContentPolicy) Validate() error {
	switch p.ContentMode {
	case LiveContentAIPregenerated, LiveContentUserAudio, LiveContentAIDynamic:
	default:
		return errors.New("请选择有效的直播内容模式")
	}
	for _, field := range []struct {
		name            string
		value, min, max int
	}{
		{"主线生命周期", p.MainlineTTLSeconds, 600, 86400},
		{"常见问题生命周期", p.FAQTTLSeconds, 300, 86400},
		{"提前更新时间", p.RefreshAheadSeconds, 60, 21600},
		{"每批替换比例", p.ReplacementPercent, 1, 100},
		{"问答版本数", p.FAQVariantCount, 1, 10},
		{"相同回答最短间隔", p.MinRepeatSeconds, 0, 86400},
		{"最低音频储备", p.MinimumBufferSeconds, 60, 14400},
	} {
		if field.value < field.min || field.value > field.max {
			return fmt.Errorf("%s必须在%d至%d之间", field.name, field.min, field.max)
		}
	}
	if p.RefreshAheadSeconds >= p.FAQTTLSeconds || p.RefreshAheadSeconds >= p.MainlineTTLSeconds {
		return errors.New("提前更新时间必须短于主线及常见问题生命周期")
	}
	if p.MinRepeatSeconds >= p.FAQTTLSeconds {
		return errors.New("重复间隔必须短于常见问题生命周期")
	}
	return nil
}

// ContentExpiry defines deadlines shared by the refresh worker and FAQ cache.
func (p LiveContentPolicy) ContentExpiry(kind string, generatedAt time.Time) (refreshAt, expiresAt time.Time) {
	ttl := p.FAQTTLSeconds
	if kind == "mainline" {
		if p.ContentMode == LiveContentUserAudio {
			return time.Time{}, time.Time{}
		}
		ttl = p.MainlineTTLSeconds
	}
	expiresAt = generatedAt.Add(time.Duration(ttl) * time.Second)
	refreshAt = expiresAt.Add(-time.Duration(p.RefreshAheadSeconds) * time.Second)
	return
}
