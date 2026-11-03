package scriptplay

import (
	"errors"
	"sort"
)

// Kind 是版本的来源类型。
type Kind string

const (
	KindOriginal Kind = "original"
	KindRewrite  Kind = "rewrite"
)

// Version 是一段已经合成好的音频。
type Version struct {
	ID          string
	PieceID     string
	Fingerprint string // 合成时片段文本的指纹
	VoiceID     string
	Model       string
	Volume      int
	Kind        Kind
	Seq         int // 该音色下的第几个，从 1 开始
	AudioRef    string
	// PlayCount / LastPlayedAt 记录这条版本被播过几次、最后一次什么时候播的。
	PlayCount    int
	LastPlayedAt string
	// Text 是这条音频实际对应的文本（原文或改写后），便于排查重复与审计。
	Text string
	// DurationMS 与 SourcePath 让播放侧能直接复用这条音频，不必再查段落缓存。
	DurationMS      int64
	SourcePath      string
	CreatedAt       string
	FactsKey        string
	PlayedTracks    []string
	CompleteCount   int
	CompletedTracks []string
}

// Pool 是某个片段在所有音色下的版本集合。
type Pool struct {
	Versions []Version
}

// VoiceVersions 返回某个音色下的全部版本，按 Seq 升序。
func (p Pool) VoiceVersions(voiceID string) []Version {
	var out []Version
	for _, v := range p.Versions {
		if v.VoiceID == voiceID {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// Voices 返回池子里出现过的音色，按首次出现顺序。
func (p Pool) Voices() []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range p.Versions {
		if v.VoiceID == "" || seen[v.VoiceID] {
			continue
		}
		seen[v.VoiceID] = true
		out = append(out, v.VoiceID)
	}
	return out
}

// HasOriginal 判断某音色下是否已有原始版本。
func (p Pool) HasOriginal(voiceID string) bool {
	for _, v := range p.Versions {
		if v.VoiceID == voiceID && v.Kind == KindOriginal {
			return true
		}
	}
	return false
}

// NextKind 决定下一次该片段在某音色下应该生成什么。
//
// 规则：还没有原始版本 → 生成原始；已有原始 → 都是改写。
// 原始生成失败时由调用方改为请求改写，这里只表达期望。
func (p Pool) NextKind(voiceID string) Kind {
	if !p.HasOriginal(voiceID) {
		return KindOriginal
	}
	return KindRewrite
}

// NextSeq 返回某音色下下一个版本序号。
func (p Pool) NextSeq(voiceID string) int {
	n := 0
	for _, v := range p.VoiceVersions(voiceID) {
		if v.Seq > n {
			n = v.Seq
		}
	}
	return n + 1
}

// CanGenerate 判断某音色下是否还能继续生成。
func (p Pool) CanGenerate(voiceID string, cap int) bool {
	return len(p.VoiceVersions(voiceID)) < VersionCap(cap)
}

// Add 追加一个版本，指纹或音色缺失时报错。
func (p *Pool) Add(v Version) error {
	if v.PieceID == "" || v.VoiceID == "" || v.Fingerprint == "" || v.AudioRef == "" {
		return errors.New("版本信息不完整")
	}
	if v.Seq <= 0 {
		v.Seq = p.NextSeq(v.VoiceID)
	}
	p.Versions = append(p.Versions, v)
	return nil
}

// Invalidate 按指纹清理：指纹为空的版本（历史数据）不动。
//
// 返回被标记为失效的版本，调用方据此安排 7 天后再物理删除。
func (p *Pool) Invalidate(keepFingerprint string) []Version {
	var stale []Version
	var kept []Version
	for _, v := range p.Versions {
		if v.Fingerprint != "" && v.Fingerprint != keepFingerprint {
			stale = append(stale, v)
			continue
		}
		kept = append(kept, v)
	}
	p.Versions = kept
	return stale
}

// VoiceState 是音色切换的状态机。
type VoiceState string

const (
	// VoiceActive 是正在播放的音色。
	VoiceActive VoiceState = "active"
	// VoicePreparing 是已请求但在等资产备齐。
	VoicePreparing VoiceState = "preparing"
)

// Switch 描述一次音色切换请求。
type Switch struct {
	Target    string
	State     VoiceState
	Ready     int // 已备齐的片段数
	Total     int // 需要备齐的片段数
	BlockLeft int // 当前音色块还剩几轮
}

// BeginSwitch 发起切换。资产未备齐时进入 preparing，继续用当前音色播放。
func BeginSwitch(current, target string, ready, total int) Switch {
	if target == "" || target == current {
		return Switch{Target: current, State: VoiceActive, Ready: ready, Total: total}
	}
	if total > 0 && ready >= total {
		return Switch{Target: target, State: VoiceActive, Ready: ready, Total: total}
	}
	return Switch{Target: target, State: VoicePreparing, Ready: ready, Total: total}
}

// Progress 返回准备进度文本，便于控制台显示。
func (s Switch) Progress() string {
	if s.State != VoicePreparing {
		return ""
	}
	return "准备中"
}

// VoicePlanner 决定本轮该用哪个音色。
//
// 规则：当前音色块没走完就继续用它；走完后若目标音色已备齐则切过去，
// 未备齐则继续用当前音色并保持准备状态。
type VoicePlanner struct {
	Current   string
	Pending   string
	BlockLeft int
}

// Tick 推进一轮，返回本轮使用的音色与是否仍在准备中。
func (p *VoicePlanner) Tick(target string, ready, total int, blockLength int) (string, bool) {
	if blockLength < 1 {
		blockLength = 1
	}
	if p.Current == "" {
		p.Current = target
		p.BlockLeft = blockLength
		return p.Current, false
	}
	// 还在本音色块里：先播够这一轮，再把剩余轮数减一。
	if p.BlockLeft > 0 {
		p.BlockLeft--
		return p.Current, p.Pending != "" && p.Pending != p.Current
	}
	plan := BeginSwitch(p.Current, target, ready, total)
	switch plan.State {
	case VoiceActive:
		if plan.Target != "" && plan.Target != p.Current {
			p.Current = plan.Target
			p.Pending = ""
			p.BlockLeft = blockLength
			return p.Current, false
		}
		p.BlockLeft = blockLength
		return p.Current, false
	default:
		p.Pending = target
		p.BlockLeft = blockLength // 继续用当前音色走一个块
		return p.Current, true
	}
}

// HasFingerprint 判断某音色下是否已有同指纹的版本。
//
// 指纹相同代表"合成的文本完全一样"，也就是同一段内容的复用，不是新版本。
func (p Pool) HasFingerprint(voiceID, fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	for _, v := range p.Versions {
		if v.VoiceID == voiceID && v.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}
