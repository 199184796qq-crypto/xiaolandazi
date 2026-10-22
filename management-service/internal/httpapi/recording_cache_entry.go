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
