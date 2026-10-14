package httpapi

import (
	"livecompanion/management/internal/model"
	"sort"
	"strings"
	"time"
)

type sessionDownloadItem struct {
	model.SessionSpeechAsset
	Sequence int                        `json:"sequence,omitempty"`
	Parts    []model.SessionSpeechAsset `json:"-"`
}

type sessionDownloadPair struct {
	tenant, room, session int64
	decision              string
}

func sessionDownloadKey(a model.SessionSpeechAsset, prefix string) (sessionDownloadPair, bool) {
	decision, ok := strings.CutPrefix(a.SourceRef, prefix)
	return sessionDownloadPair{a.TenantID, a.RoomID, a.SessionID, decision}, ok && decision != ""
}

// Group immutable playback history before filtering or pagination. A bridge is
// attached only to its own played answer, never to the nearest list entry.
func sessionDownloadItems(assets []model.SessionSpeechAsset) []sessionDownloadItem {
	ordered := append([]model.SessionSpeechAsset(nil), assets...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.PlayedAt == nil {
			return false
		}
		if b.PlayedAt == nil {
			return true
		}
		if a.PlayedAt.Equal(*b.PlayedAt) {
			return a.ID < b.ID
		}
		return a.PlayedAt.Before(*b.PlayedAt)
	})
	bridges := map[sessionDownloadPair]model.SessionSpeechAsset{}
	for _, a := range ordered {
		if a.Kind == "bridge" && a.PlayedAt != nil {
			if key, ok := sessionDownloadKey(a, "bridge:"); ok {
				bridges[key] = a
			}
		}
	}
	items := make([]sessionDownloadItem, 0, len(ordered))
	sequences := map[sessionDownloadPair]int{}
	for _, a := range ordered {
		if a.PlayedAt == nil || a.Kind == "bridge" {
			continue
		}
		item := sessionDownloadItem{SessionSpeechAsset: a, Parts: []model.SessionSpeechAsset{a}}
		item.Text = strings.TrimSpace(a.Text)
		if a.Kind == "mainline" {
			key := sessionDownloadPair{tenant: a.TenantID, room: a.RoomID, session: a.SessionID}
			sequences[key]++
			item.Sequence = sequences[key]
		}
		if key, ok := sessionDownloadKey(a, "reply:"); a.Kind == "interaction" && ok {
			if bridge, exists := bridges[key]; exists {
				item.Parts = append(item.Parts, bridge)
				if text := strings.TrimSpace(bridge.Text); text != "" {
					if item.Text != "" {
						item.Text += "\n"
					}
					item.Text += text
				}
				item.DurationMS += bridge.DurationMS
				item.Available = item.Available && bridge.Available
				if bridge.ExpiresAt != nil && (item.ExpiresAt == nil || bridge.ExpiresAt.Before(*item.ExpiresAt)) {
					item.ExpiresAt = bridge.ExpiresAt
				}
			}
		}
		items = append(items, item)
	}
	return items
}

func filterSessionDownloadItems(items []sessionDownloadItem, from, to *time.Time) []sessionDownloadItem {
	filtered := make([]sessionDownloadItem, 0, len(items))
	for _, item := range items {
		if from != nil && item.PlayedAt.Before(*from) || to != nil && !item.PlayedAt.Before(*to) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
