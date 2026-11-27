package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

// A manual selection is explicit demand, not an invented positive-PCM receipt.
// It shares the existing piece/plan gates, rewrite prompt, safety checks, durable
// text/cache and TTS billing. Polling this command never grants another rewrite.
func (s *Server) prepareManualScriptAudio(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) (volcengineCachedAudio, error) {
	if err := checkScriptGenerationGuard(ctx); err != nil {
		return volcengineCachedAudio{}, err
	}
	unit = scriptUnitForVoice(v, unit)
	pick := func() (volcengineCachedAudio, bool) {
		audio, _, ok := scriptCompatiblePoolAudio(recordingCacheMediaRoot(), v, unit, fmt.Sprintf("manual:%d:%d", room, session), rand.New(rand.NewSource(time.Now().UnixNano())))
		return audio, ok
	}
	if audio, ok := pick(); ok {
		if s.scriptAudioAvailable(ctx, tenant, audio.AssetID) {
			return audio, nil
		}
		// A retained version with lost media is repaired from its exact spoken
		// text; it is not permission to buy a different rewrite.
		if strings.TrimSpace(audio.Text) != "" {
			key := scriptFirstParagraphKey(v, audio.Text)
			spoken := volcengineScriptUnit{Text: audio.Text, Key: key, Dir: filepath.Join(filepath.Dir(unit.Dir), key)}
			if err := checkScriptGenerationGuard(ctx); err != nil {
				return volcengineCachedAudio{}, err
			}
			reportScriptManualProgress(ctx, scriptManualTextReady)
			recovered, err := s.ensureScriptFirstParagraph(ctx, tenant, room, session, v, spoken, true)
			if err != nil {
				return recovered, err
			}
			if err = checkScriptGenerationGuard(ctx); err != nil {
				return recovered, err
			}
			err = replaceScriptPoolAudio(recordingCacheMediaRoot(), v, unit, audio.AssetID, recovered)
			recovered.Text = audio.Text
			return recovered, err
		}
	}
	// Let the room queue retry when the common writer finishes. Its
	// completed compatible audio is re-read after acquiring both locks.
	release, err := tryManualScriptGeneration(recordingCacheMediaRoot(), tenant, v, unit)
	if err != nil {
		return volcengineCachedAudio{}, err
	}
	defer release()
	if audio, ok := pick(); ok && s.scriptAudioAvailable(ctx, tenant, audio.AssetID) {
		return audio, nil
	}
	// Reuse a fully generated shared text after a previous TTS failure, including
	// work from another room/voice. Do not repay the text model on a retry.
	text := ""
	shared, found, err := scriptMissingSharedText(recordingCacheMediaRoot(), v, unit)
	if err != nil {
		return volcengineCachedAudio{}, err
	}
	if found {
		text = shared.Text
	}
	if text == "" {
		text, err = s.writeManualScriptText(ctx, tenant, room, session, v, unit)
		if err != nil {
			return volcengineCachedAudio{}, err
		}
	}
	if err = checkScriptGenerationGuard(ctx); err != nil {
		return volcengineCachedAudio{}, err
	}
	key := scriptFirstParagraphKey(v, text)
	spoken := volcengineScriptUnit{Text: text, Key: key, Dir: filepath.Join(filepath.Dir(unit.Dir), key)}
	reportScriptManualProgress(ctx, scriptManualTextReady)
	audio, err := s.ensureScriptFirstParagraph(ctx, tenant, room, session, v, spoken, true)
	if err != nil {
		return audio, err
	}
	if err = checkScriptGenerationGuard(ctx); err != nil {
		return audio, err
	}
	_, err = s.commitVoiceRewrite(ctx, tenant, recordingCacheMediaRoot(), v, unit, scriptplay.Version{FactsKey: scriptRewriteFactsKey(v), PieceID: unit.poolID(), Fingerprint: scriptplay.Fingerprint(text), Text: text, VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model, Kind: scriptplay.KindRewrite, AudioRef: strconv.FormatInt(audio.AssetID, 10), DurationMS: audio.DurationMS, SourcePath: spoken.Dir}, scriptVoiceVersionCap(v), false)
	if err != nil {
		return audio, err
	}
	if err = markScriptSharedTextRendered(recordingCacheMediaRoot(), v, unit, text); err != nil {
		return audio, err
	}
	audio.Text = text
	return audio, nil
}

func (s *Server) writeManualScriptText(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) (string, error) {
	if session <= 0 {
		return "", errScriptStructureUnavailable
	}
	document, frozen := scriptRewriteSnapshotFor(v)
	if !frozen {
		return "", errors.New("当前场次缺少话术生成快照")
	}
	facts, err := s.rewriteFactsJSON(ctx, tenant, v.PlanID)
	if err != nil {
		return "", err
	}
	if len(facts) == 0 || string(facts) == "[]" {
		return "", errors.New("当前方案缺少已确认事实")
	}
	redlines, ok := backgroundRewriteRedLines(document)
	if !ok {
		return "", errors.New("当前方案话术生成规则尚未就绪")
	}
	turn, err := beginScriptRewriteTurn(ctx, recordingCacheMediaRoot(), newScriptRewriteContextScope(tenant, v.PlanID, unit.Text, string(facts)))
	if err != nil {
		return "", err
	}
	defer turn.Close()
	parsed, err := s.generateScriptText(ctx, scriptGenerationDefault, scriptTextGenerationInput{
		material: scriptTextMaterial{version: v, unit: unit, facts: facts, redLines: redlines},
		session:  session,
	})
	if err != nil {
		return "", err
	}
	if err = checkScriptGenerationGuard(ctx); err != nil {
		return "", err
	}
	if err = saveScriptSharedText(recordingCacheMediaRoot(), v, unit, parsed.Script); err != nil {
		return "", err
	}
	if err = turn.Save(parsed); err != nil {
		return "", err
	}
	return parsed.Script, nil
}
