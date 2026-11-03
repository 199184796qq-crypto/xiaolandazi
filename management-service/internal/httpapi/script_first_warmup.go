package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

// 方案级首段预热：保存方案时把首段合成到方案级缓存，开播时复制到房间缓存，
// 免得首次开播干等 10 秒以上。见 docs/2026-11-02-功能-方案级首段预热缓存.md。
const (
	planWarmupCacheName = "plan-paragraphs"
	planWarmupTimeout   = 60 * time.Second
)

func scriptFirstNamespace(model string) string {
	if model == ttsgateway.VolcengineICL2 {
		return "volcengine-longtext-v1"
	}
	return "script-first-v1"
}

// planCacheDir 不用 roomID=0 复用房间布局，而是单独放在 plans 段下：
//
//	房间级  <root>/<tenant>/<room>/<plan>/recording-cache/<ns>/paragraphs/
//	方案级  <root>/<tenant>/plans/<plan>/recording-cache/<ns>/plan-paragraphs/
//
// 多一层 plans 是为了不和任何房间号撞名，运维看目录也能一眼分清。
func planCacheDir(tenant, plan int64, model string) (string, bool) {
	if tenant <= 0 || plan <= 0 {
		log.Printf("plan cache scope invalid tenant=%d plan=%d", tenant, plan)
		return "", false
	}
	return filepath.Join(
		recordingCacheMediaRoot(),
		strconv.FormatInt(tenant, 10),
		"plans",
		strconv.FormatInt(plan, 10),
		"recording-cache",
		scriptFirstNamespace(model),
		planWarmupCacheName,
	), true
}

// warmPlanFirstParagraph waits for the opening anchor before a saved version
// becomes effective. The request (including cache/queue waits) has a 60s limit.
func (s *Server) warmPlanFirstParagraph(ctx context.Context, tenant, plan int64, v model.LiveAgentPlanVersion) error {
	return waitPlanFirstParagraph(ctx, tenant, plan, v, func(work context.Context, unit volcengineScriptUnit) error {
		audio, err := s.ensureScriptFirstParagraph(work, tenant, 0, 0, v, unit, true)
		if err != nil {
			return err
		}
		if audio.AssetID <= 0 || audio.DurationMS <= 0 || !s.scriptAudioAvailable(work, tenant, audio.AssetID) {
			return errors.New("开场声音缓存尚未就绪")
		}
		return nil
	})
}

func waitPlanFirstParagraph(ctx context.Context, tenant, plan int64, v model.LiveAgentPlanVersion, ensure func(context.Context, volcengineScriptUnit) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !usesScriptFirstParagraphs(v) {
		return nil
	}
	if tenant <= 0 || plan <= 0 || v.TenantID != tenant || v.PlanID != plan || v.VoiceIdentity.Rate <= 0 {
		return errors.New("方案首段缓存身份无效")
	}
	dir, ok := planCacheDir(tenant, plan, v.VoiceIdentity.Model)
	if !ok {
		return errors.New("方案首段缓存位置无效")
	}
	units, err := scriptFirstUnits(v, dir)
	if err != nil {
		return err
	}
	if len(units) == 0 {
		return errors.New("方案没有可生成的开场话术")
	}
	work, cancel := context.WithTimeout(ctx, planWarmupTimeout)
	defer cancel()
	started := time.Now()
	result := make(chan error, 1)
	// Providers may retain an already-paid result after cancellation. Never let
	// such a detached drain delay the response or publish a late saved version.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("plan warmup panic tenant=%d plan=%d: %v", tenant, plan, r)
				result <- errors.New("开场声音生成失败")
			}
		}()
		result <- ensure(work, units[0])
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-work.Done():
			return work.Err()
		case err := <-result:
			if work.Err() != nil {
				return work.Err()
			}
			if err != nil {
				log.Printf("plan warmup failed tenant=%d plan=%d unit=%s error=%v", tenant, plan, shortKey(units[0].Key), err)
				return err
			}
			log.Printf("plan warmup ready tenant=%d plan=%d unit=%s elapsed=%s", tenant, plan, shortKey(units[0].Key), time.Since(started).Round(time.Millisecond))
			return nil
		case <-ticker.C:
			reportAnchorAnalysis(ctx, 94, "正在准备开场声音缓存，最多等待60秒")
		}
	}
}

