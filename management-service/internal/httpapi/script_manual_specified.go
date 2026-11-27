package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

type scriptSpecifiedPending struct {
	RequestID string `json:"request_id"`
	Text      string `json:"text"`
	Ready     bool   `json:"ready,omitempty"`
}

func specifiedScriptSlot(root string, v model.LiveAgentPlanVersion, units []volcengineScriptUnit, index int, hasPlayable bool, now time.Time, warmup bool) (int, string, error) {
	inventory := scriptPlanPools(root, v, units)
	cap := inventory.Caps[index]
	count := len(inventory.Pools[index].Versions)
	if !hasPlayable {
		// An ordinary pool may already be full before the first explicit demand.
		// Admit the first playable specified version without removing paid audio.
		return max(cap, count+1), "", nil
	}
	if count < cap {
		return cap, "", nil
	}
	next, limit, retire, err := scriptPolicyGenerationForPiece(root, v, units, units[index].poolID(), now, true, warmup)
	if next != index {
		return 0, "", err
	}
	return limit, retire, err
}

func scriptSpecifiedCachedAudio(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, available func(int64) bool) (volcengineCachedAudio, bool) {
	hash := scriptPieceVersionHash(unit)
	if hash == "" {
		return volcengineCachedAudio{}, false
	}
	versions := scriptCompatiblePool(root, v, unit).Versions
	for _, i := range rand.Perm(len(versions)) {
		version := versions[i]
		if version.SpecHash != hash {
			continue
		}
		if audio, ok := scriptVersionAudio(root, unit, v, version); ok && available(audio.AssetID) {
			return audio, true
		}
	}
	return volcengineCachedAudio{}, false
}

func (s *Server) prepareSpecifiedScriptAudio(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, units []volcengineScriptUnit, index int, requestID string) (volcengineCachedAudio, error) {
	root := recordingCacheMediaRoot()
	units = append([]volcengineScriptUnit(nil), units...)
	for i := range units {
		units[i] = scriptUnitForVoice(v, units[i])
	}
	unit := units[index]
	release, err := tryManualScriptGeneration(root, tenant, v, unit)
	if err != nil {
		return volcengineCachedAudio{}, err
	}
	defer release()
	facts, err := s.resolveScriptPieceFacts(ctx, tenant, v.PlanID, unit.SpecPrompt)
	if err != nil {
		return volcengineCachedAudio{}, err
	}
	unit.SpecFactsKey = scriptPieceFactsKey(facts)
	units[index] = unit
	hash := scriptPieceVersionHash(unit)
	available := func(id int64) bool { return s.scriptAudioAvailable(ctx, tenant, id) }
	selected, hasPlayable := scriptSpecifiedCachedAudio(root, v, unit, available)
	pendingPath := filepath.Join(root, "script-texts", strconv.FormatInt(v.PlanID, 10), "specified",
		unit.poolID(), shortScriptIdentity(hash+"|"+scriptGenerationIdentity(v))+".json")
	var pending scriptSpecifiedPending
	if err = readVolcengineJSON(pendingPath, &pending); err != nil && !errors.Is(err, os.ErrNotExist) {
		return volcengineCachedAudio{}, err
	}
	if pending.RequestID != requestID || !pending.Ready {
		now := time.Now().UTC()
		cap, retire, err := specifiedScriptSlot(root, v, units, index, hasPlayable, now, scriptPolicyRoomWarmup(root, tenant, room, v.PlanID, now))
		if err == nil && cap > 0 {
			err = s.generateSpecifiedScriptAudio(ctx, tenant, room, session, v, unit, facts, cap, retire, pendingPath, requestID, pending)
			if err == nil {
				selected, hasPlayable = scriptSpecifiedCachedAudio(root, v, unit, available)
			}
		}
		if err != nil && !hasPlayable {
			return volcengineCachedAudio{}, err
		}
	}
	if !hasPlayable {
		return volcengineCachedAudio{}, errors.New("本片暂无可播放的定向话术")
	}
	if err := checkScriptGenerationGuard(ctx); err != nil {
		return volcengineCachedAudio{}, err
	}
	if err := s.checkScriptPieceFacts(ctx, tenant, v.PlanID, unit); err != nil {
		return volcengineCachedAudio{}, err
	}
	return selected, nil
}

func (s *Server) generateSpecifiedScriptAudio(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, facts json.RawMessage, cap int, retire, pendingPath, requestID string, pending scriptSpecifiedPending) error {
	snapshot, ok := scriptRewriteSnapshotFor(v)
	if !ok {
		return errors.New("当前场次缺少话术生成快照")
	}
	redLines, ok := backgroundRewriteRedLines(snapshot)
	if !ok {
		return errors.New("当前场次红线规则尚未就绪")
	}
	if err := checkScriptGenerationGuard(ctx); err != nil {
		return err
	}
	if pending.Text == "" || pending.Ready {
		parsed, err := s.generateScriptText(ctx, scriptGenerationSpecified, scriptTextGenerationInput{
			material: scriptTextMaterial{version: v, unit: unit, facts: facts, redLines: redLines},
			session:  session,
		})
		if err != nil {
			return err
		}
		pending = scriptSpecifiedPending{Text: parsed.Script}
	}
	pending.RequestID = requestID
	if err := writeVolcengineJSON(pendingPath, pending); err != nil {
		return err
	}
	key := scriptFirstParagraphKey(v, pending.Text)
	spoken := volcengineScriptUnit{Text: pending.Text, Key: key, Dir: filepath.Join(filepath.Dir(unit.Dir), key)}
	reportScriptManualProgress(ctx, scriptManualTextReady)
	audio, err := s.ensureScriptFirstParagraph(ctx, tenant, room, session, v, spoken, true)
	if err != nil {
		return err
	}
	if err := checkScriptGenerationGuard(ctx); err != nil {
		return err
	}
	if err := s.checkScriptPieceFacts(ctx, tenant, v.PlanID, unit); err != nil {
		return err
	}
	version := scriptplay.Version{
		FactsKey: scriptRewriteFactsKey(v), PieceID: unit.poolID(), Fingerprint: scriptplay.Fingerprint(pending.Text),
		Text: pending.Text, VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model, Kind: scriptplay.KindRewrite,
		AudioRef: strconv.FormatInt(audio.AssetID, 10), DurationMS: audio.DurationMS, SourcePath: spoken.Dir,
		SpecHash: scriptPieceVersionHash(unit),
	}
	var replacement []string
	if retire != "" {
		replacement = append(replacement, retire)
	}
	if _, err := s.commitVoiceRewrite(ctx, tenant, recordingCacheMediaRoot(), v, unit, version, cap, retire != "", replacement...); err != nil {
		return err
	}
	pending.Ready = true
	return writeVolcengineJSON(pendingPath, pending)
}
