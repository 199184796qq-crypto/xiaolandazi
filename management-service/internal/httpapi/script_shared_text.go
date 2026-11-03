package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Text is shared across voices; paid audio remains strictly voice/model/rate
// scoped. Persist accepted rewrites BEFORE TTS so a failed purchase is not a
// reason to call the text model again.
type scriptSharedText struct {
	Text          string          `json:"text"`
	Fingerprint   string          `json:"fingerprint"`
	FactsKey      string          `json:"facts_key"`
	Kind          scriptplay.Kind `json:"kind"`
	RenderedAudio map[string]bool `json:"rendered_audio,omitempty"`
}
type scriptSharedTexts struct {
	Versions []scriptSharedText `json:"versions"`
}

func scriptSharedTextPath(root string, plan int64, piece string) string {
	return filepath.Join(root, "script-texts", strconv.FormatInt(plan, 10), filepath.Base(scriptVoicePoolPath(root, plan, piece)))
}

func scriptSharedTextScopePath(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) string {
	return filepath.Join(root, "script-texts", strconv.FormatInt(v.PlanID, 10), "policies", scriptRewriteFactsKey(v), filepath.Base(scriptVoicePoolPath(root, v.PlanID, unit.poolID())))
}

func loadScriptSharedTextsLocked(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) (scriptSharedTexts, error) {
	var out scriptSharedTexts
	err := readVolcengineJSON(scriptSharedTextScopePath(root, v, unit), &out)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	// Import legacy already-paid rewrites from every voice without new LLM work.
	pool, err := loadScriptVoicePoolLocked(root, v.PlanID, unit.poolID())
	if err != nil {
		return out, err
	}
	seen := map[string]int{}
	var valid []scriptSharedText
	facts := scriptRewriteFactsKey(v)
	for _, entry := range out.Versions {
		if _, exists := seen[entry.Fingerprint]; !exists && entry.FactsKey == facts && entry.Kind == scriptplay.KindRewrite && strings.TrimSpace(entry.Text) != "" && entry.Fingerprint == scriptplay.Fingerprint(entry.Text) {
			seen[entry.Fingerprint] = len(valid)
			valid = append(valid, entry)
		}
	}
	for _, entry := range pool.Versions {
		if facts != "" && entry.FactsKey == facts && entry.Kind == scriptplay.KindRewrite && strings.TrimSpace(entry.Text) != "" {
			key := scriptplay.Fingerprint(entry.Text)
			index, exists := seen[key]
			if !exists {
				index = len(valid)
				seen[key] = index
				valid = append(valid, scriptSharedText{Text: entry.Text, Fingerprint: key, FactsKey: facts, Kind: scriptplay.KindRewrite})
			}
			if valid[index].RenderedAudio == nil {
				valid[index].RenderedAudio = map[string]bool{}
			}
			if entry.SourcePath != "" {
				valid[index].RenderedAudio[filepath.Base(entry.SourcePath)] = true
			}
		}
	}
	out.Versions = valid
	return out, nil
}

func saveScriptSharedText(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, text string) error {
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return err
	}
	defer release()
	pool, err := loadScriptSharedTextsLocked(root, v, unit)
	if err != nil {
		return err
	}
	key := scriptplay.Fingerprint(text)
	for _, entry := range pool.Versions {
		if entry.Fingerprint == key {
			// Also persist legacy imports before the audio slot can be retired.
			return writeVolcengineJSON(scriptSharedTextScopePath(root, v, unit), pool)
		}
	}
	pool.Versions = append(pool.Versions, scriptSharedText{Text: text, Fingerprint: key, FactsKey: scriptRewriteFactsKey(v), Kind: scriptplay.KindRewrite})
	return writeVolcengineJSON(scriptSharedTextScopePath(root, v, unit), pool)
}

func scriptMissingSharedText(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) (scriptSharedText, bool, error) {
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return scriptSharedText{}, false, err
	}
	defer release()
	texts, err := loadScriptSharedTextsLocked(root, v, unit)
	if err != nil {
		return scriptSharedText{}, false, err
	}
	pool, err := loadScriptVoicePoolLocked(root, v.PlanID, unit.poolID())
	if err != nil {
		return scriptSharedText{}, false, err
	}
	ready := map[string]bool{}
	for _, version := range pool.Versions {
		if _, ok := scriptVersionAudio(root, unit, v, version); ok {
			ready[scriptplay.Fingerprint(version.Text)] = true
		}
	}
	for _, entry := range texts.Versions {
		if !ready[entry.Fingerprint] && !entry.RenderedAudio[scriptFirstParagraphKey(v, entry.Text)] {
			return entry, true, nil
		}
	}
	return scriptSharedText{}, false, nil
}

func (s *Server) synthesizeSharedScriptText(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, text string, cap int, replace bool, retireRef ...string) error {
	key := scriptFirstParagraphKey(v, text)
	target := volcengineScriptUnit{Text: text, Key: key, Pool: unit.poolID(), Dir: filepath.Join(filepath.Dir(unit.Dir), key)}
	audio, err := s.ensureScriptFirstParagraph(ctx, tenant, room, 0, v, target, true)
	if err != nil {
		return err
	}
	_, err = commitScriptRewriteVersion(recordingCacheMediaRoot(), v, unit, scriptplay.Version{FactsKey: scriptRewriteFactsKey(v), PieceID: unit.poolID(), Fingerprint: scriptplay.Fingerprint(text), Text: text, VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model, Kind: scriptplay.KindRewrite, AudioRef: strconv.FormatInt(audio.AssetID, 10), DurationMS: audio.DurationMS, SourcePath: target.Dir}, cap, replace, retireRef...)
	if err == nil {
		err = markScriptSharedTextRendered(recordingCacheMediaRoot(), v, unit, text)
	}
	return err
}

func markScriptSharedTextRendered(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, text string) error {
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return err
	}
	defer release()
	pool, err := loadScriptSharedTextsLocked(root, v, unit)
	if err != nil {
		return err
	}
	for i := range pool.Versions {
		entry := &pool.Versions[i]
		if entry.Fingerprint == scriptplay.Fingerprint(text) {
			if entry.RenderedAudio == nil {
				entry.RenderedAudio = map[string]bool{}
			}
			entry.RenderedAudio[scriptFirstParagraphKey(v, text)] = true
		}
	}
	return writeVolcengineJSON(scriptSharedTextScopePath(root, v, unit), pool)
}
