package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"path/filepath"
	"slices"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/voicebatch"
)

type scriptManualGeneration struct {
	Command  scriptManualJump     `json:"command"`
	Identity string               `json:"identity"`
	Batch    uint64               `json:"batch"`
	Waiter   string               `json:"waiter,omitempty"`
	Progress scriptManualProgress `json:"progress,omitempty"`
}

// The receipt remains stable after a cut, while requests for the same piece
// coalesce in the durable room queue until the existing job finishes.
func enqueueScriptManualGeneration(ctx context.Context, path string, command scriptManualJump, identity string, batch uint64, wait bool) error {
	release, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	state, err := readScriptManualState(path)
	if err != nil {
		return err
	}
	if !scriptManualActive(state.Current) || state.Current.RequestID != command.RequestID {
		return errScriptManualStale
	}
	if state.GenerationRequested == command.RequestID {
		if !wait {
			for i := range state.Generation {
				if state.Generation[i].Waiter == command.RequestID {
					state.Generation[i].Waiter = ""
					state.Current.Progress = ""
					state.Receipts[command.RequestID] = *state.Current
					return writeVolcengineJSON(path, state)
				}
			}
		}
		return nil
	}
	state.GenerationRequested = command.RequestID
	job := scriptManualGeneration{
		Command: command, Identity: identity, Batch: batch,
		Progress: scriptManualQueued,
	}
	if wait {
		job.Waiter = command.RequestID
		state.Current.Status = "preparing"
	}
	index := slices.IndexFunc(state.Generation, func(old scriptManualGeneration) bool {
		return old.Command.VersionID == command.VersionID && old.Command.ProgramID == command.ProgramID &&
			old.Command.PieceID == command.PieceID && old.Identity == identity && old.Batch == batch
	})
	if index < 0 {
		state.Generation = append(state.Generation, job)
		index = len(state.Generation) - 1
	} else {
		state.Generation[index].Waiter = job.Waiter
	}
	queued := &state.Generation[index]
	progress := queued.Progress
	if progress == "" {
		progress = scriptManualQueued
	}
	setScriptManualProgress(&state, queued, progress)
	return writeVolcengineJSON(path, state)
}

func finishScriptManualGeneration(ctx context.Context, path string, command scriptManualJump, audio volcengineCachedAudio, cause error) error {
	release, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	state, err := readScriptManualState(path)
	if err != nil {
		return err
	}
	if len(state.Generation) == 0 || state.Generation[0].Command.RequestID != command.RequestID {
		return nil
	}
	job := state.Generation[0]
	state.Generation = state.Generation[1:]
	progress, message := scriptManualGenerationResult(job.Progress, cause)
	if job.Waiter != "" && scriptManualActive(state.Current) && state.Current.RequestID == job.Waiter && state.Current.Status != "waiting" {
		setScriptManualProgress(&state, &job, progress)
		if cause == nil {
			state.Prepared = &audio
		} else {
			var templateError *scriptPieceTemplateError
			if errors.As(cause, &templateError) {
				message = templateError.Error()
			}
			state.Current.Status, state.Current.Error = "failed", message
			state.Receipts[job.Waiter] = *state.Current
			state.Prepared = nil
		}
	}
	return writeVolcengineJSON(path, state)
}

func scriptManualGenerationPending(tenant, room, session int64) (bool, error) {
	state, err := readScriptManualState(scriptManualPath(tenant, room, session))
	return err == nil && (len(state.Generation) > 0 || state.Current != nil && state.Current.Status == "requested"), err
}

