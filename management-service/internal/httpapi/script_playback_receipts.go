package httpapi

import (
	"context"
	"fmt"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"log"
	"path/filepath"
	"strconv"
	"time"
)

type scriptDeliveredTrack struct {
	PolicyRecordedMS int64                  `json:"policy_recorded_ms,omitempty"`
	PolicyText       string                 `json:"policy_text,omitempty"`
	PolicyDurationMS int64                  `json:"policy_duration_ms,omitempty"`
	PolicyOffsetMS   int64                  `json:"policy_offset_ms,omitempty"`
	Unit             int                    `json:"unit"`
	TrackID          string                 `json:"track_id"`
	InstanceID       string                 `json:"instance_id"`
	AssetID          int64                  `json:"asset_id"`
	VoiceID          string                 `json:"voice_id"`
	Model            string                 `json:"model"`
	Batch            uint64                 `json:"batch,omitempty"`
	Completed        bool                   `json:"completed,omitempty"`
	TotalBlocks      int                    `json:"total_blocks,omitempty"`
	Block            scriptFirstCachedBlock `json:"block"`
	Confirmed        bool                   `json:"confirmed,omitempty"`
}

func scriptSupplyCheckpointPath(tenant, room, session int64) string {
	return filepath.Join(recordingCacheMediaRoot(), "volcengine-supply", fmt.Sprintf("%d-%d-%d.json", tenant, room, session))
}

func recoverScriptSupplyCheckpoint(tenant, room, session, version int64, snapshot coreclient.ProgramRefreshSnapshot) (string, bool) {
	if !snapshot.Running || snapshot.VersionID != version || snapshot.ProgramID == "" || snapshot.Task.SessionID != fmt.Sprintf("volcengine-long-%d", session) {
		return "", false
	}
	path := scriptSupplyCheckpointPath(tenant, room, session)
	var checkpoint volcengineScriptCursor
	if readVolcengineJSON(path, &checkpoint) != nil || checkpoint.VersionID != version {
		return "", false
	}
	if checkpoint.ProgramID == snapshot.ProgramID {
		return snapshot.ProgramID, true
	}
	// Start may have succeeded just before the response/checkpoint was lost.
	// Reconcile only the exact first pending track, then retry the same append ID.
	if checkpoint.ProgramID == "" && checkpoint.Pending != nil {
		pending := checkpoint.Pending
		match := func(id string) bool {
			return id == pending.ID || id == pending.ID+"-raw" || id == scriptFirstTrackID(session, checkpoint.Sequence, pending.Unit, 0) || id == scriptFirstTrackID(session, checkpoint.Sequence, pending.Unit, 0)+"-raw"
		}
		seen := match(snapshot.TrackID)
		for _, receipt := range snapshot.MainlinePlaybackReceipts {
			seen = seen || match(receipt.TrackID)
		}
		if seen {
			checkpoint.ProgramID = snapshot.ProgramID
			if writeVolcengineJSON(path, checkpoint) == nil {
				return snapshot.ProgramID, true
			}
		}
	}
	return "", false
}

