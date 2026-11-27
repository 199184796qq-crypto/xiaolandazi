package humanfission

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const Rate = 24000
const BatchSize = 10
const RefillAt = 2
const ExhaustedMessage = "已无法生成新的真人话术组合，请添加或更新真人话术。"

type Cue struct {
	Start int64  `json:"start_ms"`
	End   int64  `json:"end_ms"`
	Text  string `json:"text"`
}

type Part struct {
	Name     string      `json:"name,omitempty"`
	Start    int64       `json:"start_ms"`
	End      int64       `json:"end_ms"`
	Duration int64       `json:"duration_ms"`
	Object   string      `json:"object"`
	Text     string      `json:"text"`
	SRT      string      `json:"srt"`
	Words    []TimedWord `json:"words,omitempty"`
	Edited   bool        `json:"edited,omitempty"`
}

type Recipe struct {
	Name     string `json:"name,omitempty"`
	ID       string `json:"id"`
	Order    []int  `json:"order"`
	Duration int64  `json:"duration_ms"`
	Used     bool   `json:"used,omitempty"`
}

type Render struct {
	RecipeID string `json:"recipe_id"`
	Object   string `json:"object"`
	Duration int64  `json:"duration_ms"`
	SRT      string `json:"srt"`
	Stamp    string `json:"stamp"`
}

type State struct {
	Exact       *ExactRequest     `json:"exact,omitempty"`
	Input       string            `json:"input"`
	Generation  string            `json:"generation,omitempty"`
	Split       *SplitRequest     `json:"split,omitempty"`
	Merge       *MergeRequest     `json:"merge,omitempty"`
	Duration    int64             `json:"duration_ms"`
	Parts       []Part            `json:"parts,omitempty"`
	Recipes     []Recipe          `json:"recipes,omitempty"`
	Exhausted   bool              `json:"exhausted,omitempty"`
	ExhaustedAt string            `json:"exhausted_at,omitempty"`
	Last        string            `json:"last,omitempty"`
	Render      *Render           `json:"render,omitempty"`
	Renders     map[string]Render `json:"renders,omitempty"`
}

func Digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func DurationOK(duration, original int64) bool {
	return original > 0 && duration > 0 && duration*10 >= original*4 && duration*10 <= original*13
}

func (s State) ReadyParts() int {
	for _, part := range s.Parts {
		if part.SRT == "" {
			return 0
		}
	}
	return len(s.Parts)
}

func (s State) Unused() int {
	total := 0
	for _, plan := range s.Recipes {
		if !plan.Used {
			total++
		}
	}
	return total
}

func (s State) Choices(last string) []Recipe {
	choices := make([]Recipe, 0, len(s.Recipes))
	for _, wanted := range []bool{false, true} {
		for _, plan := range s.Recipes {
			if plan.Used == wanted && plan.ID != last {
				choices = append(choices, plan)
			}
		}
	}
	for _, plan := range s.Recipes {
		if plan.ID == last {
			choices = append(choices, plan)
			break
		}
	}
	return choices
}

func (s *State) AddPlans(orders [][]int) int {
	plans := make([]NamedPlan, 0, len(orders))
	for _, order := range orders {
		plans = append(plans, NamedPlan{Order: order})
	}
	return s.AddNamedPlans(plans)
}

func (s *State) AddNamedPlans(plans []NamedPlan) int {
	known := make(map[string]bool, len(s.Recipes))
	for _, plan := range s.Recipes {
		known[plan.ID] = true
	}
	added := 0
	for _, plan := range plans {
		order := plan.Order
		if len(order) == 0 || len(order) > 1 && order[0] == order[len(order)-1] {
			continue
		}
		total := int64(0)
		for i, part := range order {
			if part < 1 || part > len(s.Parts) || i > 0 && order[i-1] == part {
				total = 0
				break
			}
			total += s.Parts[part-1].Duration
			if total*10 > s.Duration*13 {
				break
			}
		}
		id := Digest([]any{s.Input, order})
		if known[id] || !DurationOK(total, s.Duration) {
			continue
		}
		s.Recipes = append(s.Recipes, Recipe{Name: plan.Name, ID: id, Order: append([]int(nil), order...), Duration: total})
		known[id] = true
		added++
		if added == BatchSize {
			break
		}
	}
	return added
}

var srtTime = regexp.MustCompile(`^(\d{2}):([0-5]\d):([0-5]\d),(\d{3})$`)

func ReadSRT(raw string) ([]Cue, error) {
	var cues []Cue
	raw = strings.TrimSpace(strings.TrimPrefix(strings.ReplaceAll(raw, "\r\n", "\n"), "\ufeff"))
	for _, block := range strings.Split(raw, "\n\n") {
		lines := strings.SplitN(strings.TrimSpace(block), "\n", 3)
		if len(lines) != 3 {
			return nil, errors.New("录音需要有效的 SRT 时间轴")
		}
		pieces := strings.Split(lines[1], " --> ")
		if len(pieces) != 2 {
			return nil, errors.New("录音需要有效的 SRT 时间轴")
		}
		start, end := parseTime(pieces[0]), parseTime(pieces[1])
		text := strings.TrimSpace(lines[2])
		if start < 0 || end <= start || len(cues) > 0 && cues[len(cues)-1].Start > start || text == "" {
			return nil, errors.New("录音 SRT 时间轴或文本无效")
		}
		cues = append(cues, Cue{Start: start, End: end, Text: text})
	}
	return cues, nil
}

