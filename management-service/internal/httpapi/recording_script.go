package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// 话术优先模式（原「录音主线」）可以有多份话术，每份是一个独立单元：
// 一个标签头 + 一个标题 + 一段正文，可选绑定一段上传的原始声音。
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
	TabID  string `json:"tab_id"`
	Title  string `json:"title"`
	Order  int    `json:"order"`
	Text   string `json:"text"`
	Pinned bool   `json:"pinned,omitempty"`
	// 一个声音对应一个文本框，不合并；这里保留数组是为了将来能挂多轨字幕，
	// 当前上限 1 段，多传一段就多一个文本框。
	AudioAssetIDs []int64 `json:"audio_asset_ids,omitempty"`
}

type recordingScript struct {
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
	if err := script.validate(); err != nil {
		return err
	}
	script.normalize()
	context["recording_script"] = script
	return nil
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
		if textRunes > recordingScriptMaxTextRunes {
			return fmt.Errorf("第 %d 个文本框的正文须在 %d 字以内", index+1, recordingScriptMaxTextRunes)
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
		if text := strings.TrimSpace(tab.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}