// Core PCM progress is the evidence for play counts. An append acknowledgement
// only proves queuing, and must not unlock paid rewrites or retirement.
func (s *Server) confirmScriptPlayback(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, units []volcengineScriptUnit, cursor *volcengineScriptCursor, snapshot coreclient.ProgramRefreshSnapshot, save func() error) error {
	played := map[string]bool{}
	for _, receipt := range snapshot.MainlinePlaybackReceipts {
		if receipt.StartedAt != "" {
			played[receipt.TrackID] = true
		}
	}
	for _, receipt := range snapshot.PlaybackReceipts {
		if receipt.PlayedMS > 0 {
			played[receipt.TrackID] = true
		}
	}
	changed := false
	started := map[string]string{}
	for _, receipt := range snapshot.MainlinePlaybackReceipts {
		started[receipt.TrackID] = receipt.StartedAt
	}
	for _, receipt := range snapshot.PlaybackReceipts {
		for i := range cursor.Delivered {
			track := &cursor.Delivered[i]
			if track.TrackID == receipt.TrackID && receipt.PlayedMS > track.PolicyRecordedMS {
				if err := recordScriptPolicyPCM(recordingCacheMediaRoot(), tenant, room, v.PlanID, *track, receipt.PlayedMS, started[track.TrackID], time.Now().UTC()); err != nil {
					return err
				}
				track.PolicyRecordedMS = receipt.PlayedMS
				changed = true
			}
		}
	}
	for _, receipt := range snapshot.PlaybackReceipts {
		if !receipt.Complete {
			continue
		}
		for i := range cursor.Delivered {
			if cursor.Delivered[i].TrackID == receipt.TrackID && !cursor.Delivered[i].Completed {
				cursor.Delivered[i].Completed = true
				changed = true
			}
		}
	}
	instances := map[string][]scriptDeliveredTrack{}
	for _, track := range cursor.Delivered {
		instances[track.InstanceID] = append(instances[track.InstanceID], track)
	}
	for _, tracks := range instances {
		first := tracks[0]
		if first.Unit < 0 || first.Unit >= len(units) || len(tracks) < max(1, first.TotalBlocks) {
			continue
		}
		complete := true
		for _, track := range tracks {
			if !track.Completed {
				complete = false
			}
		}
		if complete {
			if err := markScriptRoomPlayback(recordingCacheMediaRoot(), tenant, room, v.PlanID, units[first.Unit].poolID(), strconv.FormatInt(first.AssetID, 10), first.InstanceID, true); err != nil {
				return err
			}
			if err := markScriptVersionCompletedOnce(recordingCacheMediaRoot(), v.PlanID, units[first.Unit].poolID(), strconv.FormatInt(first.AssetID, 10), first.InstanceID); err != nil {
				return err
			}
		}
	}
	for i := range cursor.Delivered {
		track := &cursor.Delivered[i]
		if track.Confirmed || !played[track.TrackID] || track.Unit < 0 || track.Unit >= len(units) {
			continue
		}
		if err := markScriptRoomPlayback(recordingCacheMediaRoot(), tenant, room, v.PlanID, units[track.Unit].poolID(), strconv.FormatInt(track.AssetID, 10), track.InstanceID, false); err != nil {
			return err
		}
		fresh, err := markScriptVersionPlayedOnce(recordingCacheMediaRoot(), v.PlanID, units[track.Unit].poolID(), strconv.FormatInt(track.AssetID, 10), track.InstanceID)
		if err != nil {
			return err
		}
		track.Confirmed = true
		changed = true
		if !fresh {
			continue
		}
		voiceVersion := scriptVersionForVoice(v, recordingScriptVoice{VoiceID: track.VoiceID, Model: track.Model})
		recordPlaybackLedger(tenant, room, voiceVersion, track.Unit, track.Block, track.TrackID)
		log.Printf("script playback confirmed tenant=%d room=%d session=%d track=%s asset=%d", tenant, room, session, track.TrackID, track.AssetID)
		// Continuous policy ticks choose the next shared deficit; playback receipts
		// only provide evidence, never buy a version per play.
	}
	if changed {
		if len(cursor.Delivered) > 128 {
			incomplete := map[string]bool{}
			for _, track := range cursor.Delivered {
				if !track.Completed {
					incomplete[track.InstanceID] = true
				}
			}
			var retained []scriptDeliveredTrack
			for i, track := range cursor.Delivered {
				if incomplete[track.InstanceID] || !track.Confirmed || i >= len(cursor.Delivered)-128 {
					retained = append(retained, track)
				}
			}
			cursor.Delivered = retained
		}
		return save()
	}
	return nil
}

func markScriptVersionCompletedOnce(root string, plan int64, piece, asset, instance string) error {
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return err
	}
	defer release()
	pool, err := loadScriptVoicePoolLocked(root, plan, piece)
	if err != nil {
		return err
	}
	for i := range pool.Versions {
		v := &pool.Versions[i]
		if v.AudioRef != asset {
			continue
		}
		for _, seen := range v.CompletedTracks {
			if seen == instance {
				return nil
			}
		}
		v.CompleteCount++
		v.CompletedTracks = append(v.CompletedTracks, instance)
		if len(v.CompletedTracks) > 256 {
			v.CompletedTracks = v.CompletedTracks[len(v.CompletedTracks)-256:]
		}
		return saveScriptVoicePoolLocked(root, plan, piece, pool)
	}
	return nil
}

func markScriptVersionPlayedOnce(root string, planID int64, piece, audioRef, trackID string) (bool, error) {
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return false, err
	}
	defer release()
	pool, err := loadScriptVoicePoolLocked(root, planID, piece)
	if err != nil {
		return false, err
	}
	for i := range pool.Versions {
		version := &pool.Versions[i]
		if version.AudioRef != audioRef {
			continue
		}
		for _, seen := range version.PlayedTracks {
			if seen == trackID {
				return false, nil
			}
		}
		version.PlayCount++
		version.LastPlayedAt = time.Now().UTC().Format(time.RFC3339Nano)
		version.PlayedTracks = append(version.PlayedTracks, trackID)
		if len(version.PlayedTracks) > 128 {
			version.PlayedTracks = version.PlayedTracks[len(version.PlayedTracks)-128:]
		}
		return true, saveScriptVoicePoolLocked(root, planID, piece, pool)
	}
	return false, nil
}
