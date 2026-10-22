package httpapi

import (
	"context"
	"fmt"
	"log"
	"math/rand"

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

// rebuildRecordingUnitsFromCache 还原一句源文本对应的全部音频（可能被拆成多句）。
func (s *Server) rebuildRecordingUnitsFromCache(ctx context.Context, tenant int64, entry recordingcache.Entry) ([]recordingReadyUnit, bool) {
	if len(entry.Parts) > 0 {
		units := make([]recordingReadyUnit, 0, len(entry.Parts))
		for _, part := range entry.Parts {
			unit, ok := s.rebuildRecordingUnitFromCache(ctx, tenant, recordingcache.Entry{
				Sentence: entry.Sentence, Spoken: part.Spoken, AssetID: part.AssetID,
				Duration: part.Duration, Segments: part.Segments, Polished: true,
			})
			if !ok {
				return nil, false
			}
			units = append(units, unit)
		}
		return units, true
	}
	unit, ok := s.rebuildRecordingUnitFromCache(ctx, tenant, entry)
	if !ok {
		return nil, false
	}
	return []recordingReadyUnit{unit}, true
}

// recordingCacheLookup 把清单转成“源句 → 记录”的索引，一次开播只读一次盘。
// 缓存策略：每句最多保留 4 个版本；池未满时 40% 命中 / 60% 新生，
// 池满后 70% 命中 / 30% 替换；一个版本被命中 6 次即淘汰，让新版本补位。
const (
	recordingCacheMaxVersions = 4
	recordingCacheVersionHits = 6
	recordingCacheReuseWarm   = 40 // 池未满时的命中百分比
	recordingCacheReuseSteady = 70 // 池满后的命中百分比
)

// shouldReuseCachedVersion 按池内版本数决定本轮是复用还是新生。
func shouldReuseCachedVersion(versions int, rng *rand.Rand) bool {
	if versions <= 0 {
		return false
	}
	percent := recordingCacheReuseWarm
	if versions >= recordingCacheMaxVersions {
		percent = recordingCacheReuseSteady
	}
	return rng.Intn(100) < percent
}

// pruneRecordingCacheEntries 淘汰命中到期的版本，并把每句限制在 4 个版本内。
func pruneRecordingCacheEntries(entries []recordingcache.Entry) []recordingcache.Entry {
	perSentence := map[string]int{}
	out := make([]recordingcache.Entry, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Hits >= recordingCacheVersionHits {
			continue
		}
		if perSentence[entry.Sentence] >= recordingCacheMaxVersions {
			continue
		}
		perSentence[entry.Sentence]++
		out = append(out, entry)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// recordingCacheLookup 把清单转成「源句 → 全部版本」的索引。
// 同一句可以有多份润色版本，用来支撑四轮不重复。
func recordingCacheLookup(manifest *recordingcache.Manifest) map[string][]recordingcache.Entry {
	hit := make(map[string][]recordingcache.Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.AssetID > 0 && len(entry.Segments) > 0 && entry.Polished && entry.Hits < recordingCacheVersionHits {
			hit[entry.Sentence] = append(hit[entry.Sentence], entry)
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
