// Package scriptplay 是话术主线播放的调度内核：标签排序、片段选择、版本选择。
//
// 设计见 docs/2026-11-02-话术组主线播放-开发任务书.md。
// 本包只做纯计算，不碰数据库、音频与网络，方便单测与替换。
package scriptplay

import (
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
	"unicode"
)

// Mode 是方案级的播放模式。
type Mode string

const (
	// ModeFull 全读：一个标签下的片段全部播一遍再切下一个标签。
	ModeFull Mode = "full"
	// ModeSingle 单选：一个标签只播一个片段就切走。
	ModeSingle Mode = "single"
)

// Piece 是一条话术片段。
type Piece struct {
	ID   string
	Text string
}

// Tag 是一个话术标签，也就是一个播放单元。
type Tag struct {
	ID     string
	Title  string
	Fixed  bool
	Pieces []Piece
}

// Plan 是一次播放的全部输入。
type Plan struct {
	Mode           Mode
	Tags           []Tag
	VersionCap     int // 每个音色下的版本上限
	MinVoiceBlock  int // 音色块最少轮数
}

// Fingerprint 计算片段文本的归一化指纹。
//
// 归一化规则：全角转半角、去掉所有空白，再取 hash。
// 所以「这只鸭子 四十五块五」与「这只鸭子四十五块五」指纹相同，
// 用户改个空格不会触发重新生成。
func Fingerprint(text string) string {
	normalized := normalizeText(text)
	h := fnv.New64a()
	_, _ = h.Write([]byte(normalized))
	return formatHex(h.Sum64())
}

func normalizeText(text string) string {
	var b strings.Builder
	for _, r := range text {
		// 全角 ASCII 区间（！到～）映射到半角。
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		if r == 0x3000 { // 全角空格
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func formatHex(v uint64) string {
	const digits = "0123456789abcdef"
	var out [16]byte
	for i := 15; i >= 0; i-- {
		out[i] = digits[v&0xF]
		v >>= 4
	}
	return string(out[:])
}

// OrderTags 返回本轮标签的播放顺序（下标）。
//
// 固定标签锁定在它原本的位置，其余标签在剩下的空位里随机。
// 所有标签都固定时退化为原顺序，不做随机。
// 没有固定标签时整体随机，且不会与上一轮完全相同（轮数大于 1 时）。
func OrderTags(tags []Tag, previous []int, rng *rand.Rand) []int {
	n := len(tags)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if n <= 1 {
		return order
	}
	var free []int
	for i, tag := range tags {
		if tag.Fixed {
			continue
		}
		free = append(free, i)
	}
	if len(free) == 0 {
		return order // 全固定：按配置顺序
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	perm := rng.Perm(len(free))
	for slot, pick := range perm {
		order[free[slot]] = free[pick]
	}
	// 避免与上一轮完全一致：交换两个非固定位即可，仍然是合法排列。
	if len(free) > 1 && sameOrder(order, previous) {
		order[free[0]], order[free[1]] = order[free[1]], order[free[0]]
	}
	return order
}

func sameOrder(a, b []int) bool {
	if len(b) != len(a) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TagState 是单个标签在播放过程中的选择状态。
type TagState struct {
	Used map[string]bool // 单选模式下本标签已用过的片段 ID
}

// NewTagState 创建空状态。
func NewTagState() *TagState {
	return &TagState{Used: map[string]bool{}}
}

// PickPieces 返回进入某个标签时要播的片段下标。
//
// 全读：所有片段随机排序，一次播完。
// 单选：只选一个，优先选没用过的；全部用过则清空重来并从中选一个。
func PickPieces(tag Tag, mode Mode, state *TagState, rng *rand.Rand) []int {
	n := len(tag.Pieces)
	if n == 0 {
		return nil
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	if state == nil {
		state = NewTagState()
	}
	if mode == ModeSingle {
		var fresh []int
		for i, piece := range tag.Pieces {
			if !state.Used[piece.ID] {
				fresh = append(fresh, i)
			}
		}
		if len(fresh) == 0 {
			// 全部用过：重置，从整组重新开始，保证不会永远停在同一处。
			state.Used = map[string]bool{}
			fresh = make([]int, n)
			for i := range fresh {
				fresh[i] = i
			}
		}
		pick := fresh[rng.Intn(len(fresh))]
		state.Used[tag.Pieces[pick].ID] = true
		return []int{pick}
	}
	order := rng.Perm(n)
	return order
}

// VoiceBlockLength 返回一个音色块应该持续多少轮。
//
// 块长取「实际版本数」与「最少轮数」的较大值：
// 还没有改写版本时（只有 1 个版本）也能守住最少轮数。
func VoiceBlockLength(versions, minVoiceBlock int) int {
	min := minVoiceBlock
	if min < 1 {
		min = 1
	}
	if versions < min {
		return min
	}
	return versions
}

// PickVersion 从某个音色下的版本池里取一个，取过的本轮不再取。
//
// 返回下标；池子用尽时返回 -1，调用方据此切换音色块。
func PickVersion(remaining []int, rng *rand.Rand) int {
	if len(remaining) == 0 {
		return -1
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	pos := rng.Intn(len(remaining))
	return remaining[pos]
}

// ShuffleVersions 在一次音色块开始时打乱版本顺序。
func ShuffleVersions(versions []int, rng *rand.Rand) []int {
	out := append([]int(nil), versions...)
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// VersionCap 返回允许的版本上限，未配置时用默认值 6。
func VersionCap(configured int) int {
	if configured <= 0 {
		return 6
	}
	if configured > 50 {
		return 50
	}
	return configured
}

// CanGenerate 判断是否还应该为某片段某音色继续生成新版本。
func CanGenerate(current int, cap int) bool {
	return current < VersionCap(cap)
}

// SortStableByPosition 按 position 稳定排序，供读取时对齐界面顺序。
func SortStableByPosition(positions []int, ids []string) []string {
	idx := make([]int, len(ids))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return positions[idx[a]] < positions[idx[b]] })
	out := make([]string, len(ids))
	for i, p := range idx {
		out[i] = ids[p]
	}
	return out
}
