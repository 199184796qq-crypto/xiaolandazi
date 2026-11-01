package httpapi

import (
	"context"
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
// 房间已有付费缓存时不覆盖；方案级缺失或不完整时静默退回现场合成。
func (s *Server) hydrateFirstParagraphFromPlan(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) {
	if !usesScriptFirstParagraphs(v) {
		return
	}
	// 房间已有付费检查点：不覆盖。
	if _, err := os.Stat(filepath.Join(unit.Dir, "paragraph-job.json")); err == nil {
		return
	}
	dir, ok := planCacheDir(tenant, v.PlanID, v.VoiceIdentity.Model)
	if !ok {
		return
	}
	source := filepath.Join(dir, unit.Key)
	if _, err := os.Stat(filepath.Join(source, "paragraph-job.json")); err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(source, "paragraph.wav")); err != nil {
		return
	}
	if err := copyDirAtomic(source, unit.Dir); err != nil {
		log.Printf("plan warmup hydrate failed tenant=%d room=%d plan=%d error=%v", tenant, room, v.PlanID, err)
		return
	}
	log.Printf("plan warmup hydrated tenant=%d room=%d plan=%d unit=%s", tenant, room, v.PlanID, shortKey(unit.Key))
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
	temporary := target + ".warmup-tmp"
	_ = os.RemoveAll(temporary)
	if err := copyDir(source, temporary); err != nil {
		_ = os.RemoveAll(temporary)
		return err
	}
	// 目标已存在时先删掉，保证 rename 原子生效。
	if _, err := os.Stat(target); err == nil {
		if err := os.RemoveAll(target); err != nil {
			_ = os.RemoveAll(temporary)
			return err
		}
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.RemoveAll(temporary)
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
			if err := copyDir(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") && !strings.HasSuffix(entry.Name(), ".wav") {
			continue
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
