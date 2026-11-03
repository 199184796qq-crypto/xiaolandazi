package httpapi

import (
	"context"
	"fmt"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/ttsscheduler"
	"path/filepath"
)

// Only a fully resolved, authorized identity may buy audio for another voice.
// Old model/id-only snapshots remain readable, but must be reselected on save.
func normalizeScriptPlaybackVoices(input *model.CreateLiveAgentPlanVersionInput, validate func(*model.LiveAgentVoiceIdentity) error) error {
	script, ok := recordingScriptFromContext(input.GenerationContext)
	if !ok || len(script.Voices) == 0 {
		return nil
	}
	primary := input.VoiceIdentity
	for i := range script.Voices {
		voice := &script.Voices[i]
		if voice.Identity == nil {
			if voice.VoiceID != primary.VoiceID || (voice.Model != "" && voice.Model != primary.Model) {
				return fmt.Errorf("第%d个声音缺少完整身份，请重新选择后保存", i+1)
			}
			identity := primary
			voice.Identity = &identity
		}
		identity := *voice.Identity
		if identity.VoiceID != voice.VoiceID || identity.Model != voice.Model && voice.Model != "" {
			return fmt.Errorf("第%d个声音的音色与模型不一致，请重新选择", i+1)
		}
		if err := validate(&identity); err != nil {
			return fmt.Errorf("第%d个声音不可用：%w", i+1, err)
		}
		if ttsscheduler.PoolForProvider(identity.Provider) != ttsscheduler.PoolForProvider(primary.Provider) {
			return fmt.Errorf("多个声音须来自当前语音池，请重新选择")
		}
		if len(script.Voices) > 1 && !ttsgateway.SupportsScriptParagraphs(identity.Model) {
			return fmt.Errorf("第%d个声音暂不支持多声音话术播放，请重新选择", i+1)
		}
		identity.Rate = primary.Rate
		voice.Identity, voice.Model = &identity, identity.Model
	}
	first := script.Voices[0].Identity
	if first.VoiceID != primary.VoiceID || first.Model != primary.Model || first.Provider != primary.Provider || first.ProfileID != primary.ProfileID || first.BindingID != primary.BindingID || first.Source != primary.Source {
		return fmt.Errorf("第一个声音须与方案主声音一致，请重新选择后保存")
	}
	input.GenerationContext["recording_script"] = script
	return nil
}

func (s *Server) validateScriptPlaybackVoices(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion) error {
	for i, voice := range scriptVoicesForPlayback(v) {
		if voice.Identity == nil {
			continue // Legacy secondary voices can only use already-paid cache.
		}
		identity := *voice.Identity
		if err := s.validatePlanVoiceIdentity(ctx, tenant, &identity); err != nil {
			return fmt.Errorf("第%d个声音不可用：%w", i+1, err)
		}
		if ttsscheduler.PoolForProvider(identity.Provider) != ttsscheduler.PoolForProvider(v.VoiceIdentity.Provider) {
			return fmt.Errorf("多个声音须来自当前语音池，请重新选择")
		}
		if room > 0 {
			if err := s.store.CheckTTSRoomPool(ctx, tenant, room, identity.Provider); err != nil {
				return err
			}
		}
	}
	return nil
}

func scriptUnitForVoice(v model.LiveAgentPlanVersion, unit volcengineScriptUnit) volcengineScriptUnit {
	if unit.Pool == "" {
		unit.Pool = unit.Key
	}
	unit.Key = scriptFirstParagraphKey(v, unit.Text)
	unit.Dir = filepath.Join(filepath.Dir(unit.Dir), unit.Key)
	return unit
}

// Prepare just the next voice, one missing original at a time. Do not fill all
// selected voices upfront. Cache readiness includes the actual paid audio file.
func scriptNextVoicePreparation(voices []recordingScriptVoice, current int, v model.LiveAgentPlanVersion, units []volcengineScriptUnit) (model.LiveAgentPlanVersion, volcengineScriptUnit, bool) {
	if len(voices) < 2 || current < 0 || current >= len(voices) {
		return v, volcengineScriptUnit{}, false
	}
	voice := voices[(current+1)%len(voices)]
	if voice.Identity == nil {
		return v, volcengineScriptUnit{}, false
	}
	target := scriptVersionForVoice(v, voice)
	for _, unit := range units {
		unit = scriptUnitForVoice(target, unit)
		if len(scriptCompatiblePool(recordingCacheMediaRoot(), target, unit).Versions) == 0 {
			return target, unit, true
		}
	}
	return target, volcengineScriptUnit{}, false
}

func scriptVoicePreparationDue(cursor volcengineScriptCursor, snapshot coreclient.ProgramRefreshSnapshot, lastAttempt string, currentReady bool) bool {
	return cursor.ProgramID != "" && cursor.VoiceBlockLeft <= 1 && currentReady &&
		!snapshot.WaitingForContent && snapshot.BufferedMainlineMS >= volcengineReplayThresholdMS && snapshot.BufferedMainlineMS < volcengineSupplyThresholdMS &&
		lastAttempt != fmt.Sprintf("%d:%d", cursor.Round, cursor.Position)
}
