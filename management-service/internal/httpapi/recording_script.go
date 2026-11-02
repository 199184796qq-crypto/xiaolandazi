package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 话术优先第2版：标签是组名，组内保存多条独立话术；每条400–4000字符，
// 一组合计最多4000字符。旧版单框及上传原音的正文/声音绑定继续兼容读取。
//
// 只有一个文本框时就是 N=1 的退化情况，和改造前完全等价；
// 旧方案快照没有 recording_script 字段时按单框读，不要求重新保存。
const (
	recordingScriptMaxTabs        = 20
	recordingScriptMaxTitleRunes  = 30
	recordingScriptMaxTextRunes   = 4000
	recordingScriptMaxTotalRunes  = 80000
	recordingScriptMaxAudioPerTab = 1
)

var recordingScriptTabIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

type recordingScriptTab struct {
	TabID   string                 `json:"tab_id"`
	Title   string                 `json:"title"`
	Order   int                    `json:"order"`
	Text    string                 `json:"text,omitempty"` // Legacy/original-audio transcript only.
	Pinned  bool                   `json:"pinned,omitempty"`
	Entries []recordingScriptEntry `json:"entries,omitempty"`
	// 一个声音对应一个文本框，不合并；这里保留数组是为了将来能挂多轨字幕，
	// 当前上限 1 段，多传一段就多一个文本框。
	AudioAssetIDs []int64 `json:"audio_asset_ids,omitempty"`
}

type recordingScriptEntry struct {
	EntryID string `json:"entry_id"`
	Text    string `json:"text"`
}

func (t recordingScriptTab) texts() []string {
	if t.Entries != nil {
		texts := make([]string, len(t.Entries))
		for i, entry := range t.Entries {
			texts[i] = entry.Text
		}
		return texts
	}
	return []string{t.Text}
}

type recordingScript struct {
	Version       int                  `json:"version,omitempty"`
	Tabs          []recordingScriptTab `json:"tabs"`
	ActiveTabID   string               `json:"active_tab_id,omitempty"`
	PlaybackOrder []string             `json:"playback_order,omitempty"`
}

// normalizeRecordingScript 校验并规整调用方提交的话术组，写回 generation_context。
// 没有该字段时什么都不做（旧方案照常保存）。
func normalizeRecordingScript(context map[string]any) error {
	value, exists := context["recording_script"]
	if !exists || value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return errors.New("话术分组格式错误")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var script recordingScript
	if err := decoder.Decode(&script); err != nil {
		return errors.New("话术分组格式错误，只接受标签、标题、正文与绑定声音")
	}
	// Compact only newly submitted independent scripts. Historical reads and
	// recording transcripts keep their original text and paid-cache identity.
	if script.Version == 2 {
		for ti := range script.Tabs {
			for ei := range script.Tabs[ti].Entries {
				entry := &script.Tabs[ti].Entries[ei]
				entry.Text = singleParagraphRecordingScriptEntry(entry.Text)
			}
		}
	}
	if err := script.validate(); err != nil {
		return err
	}
	script.normalize()
	context["recording_script"] = script
	return nil
}

func singleParagraphRecordingScriptEntry(text string) string {
	lines := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\r' || r == '\n' || r == '\v' || r == '\f' || r == '\u0085' || r == '\u2028' || r == '\u2029'
	})
	var result strings.Builder
	for _, line := range lines {
		result.WriteString(strings.TrimSpace(line))
	}
	return result.String()
}

// recordingScriptFromContext 读取话术组；缺失或损坏时返回 false，由调用方按单框处理。
func recordingScriptFromContext(context map[string]any) (recordingScript, bool) {
	value, exists := context["recording_script"]
	if !exists || value == nil {
		return recordingScript{}, false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return recordingScript{}, false
	}
	var script recordingScript
	if err := json.Unmarshal(raw, &script); err != nil {
		return recordingScript{}, false
	}
	if err := script.validate(); err != nil {
		return recordingScript{}, false
	}
	script.normalize()
	return script, true
}

