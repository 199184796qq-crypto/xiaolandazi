package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

// 话术版本池的磁盘存储。
//
// 每个片段一份 pool.json，放在缓存目录下，记录这个片段在各音色下的音频版本。
// 先落文件而不是建表：不改变现有缓存结构，出问题只影响新链路，回退只需删目录。
var scriptVoicePoolMu sync.Mutex

// scriptVoiceVersion 是磁盘上的版本记录。
type scriptVoiceVersion struct {
	ID           string   `json:"id"`
	PieceID      string   `json:"piece_id"`
	Fingerprint  string   `json:"fingerprint"`
	VoiceID      string   `json:"voice_id"`
	Model        string   `json:"model"`
	Volume       int      `json:"volume,omitempty"`
	Kind         string   `json:"kind"`
	Seq          int      `json:"seq"`
	AudioRef     string   `json:"audio_ref"`
	PlayCount    int      `json:"play_count,omitempty"`
	LastPlayedAt string   `json:"last_played_at,omitempty"`
	Text         string   `json:"text,omitempty"`
	DurationMS   int64    `json:"duration_ms,omitempty"`
	SourcePath   string   `json:"source_path,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	PlayedTracks []string `json:"played_tracks,omitempty"`
	FactsKey     string   `json:"facts_key,omitempty"`
}

type scriptVoicePoolFile struct {
	PieceID  string               `json:"piece_id"`
	Versions []scriptVoiceVersion `json:"versions"`
}

func scriptVoicePoolDir(root string, planID int64) string {
	return filepath.Join(root, "script-voices", strconv.FormatInt(planID, 10))
}

func scriptVoicePoolPath(root string, planID int64, pieceID string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, pieceID)
	return filepath.Join(scriptVoicePoolDir(root, planID), safe+".json")
}

// loadScriptVoicePool 读取某个片段的版本池；文件不存在时返回空池。
func loadScriptVoicePool(root string, planID int64, pieceID string) (scriptplay.Pool, error) {
	scriptVoicePoolMu.Lock()
	defer scriptVoicePoolMu.Unlock()
	return loadScriptVoicePoolLocked(root, planID, pieceID)
}

func loadScriptVoicePoolLocked(root string, planID int64, pieceID string) (scriptplay.Pool, error) {
	path := scriptVoicePoolPath(root, planID, pieceID)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return scriptplay.Pool{}, nil
	}
	if err != nil {
		return scriptplay.Pool{}, err
	}
	var file scriptVoicePoolFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return scriptplay.Pool{}, err
	}
	pool := scriptplay.Pool{}
	for _, v := range file.Versions {
		pool.Versions = append(pool.Versions, scriptplay.Version{
			ID:           v.ID,
			PieceID:      v.PieceID,
			Fingerprint:  v.Fingerprint,
			VoiceID:      v.VoiceID,
			Model:        v.Model,
			Volume:       v.Volume,
			Kind:         scriptplay.Kind(v.Kind),
			Seq:          v.Seq,
			AudioRef:     v.AudioRef,
			PlayCount:    v.PlayCount,
			LastPlayedAt: v.LastPlayedAt,
			Text:         v.Text,
			DurationMS:   v.DurationMS,
			SourcePath:   v.SourcePath,
			CreatedAt:    v.CreatedAt, PlayedTracks: v.PlayedTracks, FactsKey: v.FactsKey,
		})
	}
	return pool, nil
}

// saveScriptVoicePool 原子写入某个片段的版本池。
func saveScriptVoicePool(root string, planID int64, pieceID string, pool scriptplay.Pool) error {
	scriptVoicePoolMu.Lock()
	defer scriptVoicePoolMu.Unlock()
	return saveScriptVoicePoolLocked(root, planID, pieceID, pool)
}

func saveScriptVoicePoolLocked(root string, planID int64, pieceID string, pool scriptplay.Pool) error {
	dir := scriptVoicePoolDir(root, planID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file := scriptVoicePoolFile{PieceID: pieceID}
	for _, v := range pool.Versions {
		file.Versions = append(file.Versions, scriptVoiceVersion{
			ID:           v.ID,
			PieceID:      v.PieceID,
			Fingerprint:  v.Fingerprint,
			VoiceID:      v.VoiceID,
			Model:        v.Model,
			Volume:       v.Volume,
			Kind:         string(v.Kind),
			Seq:          v.Seq,
			AudioRef:     v.AudioRef,
			PlayCount:    v.PlayCount,
			LastPlayedAt: v.LastPlayedAt,
			Text:         v.Text,
			DurationMS:   v.DurationMS,
			SourcePath:   v.SourcePath,
			CreatedAt:    v.CreatedAt, PlayedTracks: v.PlayedTracks, FactsKey: v.FactsKey,
		})
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	path := scriptVoicePoolPath(root, planID, pieceID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// addScriptVoiceVersion 往池子里追加一个版本并落盘。
func addScriptVoiceVersion(root string, planID int64, pieceID string, version scriptplay.Version) error {
	scriptVoicePoolMu.Lock()
	defer scriptVoicePoolMu.Unlock()
	pool, err := loadScriptVoicePoolLocked(root, planID, pieceID)
	if err != nil {
		return err
	}
	version.PieceID = pieceID
	for _, existing := range pool.Versions {
		if existing.AudioRef == version.AudioRef && existing.VoiceID == version.VoiceID && existing.Model == version.Model {
			return nil
		}
	}
	if version.CreatedAt == "" {
		version.CreatedAt = time.Now().Format(time.RFC3339Nano)
	}
	if err := pool.Add(version); err != nil {
		return err
	}
	return saveScriptVoicePoolLocked(root, planID, pieceID, pool)
}

// invalidateScriptVoicePool 按指纹清理版本池，返回被判失效的版本。
func invalidateScriptVoicePool(root string, planID int64, pieceID, keepFingerprint string) ([]scriptplay.Version, error) {
	scriptVoicePoolMu.Lock()
	defer scriptVoicePoolMu.Unlock()
	pool, err := loadScriptVoicePoolLocked(root, planID, pieceID)
	if err != nil {
		return nil, err
	}
	stale := pool.Invalidate(keepFingerprint)
	if len(stale) == 0 {
		return nil, nil
	}
	if err := saveScriptVoicePoolLocked(root, planID, pieceID, pool); err != nil {
		return nil, err
	}
	return stale, nil
}

// recordScriptVoiceVersion 把一次成功合成登记到片段版本池。
//
// 只登记，不改变播放：当前播放仍走原段落缓存，避免在验证前改动发声路径。
// 失败只记日志，不影响直播。
func recordScriptVoiceVersion(root string, planID int64, unit volcengineScriptUnit, v model.LiveAgentPlanVersion, audio volcengineCachedAudio) {
	if audio.AssetID <= 0 || unit.Key == "" || (audio.VoiceID != "" && audio.VoiceID != v.VoiceIdentity.VoiceID) {
		return
	}
	scriptVoicePoolMu.Lock()
	defer scriptVoicePoolMu.Unlock()
	pool, err := loadScriptVoicePoolLocked(root, planID, unit.poolID())
	if err != nil {
		log.Printf("script pool register load failed: %v", err)
		return
	}
	ref := strconv.FormatInt(audio.AssetID, 10)
	fingerprint := scriptplay.Fingerprint(unit.Text)
	for _, existing := range pool.Versions {
		if existing.VoiceID == v.VoiceIdentity.VoiceID && existing.Model == v.VoiceIdentity.Model && (existing.AudioRef == ref || (existing.Fingerprint == fingerprint && existing.SourcePath == unit.Dir)) {
			return
		}
	}
	version := scriptplay.Version{PieceID: unit.poolID(), Fingerprint: fingerprint, VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model, Kind: scriptplay.KindOriginal, AudioRef: ref, Text: unit.Text, DurationMS: audio.DurationMS, SourcePath: unit.Dir, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := pool.Add(version); err != nil {
		return
	}
	if err := saveScriptVoicePoolLocked(root, planID, unit.poolID(), pool); err != nil {
		log.Printf("script pool register save failed: %v", err)
	}
}

// scriptVoiceVersionCap 读取方案里配置的每音色版本上限，未配置时用默认 6。
func scriptVoiceVersionCap(v model.LiveAgentPlanVersion) int {
	if script, ok := recordingScriptFromContext(v.GenerationContext); ok {
		return scriptplay.VersionCap(script.EffectiveVersionCap())
	}
	return scriptplay.VersionCap(0)
}

// scriptPoolRotation 记录每个片段本轮已经播过哪些版本。
//
// 内存态：进程重启或换场后重新洗牌，不影响正确性。
var scriptPoolRotation = struct {
	sync.Mutex
	used map[string]map[string]bool
}{used: map[string]map[string]bool{}}

// scriptPoolPickVersion 从版本池里挑一个本轮还没播过的版本；池子用尽则重置。
func scriptPoolPickVersion(root string, planID int64, pieceID, voiceID, avoidOpening string, rng *rand.Rand) (scriptplay.Version, bool) {
	pool, err := loadScriptVoicePool(root, planID, pieceID)
	if err != nil {
		return scriptplay.Version{}, false
	}
	versions := pool.VoiceVersions(voiceID)
	if len(versions) == 0 {
		return scriptplay.Version{}, false
	}
	key := fmt.Sprintf("%d|%s|%s", planID, pieceID, voiceID)
	scriptPoolRotation.Lock()
	defer scriptPoolRotation.Unlock()
	used := scriptPoolRotation.used[key]
	if used == nil || len(used) >= len(versions) {
		used = map[string]bool{}
		scriptPoolRotation.used[key] = used
	}
	var fresh []scriptplay.Version
	for _, v := range versions {
		if !used[scriptVersionKey(v)] {
			fresh = append(fresh, v)
		}
	}
	if len(fresh) == 0 {
		return scriptplay.Version{}, false
	}
	// 优先挑最久没播的那一条：跨场、重启后也不会马上重复同一条音频。
	oldest := fresh[0].LastPlayedAt
	for _, v := range fresh {
		if v.LastPlayedAt < oldest {
			oldest = v.LastPlayedAt
		}
	}
	var candidates []scriptplay.Version
	for _, v := range fresh {
		if v.LastPlayedAt == oldest {
			candidates = append(candidates, v)
		}
	}
	// 相邻两条不要用同一类开场：优先挑开场不同的版本，都相同才退回去。
	if avoidOpening != "" {
		var distinct []scriptplay.Version
		for _, v := range candidates {
			if scriptOpeningOf(v.Text) != avoidOpening {
				distinct = append(distinct, v)
			}
		}
		if len(distinct) > 0 {
			candidates = distinct
		}
	}
	pick := candidates[rng.Intn(len(candidates))]
	used[scriptVersionKey(pick)] = true
	return pick, true
}

// scriptVersionKey 生成版本在轮换表里的唯一键：没有 ID 时用音色+序号兜底。
func scriptVersionKey(v scriptplay.Version) string {
	if v.ID != "" {
		return v.ID
	}
	return v.Model + "|" + v.VoiceID + "|" + v.AudioRef
}

// scriptPoolAudio 按版本记录还原一条可播放的音频。
//
// 版本只存了目录，真正的音频对象还在段落缓存的 cache.json 里，从这里读回来，
// 时间轴和分块信息都跟着一起取到，播放侧不需要特殊处理。
// scriptVoicePlan 是播放时使用的音色安排。
type scriptVoicePlan struct {
	Current string
	Pending string
	Block   int
	Planner scriptplay.VoicePlanner
}

// EffectiveVoices 返回方案选中的音色；未配置时返回空（沿用方案当前音色）。
func (s recordingScript) EffectiveVoices() []recordingScriptVoice {
	return s.Voices
}

// scriptVoicesForPlayback 取本方案要轮换的音色列表，未配置时退化为当前音色。
func scriptVoicesForPlayback(v model.LiveAgentPlanVersion) []recordingScriptVoice {
	if script, ok := recordingScriptFromContext(v.GenerationContext); ok {
		if voices := script.EffectiveVoices(); len(voices) > 0 {
			for _, voice := range voices {
				if voice.VoiceID == v.VoiceIdentity.VoiceID && (voice.Model == "" || voice.Model == v.VoiceIdentity.Model) {
					return voices
				}
			}
			return append([]recordingScriptVoice{{VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model}}, voices...)
		}
	}
	if strings.TrimSpace(v.VoiceIdentity.VoiceID) == "" {
		return nil
	}
	return []recordingScriptVoice{{VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model}}
}

// scriptVoiceReady 统计某个音色在全部片段里已经备齐多少条（每片段至少一个版本）。
func scriptVoiceReady(root string, planID int64, units []volcengineScriptUnit, voiceID string) (ready, total int) {
	total = len(units)
	for _, unit := range units {
		pool, err := loadScriptVoicePool(root, planID, unit.poolID())
		if err != nil {
			continue
		}
		if len(pool.VoiceVersions(voiceID)) > 0 {
			ready++
		}
	}
	return ready, total
}

// scriptNextPlaybackVoice 尝试切到下一个音色。
//
// 只有目标音色在全部片段上都有至少一个版本（备齐）才允许切换；
// 否则返回当前下标，播放继续用当前音色，相当于"准备中"。
func scriptNextPlaybackVoice(voices []recordingScriptVoice, current int, v model.LiveAgentPlanVersion, units []volcengineScriptUnit) (int, bool) {
	if len(voices) < 2 {
		return current, false
	}
	next := (current + 1) % len(voices)
	target := scriptVersionForVoice(v, voices[next])
	ready, total := 0, len(units)
	for _, unit := range units {
		if len(scriptCompatiblePool(recordingCacheMediaRoot(), target, unit).Versions) > 0 {
			ready++
		}
	}
	if total > 0 && ready >= total {
		return next, true
	}
	return current, false
}

// scriptVoiceBlockLeft 计算当前音色这一块还能播几轮。
//
// 块长 = max(min_voice_block, 该音色下最少版本数)，保证一块内版本不重复。
func scriptVoiceBlockLeft(voices []recordingScriptVoice, current int, v model.LiveAgentPlanVersion) int {
	if current >= 0 && current < len(voices) {
		v = scriptVersionForVoice(v, voices[current])
	}
	units, _ := scriptFirstUnits(v, "")
	return scriptVoiceBlockLength(v, units)
}

// purgePreviousVoiceAssets 保留旧快照仍可能使用的已付费音频。
//
// 缓存键里带音色，旧音色的音频改音色后永远命中不了，留着只会占空间、占版本名额。
// 只在音色真的变了才动手；清理失败只记日志，不影响保存结果。
func (s *Server) purgePreviousVoiceAssets(ctx context.Context, tenantID, planID int64, current model.LiveAgentPlanVersion) {
	// Saving another voice must not delete paid media used by active snapshots.
	// Physical cleanup requires the separate retention/reference-aware sweeper.
}

// scriptPlayUnitsFor 按某个版本的播放顺序算出段落单元（只用于定位缓存目录）。
func scriptPlayUnitsFor(v model.LiveAgentPlanVersion, script recordingScript, dir string) []volcengineScriptUnit {
	var units []volcengineScriptUnit
	for _, tab := range script.Tabs {
		for _, entry := range tab.Entries {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			key := scriptFirstParagraphKey(v, text)
			units = append(units, volcengineScriptUnit{Text: text, Key: key, Dir: filepath.Join(dir, key)})
		}
	}
	return units
}

// markScriptVersionPlayed 给刚推送出去的那条版本累加播放次数和时间。
//
// 按音频 ID 精确匹配，避免认错版本；失败只记日志，不影响播放。
func markScriptVersionPlayed(root string, planID int64, pieceKey, audioRef string) {
	if pieceKey == "" || audioRef == "" {
		return
	}
	scriptVoicePoolMu.Lock()
	defer scriptVoicePoolMu.Unlock()
	pool, err := loadScriptVoicePoolLocked(root, planID, pieceKey)
	if err != nil {
		return
	}
	changed := false
	for i := range pool.Versions {
		if pool.Versions[i].AudioRef == audioRef {
			pool.Versions[i].PlayCount++
			pool.Versions[i].LastPlayedAt = time.Now().Format(time.RFC3339)
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := saveScriptVoicePoolLocked(root, planID, pieceKey, pool); err != nil {
		log.Printf("script voice play count save failed plan=%d piece=%s: %v", planID, shortKey(pieceKey), err)
	}
}

// pruneScriptVoicePools 删掉池子里不属于当前脚本的片段文件。
//
// 话术改一句，键就变；旧文件会一直留着占空间、也把"片段数"统计撑大。
// 只删文件名不在当前片段集合里的池文件，当前片段一个都不动。
func pruneScriptVoicePools(planID int64, units []volcengineScriptUnit) {
	root := recordingCacheMediaRoot()
	scriptVoicePoolMu.Lock()
	defer scriptVoicePoolMu.Unlock()
	for _, unit := range units {
		if unit.poolID() == unit.Key || unit.Key == "" {
			continue
		}
		old, err := loadScriptVoicePoolLocked(root, planID, unit.Key)
		if err != nil || len(old.Versions) == 0 {
			continue
		}
		current, err := loadScriptVoicePoolLocked(root, planID, unit.poolID())
		if err != nil {
			continue
		}
		for _, version := range old.Versions {
			exists := false
			for _, kept := range current.Versions {
				if kept.AudioRef == version.AudioRef && kept.VoiceID == version.VoiceID && kept.Model == version.Model {
					exists = true
					break
				}
			}
			if !exists {
				version.PieceID = unit.poolID()
				current.Versions = append(current.Versions, version)
			}
		}
		_ = saveScriptVoicePoolLocked(root, planID, unit.poolID(), current)
	}
	// Keep the source pools and all paid media. An old live snapshot or an undo
	// can still reference them. Never implement retention by eager deletion here.
}

// scriptPoolPickForTest 暴露给测试：只挑版本，不读音频。
func scriptPoolPickForTest(root string, planID int64, pieceKey, voiceID string, rng *rand.Rand) (scriptplay.Version, bool) {
	return scriptPoolPickVersion(root, planID, pieceKey, voiceID, "", rng)
}

// scriptOpeningOf 取开头一小段做"开场指纹"，用于避免相邻两条开场撞车。
func scriptOpeningOf(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 12 {
		runes = runes[:12]
	}
	return string(runes)
}

// hasRetirableVersion 判断该音色下是否有版本已经播够次数、可以被换掉。
func hasRetirableVersion(pool scriptplay.Pool, voiceID string, minPlays int) bool {
	for _, v := range pool.VoiceVersions(voiceID) {
		if v.Kind == scriptplay.KindRewrite && v.PlayCount >= minPlays {
			return true
		}
	}
	return false
}
