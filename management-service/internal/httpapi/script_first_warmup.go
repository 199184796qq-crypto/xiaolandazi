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
	planWarmupTimeout   = 5 * time.Minute
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

// warmPlanFirstParagraph 只合成首段。文本或声音没变时 key 命中，不重复计费。
// 异步执行，失败只记日志，绝不影响保存结果。
func (s *Server) warmPlanFirstParagraph(tenant, plan int64, v model.LiveAgentPlanVersion) {
	if !usesScriptFirstParagraphs(v) {
		return
	}
	if v.VoiceIdentity.Rate == 0 {
		v.VoiceIdentity.Rate = 1
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("plan warmup panic tenant=%d plan=%d: %v", tenant, plan, r)
			}
		}()
		dir, ok := planCacheDir(tenant, plan, v.VoiceIdentity.Model)
		if !ok {
			return
		}
		units, err := scriptFirstUnits(v, dir)
		if err != nil || len(units) == 0 {
			if err != nil {
				log.Printf("plan warmup units failed tenant=%d plan=%d: %v", tenant, plan, err)
			}
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), planWarmupTimeout)
		defer cancel()
		started := time.Now()
		if _, err = s.ensureScriptFirstParagraph(ctx, tenant, 0, 0, v, units[0], true); err != nil {
			log.Printf("plan warmup failed tenant=%d plan=%d unit=%s error=%v",
				tenant, plan, shortKey(units[0].Key), err)
			return
		}
		log.Printf("plan warmup ready tenant=%d plan=%d unit=%s elapsed=%s",
			tenant, plan, shortKey(units[0].Key), time.Since(started).Round(time.Millisecond))
	}()
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
		return err
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
