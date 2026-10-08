package httpapi

import (
	"encoding/json"
	"regexp"
	"strings"

	"livecompanion/management/internal/anchorcycle"
)

func loadOpeningHookMother() (string, string, error) {
	return loadReplaceablePromptAsset("ANCHOR_STYLE_OPENING_HOOK_LIBRARY_PATH", "configs/prompts/anchor-style/opening-hook-library.md", "开场钩子库")
}

func renderAnchorOpeningHook(state *anchorcycle.State) (string, error) {
	if state.Phase != "opening" {
		return "", nil
	}
	md, source := state.OpeningHookLibraryMD, state.OpeningHookLibrarySource
	if md == "" {
		var err error
		md, source, err = loadOpeningHookMother()
		if err != nil {
			return "", err
		}
	}
	library, err := anchorcycle.ParseOpeningHooks(md)
	if err != nil {
		return "", err
	}
	if err = state.SelectOpeningHook(library, openingHookSlotValues(state, library), source); err != nil {
		return "", err
	}
	raw, err := json.Marshal(state.PendingOpeningHook)
	return string(raw), err
}

// Slot names/mappings come from the mother or confirmed plan asset. Code only
// adapts formal rows and current-task fields, never sample/style facts.
func openingHookSlotValues(state *anchorcycle.State, library *anchorcycle.OpeningHookLibrary) map[string]string {
	task := state.Task()
	taskFields := map[string]string{"selling_point": task.SellingPoint, "angle": task.Angle, "target_audience": task.TargetAudience, "pain_point": task.PainPoint}
	values := map[string]string{}
	for key, binding := range library.Bindings {
		value := ""
		if binding.Source == "当前卖点" {
			value = taskFields[binding.Field]
		} else {
			for _, line := range strings.Split(state.Facts, "\n") {
				line = strings.TrimSpace(line)
				end := strings.Index(line, "]")
				if !strings.HasPrefix(line, "- [") || end < 4 {
					continue
				}
				label := line[3:end]
				body := strings.TrimSpace(line[end+1:])
				if cut := strings.Index(body, "；安全改写="); cut >= 0 {
					body = body[:cut]
				}
				if cut := strings.Index(body, "；禁说="); cut >= 0 {
					body = body[:cut]
				}
				if binding.Source == "事实条目" {
					category, factKey, _ := strings.Cut(label, "/")
					if category == binding.Category && (binding.Key == "" || strings.Contains(factKey, binding.Key)) {
						value = body
						break
					}
				} else if binding.Source == "有效福利" && label == "有效福利" {
					fields := map[string]string{}
					for _, field := range strings.Split(body, "；") {
						name, val, ok := strings.Cut(field, "=")
						if ok {
							fields[strings.TrimSpace(name)] = strings.TrimSpace(val)
						}
					}
					if fields[binding.Field] != "" && fields[binding.Field] != "未设置" {
						value = fields[binding.Field]
						if binding.Field == "活动" && fields["链接"] != "" {
							value = fields["链接"] + "：" + value
						}
						break
					}
				}
			}
		}
		if value != "" && binding.Pattern != "" {
			matches := regexp.MustCompile(binding.Pattern).FindStringSubmatch(value)
			value = ""
			if len(matches) == 2 {
				value = matches[1]
			}
		}
		values[key] = strings.TrimSpace(value)
	}
	return values
}
