package strategycenter

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Read loads each request's current project mother. Explicit directories never
// silently fall back to another installation's templates.
func readInteractionMother(name string) ([]byte, error) {
	paths := []string{}
	if dir := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_PROMPT_DIR")); dir != "" {
		paths = append(paths, filepath.Join(dir, name))
	} else {
		cwd, _ := os.Getwd()
		for i := 0; i < 8 && cwd != ""; i++ {
			paths = append(paths, filepath.Join(cwd, "configs", "prompts", "anchor-style", name))
			parent := filepath.Dir(cwd)
			if parent == cwd {
				break
			}
			cwd = parent
		}
		exe, _ := os.Executable()
		paths = append(paths, filepath.Join(filepath.Dir(exe), "..", "configs", "prompts", "anchor-style", name))
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 || len(raw) > 256*1024 {
			return nil, fmt.Errorf("互动母版长度错误: %s", p)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("缺少互动母版 %s", name)
}
func LoadInteractionMother() (InteractionSchedule, error) {
	raw, err := readInteractionMother("interaction-scheduling.json")
	if err != nil {
		return InteractionSchedule{}, err
	}
	s, err := Decode(raw)
	if err != nil {
		return s, err
	}
	s.TemplateSHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	return s, nil
}
func ValidateInteractionSchedule(s InteractionSchedule) error {
	if (s.TemplateVersion != 1 && s.TemplateVersion != 2) || s.Name == "" || s.MaxRetries < 0 || s.MaxRetries > 5 {
		return fmt.Errorf("互动配置版本或重试次数错误")
	}
	for _, k := range []string{"less", "natural", "more"} {
		if s.AnswerLimits[k] < 1 || s.AnswerLimits[k] > 20 {
			return fmt.Errorf("回答频率上限错误: %s", k)
		}
	}
	if s.AnswerLimits["less"] > s.AnswerLimits["natural"] || s.AnswerLimits["natural"] > s.AnswerLimits["more"] {
		return fmt.Errorf("回答频率应由少到多递增")
	}
	for _, k := range []string{"complaint", "product_question", "order", "warmup", "welcome", "like_follow", "smalltalk"} {
		e, ok := s.Events[k]
		if !ok || e.Name == "" || e.Weight < 0 || e.Weight > 100 || e.TTLSeconds < 1 || e.TTLSeconds > 600 || e.CooldownSeconds < 0 || e.CooldownSeconds > 600 || e.MaxWaitSeconds < 0 || e.MaxWaitSeconds > 600 || e.BatchThreshold < 1 || e.BatchThreshold > 1000 {
			return fmt.Errorf("互动事件配置错误: %s", k)
		}
		if e.Priority != "P0" && e.Priority != "P1" && e.Priority != "P2" && e.Priority != "P3" {
			return fmt.Errorf("互动优先级错误: %s", k)
		}
		if len(e.Goals) == 0 {
			return fmt.Errorf("缺少互动目的: %s", k)
		}
		seen := map[string]bool{}
		for _, g := range e.Goals {
			if g.Key == "" || seen[g.Key] || g.Name == "" || g.Action == "" || len(g.Angles) == 0 {
				return fmt.Errorf("互动目的配置错误: %s", k)
			}
			seen[g.Key] = true
			for _, h := range g.Heats {
				if s.HeatLabels[h] == "" {
					return fmt.Errorf("未知热度: %s", h)
				}
			}
			for _, stage := range g.Stages {
				if s.StageLabels[stage] == "" {
					return fmt.Errorf("未知主线阶段: %s", stage)
				}
			}
		}
	}
	for key, t := range s.TimingOverrides {
		if key != "reply_like" && key != "reply_follow" && key != "welcome_named" && key != "welcome_batch" {
			return fmt.Errorf("未知子事件: %s", key)
		}
		if t.Name == "" || t.MaxWaitSeconds < 0 || t.MaxWaitSeconds > 600 || t.TTLSeconds < 1 || t.TTLSeconds > 600 || t.BatchThreshold < 1 || t.BatchThreshold > 1000 {
			return fmt.Errorf("子事件聚合规则错误: %s", key)
		}
	}
	for _, key := range []string{"name", "version", "plan_id", "template_version", "template_sha256", "field_labels", "priority_labels", "heat_labels", "stage_labels", "answer_limits", "max_retries", "events", "enabled", "priority", "weight", "cooldown_seconds", "ttl_seconds", "max_wait_seconds", "batch_threshold", "goals", "key", "action", "heats", "stages", "angles", "timing_overrides"} {
		if strings.TrimSpace(s.FieldLabels[key]) == "" {
			return fmt.Errorf("缺少字段中文名称: %s", key)
		}
	}
	for _, key := range []string{"P0", "P1", "P2", "P3"} {
		if s.PriorityLabels[key] == "" {
			return fmt.Errorf("缺少优先级中文名称: %s", key)
		}
	}
	if len(s.Events) != 7 {
		return fmt.Errorf("存在未知互动事件")
	}
	return nil
}

func Decode(raw []byte) (InteractionSchedule, error) {
	var err error
	var s InteractionSchedule
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&s); err != nil {
		return s, err
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return s, fmt.Errorf("互动母版包含多余内容")
	}
	if err = ValidateInteractionSchedule(s); err != nil {
		return s, err
	}
	return s, nil
}
