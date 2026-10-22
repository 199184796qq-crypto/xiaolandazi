package httpapi

import (
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/recordingcache"
)

// recordingAssetIDFromTrack 从 track id（recording-<seq>-block-<n>-<assetID>）
// 取出归档资产 id。
func recordingAssetIDFromTrack(trackID string) int64 {
	id := strings.TrimSpace(trackID)
	if id == "" {
		return 0
	}
	parts := strings.Split(id, "-")
	value, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// recordingCacheEntryFromUnits 把一句源文本合成出的全部单元转成缓存记录。
// 润色可能把一句拆成多句，这时按 parts 保存，命中后整组还原。
func recordingCacheEntryFromUnits(source string, units []recordingReadyUnit) (recordingcache.Entry, bool) {
	if len(units) == 0 {
		return recordingcache.Entry{}, false
	}
	if len(units) == 1 {
		return recordingCacheEntryFromUnit(source, units[0])
	}
	parts := make([]recordingcache.Part, 0, len(units))
	for _, unit := range units {
		assetID := recordingAssetIDFromTrack(unit.Track.ID)
		if assetID <= 0 || len(unit.Track.Timeline) == 0 {
			return recordingcache.Entry{}, false
		}
		segments := make([]recordingcache.Segment, 0, len(unit.Track.Timeline))
		for _, segment := range unit.Track.Timeline {
			segments = append(segments, recordingcache.Segment{
				Text: segment.Text, StartMS: int64(segment.StartMS),
				EndMS: int64(segment.EndMS), SafeCut: segment.SafeCut,
			})
		}
		parts = append(parts, recordingcache.Part{
			Spoken: unit.Text, AssetID: assetID,
			Duration: int64(unit.Track.DurationMS), Segments: segments,
		})
	}
	return recordingcache.Entry{
		Sentence: source, Spoken: units[0].Text, AssetID: parts[0].AssetID,
		Duration: parts[0].Duration, Segments: parts[0].Segments,
		Parts: parts, Polished: true, UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}, true
}

// recordingCacheEntryFromUnit 把刚合成好的单元转成缓存记录。
// 存的是实际送 TTS 的正文和真实时间轴，命中时才能原样还原。
func recordingCacheEntryFromUnit(source string, unit recordingReadyUnit) (recordingcache.Entry, bool) {
	assetID := recordingAssetIDFromTrack(unit.Track.ID)
	if assetID <= 0 || len(unit.Track.Timeline) == 0 {
		return recordingcache.Entry{}, false
	}
	segments := make([]recordingcache.Segment, 0, len(unit.Track.Timeline))
	for _, segment := range unit.Track.Timeline {
		segments = append(segments, recordingcache.Segment{
			Text:    segment.Text,
			StartMS: int64(segment.StartMS),
			EndMS:   int64(segment.EndMS),
			SafeCut: segment.SafeCut,
		})
	}
	return recordingcache.Entry{
		Sentence:  source,
		Spoken:    unit.Text,
		AssetID:   assetID,
		Duration:  int64(unit.Track.DurationMS),
		Segments:  segments,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}, true
}