func parseTime(value string) int64 {
	m := srtTime.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return -1
	}
	n := int64(0)
	for i, factor := range []int64{3600000, 60000, 1000, 1} {
		d, _ := strconv.ParseInt(m[i+1], 10, 32)
		n += d * factor
	}
	return n
}

func timecode(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func SRT(cues []Cue) string {
	var b strings.Builder
	for i, cue := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, timecode(cue.Start), timecode(cue.End), cue.Text)
	}
	return b.String()
}

func ReadWAV(raw []byte) ([]int16, error) {
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, errors.New("需要 PCM WAV 音频")
	}
	var format, data []byte
	for at := 12; at+8 <= len(raw); {
		n := int(binary.LittleEndian.Uint32(raw[at+4 : at+8]))
		if n > len(raw)-at-8 {
			return nil, errors.New("WAV 数据不完整")
		}
		switch string(raw[at : at+4]) {
		case "fmt ":
			format = raw[at+8 : at+8+n]
		case "data":
			data = raw[at+8 : at+8+n]
		}
		at += 8 + n + n%2
	}
	if len(format) < 16 || binary.LittleEndian.Uint16(format) != 1 ||
		binary.LittleEndian.Uint16(format[2:]) != 1 || binary.LittleEndian.Uint32(format[4:]) != Rate ||
		binary.LittleEndian.Uint16(format[14:]) != 16 || len(data) < 2 || len(data)%2 != 0 {
		return nil, errors.New("需要 24 kHz 单声道 16 位 PCM")
	}
	pcm := make([]int16, len(data)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return pcm, nil
}

func WAV(pcm []int16) []byte {
	var out bytes.Buffer
	out.Grow(44 + len(pcm)*2)
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(36+len(pcm)*2))
	out.WriteString("WAVEfmt ")
	binary.Write(&out, binary.LittleEndian, uint32(16))
	binary.Write(&out, binary.LittleEndian, [2]uint16{1, 1})
	binary.Write(&out, binary.LittleEndian, [2]uint32{Rate, Rate * 2})
	binary.Write(&out, binary.LittleEndian, [2]uint16{2, 16})
	out.WriteString("data")
	binary.Write(&out, binary.LittleEndian, uint32(len(pcm)*2))
	binary.Write(&out, binary.LittleEndian, pcm)
	return out.Bytes()
}

func MS(samples int) int64 { return (int64(samples)*1000 + Rate/2) / Rate }
func Samples(ms int64) int { return int(ms * Rate / 1000) }

func QuietCuts(pcm []int16) []int {
	const frame = Rate / 100
	rms := make([]float64, len(pcm)/frame)
	var voiced []float64
	for i := range rms {
		sum := 0.0
		for _, sample := range pcm[i*frame : (i+1)*frame] {
			value := float64(sample) / 32768
			sum += value * value
		}
		rms[i] = math.Sqrt(sum / frame)
		if rms[i] > .01 {
			voiced = append(voiced, rms[i])
		}
	}
	if len(voiced) == 0 {
		return nil
	}
	sort.Float64s(voiced)
	threshold := max(.002, min(.010, voiced[int(float64(len(voiced)-1)*.15)]*.30))
	var cuts []int
	start := 0
	for i := 0; i <= len(rms); i++ {
		if i < len(rms) && rms[i] <= threshold {
			continue
		}
		if start > 0 && i < len(rms) && i-start >= 20 {
			position := (start + i) * frame / 2
			safe := true
			for _, value := range pcm[position-Rate/40 : position+Rate/40] {
				if value > 1638 || value < -1638 {
					safe = false
					break
				}
			}
			if safe {
				cuts = append(cuts, position)
			}
		}
		start = i + 1
	}
	return cuts
}

func Partition(pcm []int16, guidance []int64) ([]int, error) {
	if len(pcm) == 0 || len(guidance) > 160 {
		return nil, errors.New("录音或分组坐标无效")
	}
	candidates := QuietCuts(pcm)
	type path struct {
		cost  int64
		index []int
	}
	options := map[int]path{0: {}}
	previous := int64(0)
	for _, ms := range guidance {
		if ms <= previous || ms >= MS(len(pcm)) {
			return nil, errors.New("分组坐标必须位于录音内且严格递增")
		}
		previous = ms
		next := make(map[int]path)
		for _, cut := range candidates {
			gap := MS(cut) - ms
			if gap > 4000 || gap < -4000 || len(pcm)-cut < Samples(400) {
				continue
			}
			for last, old := range options {
				if cut-last < Samples(400) {
					continue
				}
				cost := old.cost + gap*gap
				prior, has := next[cut]
				if !has || cost < prior.cost || cost == prior.cost && slices.Compare(old.index, prior.index[:len(old.index)]) < 0 {
					next[cut] = path{cost: cost, index: append(append([]int(nil), old.index...), cut)}
				}
			}
		}
		if len(next) == 0 {
			return nil, fmt.Errorf("第 %.3f 秒附近找不到符合 200 毫秒停顿的独立切点", float64(ms)/1000)
		}
		options = next
	}
	best := path{cost: math.MaxInt64}
	for _, option := range options {
		if option.cost < best.cost || option.cost == best.cost && slices.Compare(option.index, best.index) < 0 {
			best = option
		}
	}
	return append(append([]int{0}, best.index...), len(pcm)), nil
}
