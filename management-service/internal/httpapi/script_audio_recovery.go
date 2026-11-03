package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
)

const scriptArchiveRecoveryTimeout = 8 * time.Second

// Signing an object URL does not prove its audio still exists. Check a small
// header before accepting a cached track, with a bounded storage deadline.
func (s *Server) scriptAudioAvailable(ctx context.Context, tenant, id int64) bool {
	if s.store == nil || s.assetStorage == nil {
		return false
	}
	work, done := context.WithTimeout(ctx, scriptArchiveRecoveryTimeout)
	defer done()
	a, err := s.store.GetMediaAsset(work, tenant, id)
	if err != nil {
		return false
	}
	store, err := s.assetStorage.For(a.StorageDriver)
	if err != nil {
		return false
	}
	r, err := store.Open(work, a.ObjectKey)
	if err != nil {
		return false
	}
	defer r.Close()
	var header [44]byte
	_, err = io.ReadFull(r, header[:])
	return err == nil && work.Err() == nil && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE"
}

func replaceScriptPoolAudio(root string, v model.LiveAgentPlanVersion, u volcengineScriptUnit, old int64, audio volcengineCachedAudio) error {
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return err
	}
	defer release()
	pool, err := loadScriptVoicePoolLocked(root, v.PlanID, u.poolID())
	if err != nil {
		return err
	}
	changed := false
	for i, x := range pool.Versions {
		if x.AudioRef == strconv.FormatInt(old, 10) && x.VoiceID == v.VoiceIdentity.VoiceID && x.Model == v.VoiceIdentity.Model {
			pool.Versions[i].AudioRef = strconv.FormatInt(audio.AssetID, 10)
			pool.Versions[i].DurationMS = audio.DurationMS
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return saveScriptVoicePoolLocked(root, v.PlanID, u.poolID(), pool)
}

// Called only while holding the exact paragraph's shared process/file lock.
// Preserve evidence, but never let an unusable old payment marker fence a new
// authorized purchase forever. Active provider calls retain this same lock.
func retireUnavailableScriptFiles(ctx context.Context, unit volcengineScriptUnit, names ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var backup string
	for _, name := range names {
		switch name {
		case "cache.json", "paragraph-job.json", "paragraph.wav", "job.json", "job.json.wav":
		default:
			return errors.New("invalid paragraph recovery file")
		}
		path := filepath.Join(unit.Dir, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if backup == "" {
			var err error
			backup, err = os.MkdirTemp(unit.Dir, ".unavailable-")
			if err != nil {
				return err
			}
		}
		if err := os.Rename(path, filepath.Join(backup, name)); err != nil {
			return err
		}
	}
	return nil
}

func loadRecoverableScriptCache(ctx context.Context, unit volcengineScriptUnit) (volcengineParagraphCache, error) {
	cache, err := loadVolcengineParagraphCache(filepath.Join(unit.Dir, "cache.json"), unit.Key)
	if err == nil {
		return cache, nil
	}
	if err := retireUnavailableScriptFiles(ctx, unit, "cache.json"); err != nil {
		return cache, err
	}
	log.Printf("script cache index unavailable key=%s; recover archive or synthesize", shortKey(unit.Key))
	return volcengineParagraphCache{Key: unit.Key, Audio: map[int]volcengineCachedAudio{}}, nil
}

func scriptPaidSourceReady(unit volcengineScriptUnit) bool {
	var state scriptFirstPaidState
	if readVolcengineJSON(filepath.Join(unit.Dir, "paragraph-job.json"), &state) != nil || !state.Complete || state.Key != unit.Key {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(unit.Dir, "paragraph.wav"))
	sum := sha256.Sum256(raw)
	return err == nil && len(raw) > 0 && hex.EncodeToString(sum[:]) == state.SourceSHA
}

// Reading an archive is not a model/TTS request. It has a short deadline so a
// broken object store cannot indefinitely delay a fresh paid synthesis.
func (s *Server) recoverScriptParagraphArchive(ctx context.Context, tenant int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, cache *volcengineParagraphCache) (volcengineCachedAudio, bool) {
	if s.store == nil || s.assetStorage == nil || ctx.Err() != nil {
		return volcengineCachedAudio{}, false
	}
	work, done := context.WithTimeout(ctx, scriptArchiveRecoveryTimeout)
	defer done()
	assets, err := s.store.FindScriptParagraphAssets(work, tenant, v.PlanID, unit.Key)
	if err != nil {
		return volcengineCachedAudio{}, false
	}
	for _, asset := range assets {
		audio, ok := recoverScriptArchiveObject(work, s.assetStorage, tenant, v, unit, asset)
		if !ok {
			continue
		}
		cache.Audio[100] = audio
		if ctx.Err() != nil {
			return volcengineCachedAudio{}, false
		}
		if err := writeVolcengineJSON(filepath.Join(unit.Dir, "cache.json"), *cache); err != nil {
			return volcengineCachedAudio{}, false
		}
		log.Printf("script archive recovered tenant=%d plan=%d key=%s asset=%d", tenant, v.PlanID, shortKey(unit.Key), audio.AssetID)
		return audio, true
	}
	return volcengineCachedAudio{}, false
}

func recoverScriptArchiveObject(ctx context.Context, registry *storage.Registry, tenant int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, a model.MediaAsset) (volcengineCachedAudio, bool) {
	m := a.Metadata
	if ctx.Err() != nil || a.ID <= 0 || a.TenantID != tenant || a.Status != "active" || a.AssetType != "generated_voice" || a.DurationMS == nil || *a.DurationMS == 0 ||
		m["paragraph_key"] != unit.Key || m["voice_id"] != v.VoiceIdentity.VoiceID || m["tts_model"] != v.VoiceIdentity.Model ||
		fmt.Sprint(m["plan_id"]) != fmt.Sprint(v.PlanID) || m["original_transcript"] != strings.TrimSpace(unit.Text) || m["untimed_transcript"] != true ||
		a.SizeBytes <= 44 || a.SizeBytes > uint64(ttsgateway.MaxLongTextAudioBytes+44) || len(a.ChecksumSHA256) != 64 {
		return volcengineCachedAudio{}, false
	}
	store, err := registry.For(a.StorageDriver)
	if err != nil {
		return volcengineCachedAudio{}, false
	}
	r, err := store.Open(ctx, a.ObjectKey)
	if err != nil {
		return volcengineCachedAudio{}, false
	}
	defer r.Close()
	h := sha256.New()
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return volcengineCachedAudio{}, false
	}
	_, _ = h.Write(header[:])
	n, err := io.Copy(h, io.LimitReader(r, int64(a.SizeBytes)+1-12))
	n += 12
	if err != nil || ctx.Err() != nil || n != int64(a.SizeBytes) || hex.EncodeToString(h.Sum(nil)) != a.ChecksumSHA256 {
		return volcengineCachedAudio{}, false
	}
	return volcengineCachedAudio{AssetID: a.ID, DurationMS: int64(*a.DurationMS), VoiceID: v.VoiceIdentity.VoiceID, UntimedTranscript: true}, true
}