func (s *Server) tickScriptManualGeneration(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, units []volcengineScriptUnit, p coreclient.ProgramRefreshSnapshot, batch uint64) error {
	path := scriptManualPath(tenant, room, session)
	state, err := readScriptManualState(path)
	if err != nil || len(state.Generation) == 0 {
		return err
	}
	root := recordingCacheMediaRoot()
	release, idle, err := tryRoomScriptGeneration(root, tenant, room)
	if err != nil || !idle {
		return err
	}
	// The queue could have advanced while another producer held the room slot.
	state, err = readScriptManualState(path)
	if err != nil || len(state.Generation) == 0 {
		release()
		return err
	}
	job := state.Generation[0]
	command := job.Command
	index := scriptManualUnit(units, command.PieceID)
	if !scriptManualMatches(&command, session, v, p) || job.Batch != batch || index < 0 {
		defer release()
		return finishScriptManualGeneration(ctx, path, command, volcengineCachedAudio{}, errScriptManualStale)
	}
	units = slices.Clone(units)
	go func() {
		defer release()
		var audio volcengineCachedAudio
		var err error
		defer func() {
			if failure := recover(); failure != nil {
				err = errors.New("定向话术准备异常")
			}
			if errors.Is(err, errRoomScriptGenerationBusy) || errors.Is(err, errVolcengineLivePaused) || ctx.Err() != nil {
				persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer cancel()
				_ = updateScriptManualProgress(persist, path, command, scriptManualQueued)
				return
			}
			if err != nil {
				log.Printf("manual script generation deferred tenant=%d room=%d session=%d request=%s: %v", tenant, room, session, command.RequestID, err)
			}
			persist, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer done()
			if finishErr := finishScriptManualGeneration(persist, path, command, audio, err); finishErr != nil {
				log.Printf("manual script generation receipt room=%d session=%d: %v", room, session, finishErr)
			}
		}()
		work, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		work = context.WithValue(work, scriptManualProgressKey{}, func(phase scriptManualProgress) {
			persist, done := context.WithTimeout(work, time.Second)
			defer done()
			if err := updateScriptManualProgress(persist, path, command, phase); err != nil && persist.Err() == nil {
				log.Printf("manual script progress room=%d session=%d: %v", room, session, err)
			}
		})
		work, voice, err := s.voiceAssetGeneration(work, tenant, v)
		if err == nil && scriptGenerationIdentity(voice) != job.Identity {
			err = errScriptManualStale
		}
		guard := func(c context.Context) error {
			paused, err := s.checkLiveAnchorOwner(c, tenant, room, session, voice.ID)
			if err != nil {
				return err
			}
			if paused {
				return errVolcengineLivePaused
			}
			state, err := s.store.RuntimeVoiceBatch(c, tenant, room, session, voice.ID)
			if err != nil {
				return err
			}
			if state.Batch != job.Batch || state.Pending != nil || !voicebatch.Same(state.Active, voice.VoiceIdentity) {
				return errScriptManualStale
			}
			return nil
		}
		work = context.WithValue(work, scriptGenerationGuardKey{}, guard)
		work = withLiveCostScope(work, liveCostCallScope{TenantID: tenant, RoomID: room, SessionID: session, PlanID: v.PlanID, Priority: "optional", Reason: "手动选择话术准备", Key: fmt.Sprintf("script-manual:%d:%s", session, command.RequestID)})
		work = withScriptModelCostSteps(work)
		if err == nil {
			err = checkScriptGenerationGuard(work)
		}
		if err == nil {
			if unit := units[index]; unit.SpecPromptEnabled && scriptPiecePromptHash(unit) != "" {
				audio, err = s.prepareSpecifiedScriptAudio(work, tenant, room, session, voice, units, index, command.RequestID)
			} else {
				audio, err = s.prepareManualScriptAudio(work, tenant, room, session, voice, units[index])
			}
		}
	}()
	return nil
}

func (s *Server) scriptManualCachedAudio(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, targeted bool) (volcengineCachedAudio, bool) {
	unit = scriptUnitForVoice(v, unit)
	if targeted {
		facts, err := s.resolveScriptPieceFacts(ctx, tenant, v.PlanID, unit.SpecPrompt)
		if err == nil {
			unit.SpecFactsKey = scriptPieceFactsKey(facts)
		} else {
			unit.SpecPrompt = "" // A broken reference cannot match previously generated audio.
		}
	}
	available := func(id int64) bool { return s.scriptAudioAvailable(ctx, tenant, id) }
	return selectManualCachedAudio(recordingCacheMediaRoot(), fmt.Sprintf("manual:%d:%d", room, session), v, unit, targeted, available)
}

func selectManualCachedAudio(root, scope string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, targeted bool, available func(int64) bool) (volcengineCachedAudio, bool) {
	if targeted {
		if audio, ok := scriptSpecifiedCachedAudio(root, v, unit, available); ok {
			return audio, true
		}
	} else {
		audio, _, ok := scriptCompatiblePoolAudio(root, v, unit, scope, rand.New(rand.NewSource(time.Now().UnixNano())))
		if ok && available(audio.AssetID) {
			return audio, true
		}
	}
	cache, err := loadVolcengineParagraphCache(filepath.Join(unit.Dir, "cache.json"), unit.Key)
	if err == nil {
		if audio, ok := cache.Audio[100]; ok && audio.VoiceID == v.VoiceIdentity.VoiceID && audio.DurationMS > 0 && audio.AssetID > 0 &&
			available(audio.AssetID) {
			audio.Text = unit.Text
			return audio, true
		}
	}
	return volcengineCachedAudio{}, false
}

func (s *Server) ensureRoomScriptLookahead(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, allowSynthesis bool) (volcengineCachedAudio, error) {
	release, idle, err := tryRoomScriptGeneration(recordingCacheMediaRoot(), tenant, room)
	if err != nil {
		return volcengineCachedAudio{}, err
	}
	if !idle {
		return volcengineCachedAudio{}, errRoomScriptGenerationBusy
	}
	defer release()
	return s.ensureScriptFirstParagraph(ctx, tenant, room, session, v, unit, allowSynthesis)
}