func (s recordingScript) validate() error {
	if s.Version != 0 && s.Version != 2 {
		return errors.New("话术组版本不支持")
	}
	if len(s.Tabs) == 0 {
		return errors.New("话术分组至少要有一个文本框")
	}
	if len(s.Tabs) > recordingScriptMaxTabs {
		return fmt.Errorf("话术分组最多 %d 个文本框", recordingScriptMaxTabs)
	}
	seen := make(map[string]struct{}, len(s.Tabs))
	total := 0
	for index, tab := range s.Tabs {
		id := strings.TrimSpace(tab.TabID)
		if id == "" {
			id = fmt.Sprintf("t%d", index+1)
		}
		if !recordingScriptTabIDPattern.MatchString(id) {
			return fmt.Errorf("第 %d 个文本框的标签非法", index+1)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("第 %d 个文本框的标签重复", index+1)
		}
		seen[id] = struct{}{}
		if len([]rune(strings.TrimSpace(tab.Title))) > recordingScriptMaxTitleRunes {
			return fmt.Errorf("第 %d 个文本框的标题须在 %d 字以内", index+1, recordingScriptMaxTitleRunes)
		}
		textRunes := len([]rune(strings.TrimSpace(tab.Text)))
		if s.Version == 2 {
			if tab.Text != "" {
				return fmt.Errorf("第%d个话术组须单独保存各条话术，不能同时提交旧正文", index+1)
			}
			if len(tab.Entries) == 0 || len(tab.Entries) > 10 {
				return fmt.Errorf("第%d个话术组须有1到10条独立话术", index+1)
			}
			entryIDs := make(map[string]bool, len(tab.Entries))
			textRunes = 0
			for i, entry := range tab.Entries {
				if !recordingScriptTabIDPattern.MatchString(entry.EntryID) || entryIDs[entry.EntryID] {
					return fmt.Errorf("第%d组第%d条话术标识无效或重复", index+1, i+1)
				}
				entryIDs[entry.EntryID] = true
				n := utf8.RuneCountInString(entry.Text)
				if strings.TrimSpace(entry.Text) == "" || n < 400 || n > 4000 {
					return fmt.Errorf("第%d组第%d条话术为%d字符，每条须为400到4000字符", index+1, i+1, n)
				}
				textRunes += n
			}
			if textRunes > 4000 {
				return fmt.Errorf("第%d个话术组共%d字符，所有话术合计不能超过4000字符", index+1, textRunes)
			}
		} else if tab.Entries != nil {
			return errors.New("独立话术须使用第2版话术组格式")
		}
		// A long uploaded recording still maps to one tab/one audio asset. Keep
		// the existing total cap; do not truncate ASR or split its audio binding.
		maxTextRunes := recordingScriptMaxTextRunes
		if len(tab.AudioAssetIDs) > 0 {
			maxTextRunes = recordingScriptMaxTotalRunes
		}
		if textRunes > maxTextRunes {
			return fmt.Errorf("第 %d 个文本框的正文须在 %d 字以内", index+1, maxTextRunes)
		}
		total += textRunes
		if len(tab.AudioAssetIDs) > recordingScriptMaxAudioPerTab {
			return fmt.Errorf("第 %d 个文本框只能绑定一段原始声音", index+1)
		}
		audioSeen := make(map[int64]struct{}, len(tab.AudioAssetIDs))
		for _, assetID := range tab.AudioAssetIDs {
			if assetID <= 0 {
				return fmt.Errorf("第 %d 个文本框绑定的声音非法", index+1)
			}
			if _, dup := audioSeen[assetID]; dup {
				return fmt.Errorf("第 %d 个文本框重复绑定同一段声音", index+1)
			}
			audioSeen[assetID] = struct{}{}
		}
	}
	if total > recordingScriptMaxTotalRunes {
		return fmt.Errorf("话术总字数须在 %d 字以内", recordingScriptMaxTotalRunes)
	}
	if active := strings.TrimSpace(s.ActiveTabID); active != "" {
		if _, ok := seen[active]; !ok {
			return errors.New("激活的文本框不存在")
		}
	}
	if len(s.PlaybackOrder) > 0 {
		if len(s.PlaybackOrder) != len(s.Tabs) {
			return errors.New("播放顺序必须包含全部文本框")
		}
		orderSeen := make(map[string]struct{}, len(s.PlaybackOrder))
		for _, id := range s.PlaybackOrder {
			id = strings.TrimSpace(id)
			if _, ok := seen[id]; !ok {
				return errors.New("播放顺序里有不存在的文本框")
			}
			if _, dup := orderSeen[id]; dup {
				return errors.New("播放顺序里有重复的文本框")
			}
			orderSeen[id] = struct{}{}
		}
	}
	return nil
}

// normalize 补齐缺省值：标签、顺序、激活动作。就地修改。
func (s *recordingScript) normalize() {
	for index := range s.Tabs {
		tab := &s.Tabs[index]
		tab.TabID = strings.TrimSpace(tab.TabID)
		if tab.TabID == "" {
			tab.TabID = fmt.Sprintf("t%d", index+1)
		}
		tab.Title = strings.TrimSpace(tab.Title)
		tab.Text = strings.TrimSpace(tab.Text)
		tab.Order = index
	}
	if strings.TrimSpace(s.ActiveTabID) == "" && len(s.Tabs) > 0 {
		s.ActiveTabID = s.Tabs[0].TabID
	}
	s.ActiveTabID = strings.TrimSpace(s.ActiveTabID)
	for index := range s.PlaybackOrder {
		s.PlaybackOrder[index] = strings.TrimSpace(s.PlaybackOrder[index])
	}
}

// scriptedText 把全部文本框按播放顺序拼成一份正文，用于当前的单轨播放路径。
// 没有指定顺序时按标签顺序拼接；空文本框跳过。
func (s recordingScript) scriptedText() string {
	if len(s.Tabs) == 0 {
		return ""
	}
	byID := make(map[string]recordingScriptTab, len(s.Tabs))
	for _, tab := range s.Tabs {
		byID[tab.TabID] = tab
	}
	ordered := make([]recordingScriptTab, 0, len(s.Tabs))
	if len(s.PlaybackOrder) > 0 {
		for _, id := range s.PlaybackOrder {
			if tab, ok := byID[id]; ok {
				ordered = append(ordered, tab)
			}
		}
	} else {
		ordered = append(ordered, s.Tabs...)
	}
	parts := make([]string, 0, len(ordered))
	for _, tab := range ordered {
		for _, text := range tab.texts() {
			if text = strings.TrimSpace(text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
