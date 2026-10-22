package httpapi

import (
	"context"
	"log"

	"livecompanion/management/internal/recordingcache"
)

// purgeRecordingCacheScope 清理一个范围内的音频缓存：
// 先按索引取出要删的文件，删盘，再把索引行软删。
//
// 调用方必须先确认该房间没有正在使用这份缓存的直播场次（status 为
// running/paused 的 live_runtime_sessions）。这个函数不替调用方做
// 活跃场次判断，避免把“能不能删”的业务规则埋进底层。
func (s *Server) purgeRecordingCacheScope(ctx context.Context, mediaRoot string, tenantID, roomID, planID int64, voiceKey, scriptHash string) (recordingcache.PurgeResult, error) {
	var result recordingcache.PurgeResult
	if tenantID <= 0 {
		return result, nil
	}
	entries, err := s.store.ListMediaIndexByScope(ctx, tenantID, roomID, planID, voiceKey, scriptHash)
	if err != nil {
		return result, err
	}
	if len(entries) == 0 {
		return result, nil
	}

	files := make([]recordingcache.MediaEntry, 0, len(entries))
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		files = append(files, recordingcache.MediaEntry{
			AssetID:   entry.AssetID,
			ObjectKey: entry.ObjectKey,
			SizeBytes: entry.SizeBytes,
		})
		ids = append(ids, entry.ID)
	}

	result, err = recordingcache.PurgeFiles(mediaRoot, files)
	if err != nil {
		return result, err
	}
	if err := s.store.MarkMediaIndexDeleted(ctx, ids); err != nil {
		return result, err
	}
	log.Printf("recording cache purged tenant=%d room=%d plan=%d voice=%s script=%s removed=%d missing=%d bytes=%d",
		tenantID, roomID, planID, voiceKey, scriptHash, result.Removed, result.Missing, result.Bytes)
	return result, nil
}
