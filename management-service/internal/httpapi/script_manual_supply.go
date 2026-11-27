package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/voiceasset"
	"livecompanion/management/internal/voicebatch"
)

type scriptManualStage struct {
	Command           scriptManualJump
	Unit              int
	Instance          string
	Audio             volcengineCachedAudio
	Input             coreclient.ProgramRefreshInput
	Applied           bool
	LeasesConfirmed   bool
	ReplacedInstances []string
}

func scriptManualUnit(units []volcengineScriptUnit, piece string) int {
	for i, u := range units {
		if u.poolID() == piece {
			return i
		}
	}
	return -1
}
func scriptManualContinue(cursor *volcengineScriptCursor, unit int) error {
	for position, index := range cursor.Order {
		if index != unit {
			continue
		}
		cursor.Position, cursor.Previous, cursor.LastQueued = position+1, unit, unit
		if len(cursor.UnitSelections) != len(cursor.Order) {
			cursor.UnitSelections = make([]int, len(cursor.Order))
		}
		cursor.UnitSelections[unit]++
		cursor.ConsecutiveFallbacks = 0
		return nil
	}
	return errors.New("selected script is absent from playback order")
}

// Called by the single supply owner, including while ordinary lookahead is full.
// true prevents automatic append until the replacement has a Core receipt.
func (s *Server) processScriptManual(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, units []volcengineScriptUnit, cursor *volcengineScriptCursor, p coreclient.ProgramRefreshSnapshot, save func() error) (bool, error) {
	path := scriptManualPath(tenant, room, session)
	fail := func(j scriptManualJump, reason string, cause error) (bool, error) {
		log.Printf("manual script deferred tenant=%d room=%d session=%d request=%s: %v", tenant, room, session, j.RequestID, cause)
		if err := updateScriptManual(ctx, path, j, "failed", reason); err != nil {
			return true, err
		}
		cursor.ManualJump = nil
		return true, save()
	}
	if cursor.ManualJump == nil {
		commandState, err := readScriptManualState(path)
		if err != nil {
			return false, err
		}
		job := commandState.Current
		if !scriptManualActive(job) {
			return false, nil
		}
		if !scriptManualMatches(job, session, v, p) {
			return fail(*job, "直播场次或方案已变化，请重新选择", errScriptManualStale)
		}
		fission := strings.HasPrefix(job.PieceID, fissionChoicePrefix)
		standalone := scriptStandaloneChoice(job.PieceID)
		index := scriptManualUnit(units, job.PieceID)
		if index < 0 && !standalone {
			return fail(*job, "所选话术已不在当前方案中", errScriptManualStale)
		}
		state, err := s.store.RuntimeVoiceBatch(ctx, tenant, room, session, v.ID)
		if err != nil {
			return false, err
		}
		if state.Pending != nil || !voicebatch.Same(state.Active, v.VoiceIdentity) {
			return fail(*job, "主播声音正在切换，请完成后重新选择话术", errScriptManualBusy)
		}
		var unit volcengineScriptUnit
		if index >= 0 {
			unit = units[index]
		}
		var audio volcengineCachedAudio
		if fission {
			var ready bool
			audio, ready, err = s.prepareManualFission(ctx, tenant, v.PlanID, *job, true)
			if err != nil {
				return fail(*job, err.Error(), err)
			}
			if !ready {
				return false, nil
			}
		} else if standalone {
			asset, err := s.loadHumanSourceChoice(ctx, tenant, v.PlanID, job.PieceID)
			if err != nil || !s.scriptAudioAvailable(ctx, tenant, asset.ID) {
				return fail(*job, errHumanSourceChoiceChanged.Error(), errHumanSourceChoiceChanged)
			}
			audio = humanSourceCachedAudio(asset)
		} else if job.RecordingID > 0 {
			asset, err := s.store.ScriptHumanVoice(ctx, tenant, v.PlanID, unit.HumanKey, job.RecordingID)
			if err != nil {
				return fail(*job, "所选真人声音已不可用，当前播放继续，请重新选择", err)
			}
			if asset.DurationMS == nil || *asset.DurationMS == 0 || !s.scriptAudioAvailable(ctx, tenant, asset.ID) {
				return fail(*job, "所选真人声音已不可用，当前播放继续，请重新选择", errScriptManualStale)
			}
			audio = scriptHumanCachedAudio(asset, unit)
		} else {
			audio, _ = s.pickScriptHumanVoice(ctx, tenant, room, session, v, unit, "manual/"+job.RequestID)
		}
		if !audio.HumanVoice {
			_, voice, err := s.voiceAssetGeneration(ctx, tenant, v)
			if err != nil {
				return fail(*job, "目标声音暂不可用，当前播放继续", err)
			}
			targeted := unit.SpecPromptEnabled && scriptPiecePromptHash(unit) != ""
			var ready bool
			audio, ready = s.scriptManualCachedAudio(ctx, tenant, room, session, voice, unit, targeted)
			if !ready && !targeted && commandState.Prepared != nil &&
				commandState.Prepared.VoiceID == voice.VoiceIdentity.VoiceID &&
				s.scriptAudioAvailable(ctx, tenant, commandState.Prepared.AssetID) {
				audio, ready = *commandState.Prepared, true
			}
			if targeted || !ready || commandState.GenerationRequested == job.RequestID {
				if err = enqueueScriptManualGeneration(ctx, path, *job, scriptGenerationIdentity(voice), state.Batch, !ready); err != nil {
					if errors.Is(err, errScriptManualStale) {
						return false, nil
					}
					return false, err
				}
			}
			if !ready {
				if commandState.Prepared != nil {
					return fail(*job, "目标声音已更新，请重新选择话术", errScriptManualStale)
				}
				return false, nil
			}
		}
		// Check Core's latest generation after validating the cached audio.
		p, err = s.core.GetRoomProgramSnapshot(ctx, tenant, room)
		if err != nil {
			return fail(*job, "读取播放状态失败，当前播放继续，请重试", err)
		}
		// An interaction temporarily suspends mainline; Core already knows how
		// to retain this ready replacement until the host resumes and finishes.
		// A user pause is rejected by the live-owner guard, not by this flag.
		if !scriptManualMatches(job, session, v, p) || p.PendingJobID != "" {
			return fail(*job, "播放状态已变化，请稍后重新选择", errScriptManualStale)
		}
		if standalone {
			index = scriptFissionContinuation(cursor, p.TrackID)
		}
		cursor.Sequence++
		stage := &scriptManualStage{Command: *job, Unit: index, Instance: fmt.Sprintf("long-%d-%d", session, cursor.Sequence), Audio: audio}
		future := map[string]bool{}
		for _, track := range p.FutureWindow {
			future[track.ID] = true
		}
		for _, sent := range cursor.Delivered {
			if future[sent.TrackID] {
				stage.ReplacedInstances = append(stage.ReplacedInstances, sent.InstanceID)
			}
		}
		stage.Input = coreclient.ProgramRefreshInput{ReplaceAfterCurrent: true, JobID: "script-manual-" + job.RequestID, ExpectedProgramID: p.ProgramID, ExpectedVersionID: v.ID, ExpectedGeneration: p.Generation, Generation: p.Generation + 1, NewVersionID: v.ID, NewVersionNo: v.VersionNo}
		blocks := audio.Blocks
		if len(blocks) == 0 {
			text := audio.Text
			if text == "" {
				text = unit.Text
			}
			blocks = []scriptFirstCachedBlock{{AssetID: audio.AssetID, DurationMS: audio.DurationMS, Text: text, Timeline: audio.Timeline}}
		}
		var offset int64
		for bi, block := range blocks {
			id := stage.Instance
			if len(audio.Blocks) > 0 {
				id = scriptFirstTrackID(session, cursor.Sequence, index, bi)
			}
			if audio.UntimedTranscript {
				id += "-raw"
			}
			if err = s.store.LeaseVoiceAsset(ctx, tenant, block.AssetID, room, session, p.ProgramID, id); err != nil {
				return fail(*job, "目标声音暂不可用，当前播放继续", err)
			}
			url, err := s.recordingStreamLeasedAssetURL(ctx, tenant, block.AssetID, session, id)
			if err != nil {
				return fail(*job, "目标声音暂不可用，当前播放继续", err)
			}
			label := "手动选择话术"
			if audio.HumanVoice {
				label = "真人声音"
			}
			deliveredUnit := index
			if standalone {
				label = job.Title
				deliveredUnit = -1
			}
			stage.Input.Tracks = append(stage.Input.Tracks, coreclient.ProgramRefreshTrack{ID: id, Label: label, Text: block.Text, AudioURL: url, DurationMS: int(block.DurationMS), AllowConsecutiveRepeat: true, UntimedTranscript: audio.UntimedTranscript, Timeline: recordingStreamTimeline(block.Timeline), SafePoints: recordingStreamSafePoints(volcengineSemanticSafePoints(block.Timeline))})
			cursor.Delivered = append(cursor.Delivered, scriptDeliveredTrack{
				PieceID:          job.PieceID,
				HumanVoice:       audio.HumanVoice,
				HumanSource:      audio.HumanSource,
				PolicyText:       audio.Text,
				PolicyDurationMS: audio.DurationMS,
				PolicyOffsetMS:   offset,
				Unit:             deliveredUnit,
				TrackID:          id,
				InstanceID:       stage.Instance,
				AssetID:          audio.AssetID,
				VoiceID:          v.VoiceIdentity.VoiceID,
				Model:            v.VoiceIdentity.Model,
				Batch:            cursor.VoiceBatch,
				TotalBlocks:      len(blocks),
				Block:            block,
			})
			offset += block.DurationMS
		}
		if !standalone {
			if err = reserveScriptPolicyAudio(recordingCacheMediaRoot(), v, unit, tenant, room, stage.Instance, audio); err != nil {
				return fail(*job, "保存目标话术失败，当前播放继续", err)
			}
		}
		cursor.ManualJump = stage
		if err = save(); err != nil {
			return true, err
		}
	}
	stage := cursor.ManualJump
	if !stage.LeasesConfirmed && (p.LastAppliedJobID == stage.Input.JobID || p.PendingJobID == stage.Input.JobID) {
		// The Core acceptance can survive a lost response / local SQL commit.
		// Reconcile every prepared child before leaving its durable stage.
		for bi, track := range stage.Input.Tracks {
			asset := stage.Audio.AssetID
			if len(stage.Audio.Blocks) > 0 {
				asset = stage.Audio.Blocks[bi].AssetID
			}
			if err := s.store.ConfirmVoiceAssetLease(ctx, tenant, asset, session, track.ID, p.ProgramID); err != nil {
				return true, err
			}
			if !stage.Audio.HumanVoice {
				s.archiveTTSCacheReplay(ctx, tenant, room, session, p.ProgramID, track)
			}
		}
		stage.LeasesConfirmed = true
		if err := save(); err != nil {
			return true, err
		}
	}
	if p.LastAppliedJobID == stage.Input.JobID {
		if !stage.Applied {
			if err := releaseScriptManualReservations(ctx, tenant, room, v.PlanID, stage.ReplacedInstances); err != nil {
				return true, err
			}
			if scriptStandaloneChoice(stage.Command.PieceID) {
				resumeScriptAfterFission(cursor, stage.Unit)
				cursor.LastHumanChoice = scriptHumanChoiceKey(stage.Command.PieceID)
			} else if err := scriptManualContinue(cursor, stage.Unit); err != nil {
				return true, err
			}
			stage.Applied = true
			if err := save(); err != nil {
				return true, err
			}
		}
		// A choice changes the auto-play pool only after Core applies its cut.
		modePath := scriptMainlineModePath(recordingCacheMediaRoot(), tenant, room)
		if err := writeScriptMainlineMode(ctx, modePath, scriptManualMode(stage.Command.PieceID)); err != nil {
			return true, err
		}
		// Keep the stage until both the cursor and command are durable. Repeating
		// this branch is harmless: restore the same next position, not an append.
		if err := updateScriptManual(ctx, path, stage.Command, "applied", ""); err != nil && !errors.Is(err, errScriptManualStale) {
			return true, err
		}
		cursor.ManualJump = nil
		return true, save()
	}
	if !scriptManualMatches(&stage.Command, session, v, p) {
		return fail(stage.Command, "直播场次或方案已变化，请重新选择", errScriptManualStale)
	}
	if p.PendingJobID == stage.Input.JobID {
		if time.Until(stage.Input.ValidUntil) < 25*time.Second {
			stage.Input.ValidUntil = time.Now().Add(50 * time.Second)
			if _, err := s.core.RenewRoomProgramRefresh(ctx, tenant, room, stage.Input.JobID, p.ProgramID, stage.Input.ValidUntil); err != nil {
				return true, waitLiveProducer(ctx)
			}
			if err := save(); err != nil {
				return true, err
			}
		}
		return true, waitLiveProducer(ctx)
	}
	if p.Generation != stage.Input.ExpectedGeneration || p.PendingJobID != "" {
		return fail(stage.Command, "播放顺序已变化，请重新选择", errScriptManualStale)
	}
	stage.Input.ValidUntil = time.Now().Add(50 * time.Second)
	if err := save(); err != nil {
		return true, err
	}
	for bi, track := range stage.Input.Tracks {
		blockAsset := stage.Audio.AssetID
		if len(stage.Audio.Blocks) > 0 {
			blockAsset = stage.Audio.Blocks[bi].AssetID
		}
		err := s.store.DispatchVoiceAsset(ctx, tenant, blockAsset, session, track.ID, func() (string, error) {
			_, err := s.core.RefreshRoomProgram(ctx, tenant, room, stage.Input)
			return p.ProgramID, err
		})
		if err != nil {
			if errors.Is(err, voiceasset.ErrCleared) {
				return fail(stage.Command, "目标声音已更新，请重新选择话术", err)
			}
			// A timeout is ambiguous. Keep the exact signed URLs/IDs/body and
			// reconcile Core before retrying; never create another paid request.
			if errors.Is(err, coreclient.ErrProgramRefreshConflict) {
				fresh, readErr := s.core.GetRoomProgramSnapshot(ctx, tenant, room)
				if readErr == nil && fresh.LastAppliedJobID != stage.Input.JobID && fresh.PendingJobID != stage.Input.JobID && fresh.Generation == p.Generation {
					return fail(stage.Command, "目标话术未能切换，当前播放继续，请重试", err)
				}
			}
			return true, waitLiveProducer(ctx)
		}
		if !stage.Audio.HumanVoice {
			s.archiveTTSCacheReplay(ctx, tenant, room, session, p.ProgramID, track)
		}
	}
	if err := updateScriptManual(ctx, path, stage.Command, "waiting", ""); err != nil {
		return true, err
	}
	return true, waitLiveProducer(ctx)
}

// Discard only unheard reservations for the replaced future. Actual listening
// history, coverage counters, provider results and physical audio are retained.
func releaseScriptManualReservations(ctx context.Context, tenant, room, plan int64, instances []string) error {
	if len(instances) == 0 {
		return nil
	}
	path := scriptRoomPolicyPath(recordingCacheMediaRoot(), tenant, room, plan)
	release, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	st, err := readScriptRoomPolicy(path, tenant, room, plan)
	if err != nil {
		return err
	}
	for _, cycle := range st.Cycles {
		for _, instance := range instances {
			delete(cycle.Pending, instance)
		}
	}
	return writeVolcengineJSON(path, st)
}
