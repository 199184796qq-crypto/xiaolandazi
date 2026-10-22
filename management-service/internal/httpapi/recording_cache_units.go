package httpapi

import (
	"context"
	"fmt"
	"log"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

// rebuildRecordingUnitFromCache 把缓存清单里的一条记录还原成 Core 能接受的
// track。还原出来的时间轴与安全切点必须与现场合成时逐字段一致，否则 Core
// 会以“切点不在语义句界上”拒绝整条 track。
func (s *Server) rebuildRecordingUnitFromCache(ctx context.Context, tenant int64, entry recordingcache.Entry) (recordingReadyUnit, bool) {
	if entry.AssetID <= 0 || len(entry.Segments) == 0 {
		return recordingReadyUnit{}, false
	}
	url, err := s.recordingStreamAssetURL(ctx, tenant, entry.AssetID)
	if err != nil || url == "" {
		return recordingReadyUnit{}, false
	}
	timeline := make([]model.LiveAgentPlanTimelineSegment, 0, len(entry.Segments))
	for index, segment := range entry.Segments {
		timeline = append(timeline, model.LiveAgentPlanTimelineSegment{
			SegmentID: fmt.Sprintf("cache-%d-%03d", entry.AssetID, index+1),
			Index:     index + 1,
			StartMS:   segment.StartMS,
			EndMS:     segment.EndMS,
			Text:      segment.Text,
			SafeCut:   segment.SafeCut,
		})
	}
	track := coreclient.ProgramRefreshTrack{
		ID:         fmt.Sprintf("recording-cache-%d", entry.AssetID),
		Label:      "录音AI声音",
		Text:       entry.Spoken,
		AudioURL:   url,
		DurationMS: int(entry.Duration),
		Timeline:   recordingStreamTimeline(timeline),
		SafePoints: recordingStreamSafePoints(buildFullShowSafePoints(timeline)),
	}
	return recordingReadyUnit{Text: entry.Spoken, Track: track}, true
}

// recordingCacheLookup 把清单转成“源句 → 记录”的索引，一次开播只读一次盘。
func recordingCacheLookup(manifest *recordingcache.Manifest) map[string]recordingcache.Entry {
	hit := make(map[string]recordingcache.Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.AssetID > 0 && len(entry.Segments) > 0 {
			hit[entry.Sentence] = entry
		}
	}
	return hit
}

// recordingCacheScopeFor 推导本次开播的缓存目录。
func recordingCacheScopeFor(tenant, room, plan int64) (recordingcache.Scope, bool) {
	scope, err := recordingcache.ScopeFor(recordingCacheMediaRoot(), tenant, room, plan)
	if err != nil {
		log.Printf("recording cache scope invalid tenant=%d room=%d plan=%d: %v", tenant, room, plan, err)
		return recordingcache.Scope{}, false
	}
	return scope, true
}