// hydrateFirstParagraphFromPlan 开播时把方案级首段复制进房间缓存。
// 房间已有付费缓存时不覆盖；缺失时走原合成，不确定的已付费结果保留并报错。
func (s *Server) hydrateFirstParagraphFromPlan(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) error {
	if !usesScriptFirstParagraphs(v) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Any existing room directory belongs to the normal paid-source recovery
	// path. Never replace it, including a supplier-specific pending checkpoint.
	if _, err := os.Lstat(unit.Dir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, ok := planCacheDir(tenant, v.PlanID, v.VoiceIdentity.Model)
	if !ok {
		return errors.New("方案首段缓存身份无效")
	}
	source := filepath.Join(dir, unit.Key)
	// Use the same source lock as synthesis. If a save is still generating,
	// await that result rather than buy the same first paragraph in a new room.
	unlock, err := lockVolcengineParagraph(ctx, source)
	if err != nil {
		return err
	}
	defer unlock()
	roomUnlock, err := lockVolcengineParagraph(ctx, unit.Dir)
	if err != nil {
		return err
	}
	defer roomUnlock()
	if _, err = os.Lstat(unit.Dir); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ready, err := planWarmupSourceReady(v, unit, source)
	if err != nil || !ready {
		// A damaged prewarm is optional. The foreground producer now restores
		// its matching archive or buys a replacement rather than failing startup.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = copyDirAtomic(source, unit.Dir); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	log.Printf("plan warmup hydrated tenant=%d room=%d plan=%d unit=%s", tenant, room, v.PlanID, shortKey(unit.Key))
	return nil
}

// Missing prewarm means normal live synthesis. An existing paid intent with an
// uncertain/corrupt result must not silently fall through to a second purchase.
func planWarmupSourceReady(v model.LiveAgentPlanVersion, unit volcengineScriptUnit, source string) (bool, error) {
	if v.VoiceIdentity.Model == ttsgateway.VolcengineICL2 {
		state := filepath.Join(source, "job.json")
		if _, err := os.Lstat(state); errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		_, ready := ttsgateway.LoadLongTextResult(ttsgateway.SynthesizeRequest{Model: v.VoiceIdentity.Model, VoiceID: v.VoiceIdentity.VoiceID, Text: unit.Text, Rate: v.VoiceIdentity.Rate, LongText: &ttsgateway.LongTextJob{StatePath: state, SourceVoiceID: v.VoiceIdentity.VoiceID}})
		if !ready {
			return false, errors.New("方案首段长文本任务结果尚未确认，已保留原任务，未重复购买；请重新保存方案恢复预热")
		}
		return true, nil
	}
	var state scriptFirstPaidState
	err := readVolcengineJSON(filepath.Join(source, "paragraph-job.json"), &state)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !state.Complete || state.Key != unit.Key {
		return false, errors.New("方案首段合成结果尚未确认，已保留原任务，未重复购买")
	}
	f, err := os.Open(filepath.Join(source, "paragraph.wav"))
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, ttsgateway.MaxLongTextAudioBytes+45))
	if err != nil || n <= 44 || n > ttsgateway.MaxLongTextAudioBytes+44 || hex.EncodeToString(h.Sum(nil)) != state.SourceSHA {
		return false, errors.New("已付费方案首段校验失败，未重复购买")
	}
	return true, nil
}

func shortKey(key string) string {
	if len(key) > 16 {
		return key[:16]
	}
	return key
}

// copyDirAtomic 先复制到同级临时目录再整体改名，避免半份文件被当成有效缓存。
func copyDirAtomic(source, target string) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.MkdirTemp(parent, ".warmup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	if err := copyDir(source, temporary); err != nil {
		return err
	}
	// Other normal producers hold the target paragraph lock as well. The final
	// recheck also protects a newly-created destination: paid data is never deleted.
	if _, err := os.Lstat(target); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	return nil
}

// copyDir 只搬音频与元数据，杂物不进房间缓存。
func copyDir(source, target string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") && !strings.HasSuffix(entry.Name(), ".wav") {
			continue
		}
		if !entry.Type().IsRegular() {
			return errors.New("方案首段缓存包含非普通文件")
		}
		if err := copyFile(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
