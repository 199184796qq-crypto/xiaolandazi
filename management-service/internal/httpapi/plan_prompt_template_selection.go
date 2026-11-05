package httpapi

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

type planPromptSelectedTemplate struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Content         string `json:"content"`
	RenderedContent string `json:"rendered_content"`
	RenderError     string `json:"render_error,omitempty"`
}

// Reuse the existing built-in value sources, never the legacy render/generation
// handler: that handler removes unresolved lines and may invoke a model.
func (s *Server) planPromptSelectionValues(ctx context.Context, tenantID int64, plan model.LiveAgentPlan) (map[string]string, error) {
	profile, err := s.store.GetPlanBusinessProfile(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, err
	}
	benefits, err := s.store.ListLiveAgentPlanBenefits(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, err
	}
	facts, err := s.store.ListLiveAgentPlanFacts(ctx, tenantID, plan.ID)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(planPromptDefaults)+12)
	for key, value := range planPromptDefaults {
		values[key] = value
	}
	for key, value := range planPromptWriterValues(plan, profile, links, benefits, facts, nil) {
		if key != "现有提示词" {
			values[key] = value
		}
	}
	values["直播间名"] = plan.Name
	values["商品"] = values["商品卡"]
	values["福利"] = values["活动福利"]
	// An absent business profile is not evidence for a generic business/track.
	if strings.TrimSpace(profile.BizDomain) == "" {
		delete(values, "业务模式")
	}
	if strings.TrimSpace(profile.EcommerceIndustry) == "" && strings.TrimSpace(profile.LocalServiceIndustry) == "" && strings.TrimSpace(profile.BizDomain) == "" {
		delete(values, "直播赛道")
		delete(values, "本行业事实类")
	}
	if track := values["直播赛道"]; track != "" {
		values["赛道"] = track
	} else {
		delete(values, "赛道")
	}
	return values, nil
}

var selectedPromptSlot = regexp.MustCompile(`\{\{([^{}\r\n]*)\}\}`)

// Single pass over the ORIGINAL text. Replacement data is literal and is never
// reinterpreted as another placeholder, block, regular expression or HTML.
// Missing/blank values and runtime/unknown slots retain their original bytes.
func fillSelectedPlanPrompt(original string, values map[string]string) (string, error) {
	const maxBytes = 60000
	tooLong := errors.New("替换后的模板超过20000字或60000字节，请精简模板或方案资料")
	var out strings.Builder
	appendText := func(text string) bool {
		if out.Len()+len(text) > maxBytes {
			return false
		}
		out.WriteString(text)
		return true
	}
	tokens := selectedPromptSlot.FindAllStringSubmatchIndex(original, -1)
	position := 0
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !appendText(original[position:token[0]]) {
			return "", tooLong
		}
		key := strings.TrimSpace(original[token[2]:token[3]])
		end := token[1]
		text := original[token[0]:end]
		if strings.HasPrefix(key, "#") {
			// Preserve unavailable/unknown blocks as a whole, including their body.
			stack := []string{strings.TrimSpace(key[1:])}
			for j := i + 1; j < len(tokens); j++ {
				next := strings.TrimSpace(original[tokens[j][2]:tokens[j][3]])
				if strings.HasPrefix(next, "#") {
					stack = append(stack, strings.TrimSpace(next[1:]))
				} else if strings.HasPrefix(next, "/") {
					if len(stack) == 0 || strings.TrimSpace(next[1:]) != stack[len(stack)-1] {
						break
					}
					stack = stack[:len(stack)-1]
					if len(stack) == 0 {
						end = tokens[j][1]
						text = original[token[0]:end]
						block := strings.TrimSpace(key[1:])
						if (block == "商品" || block == "福利") && strings.TrimSpace(values[block]) != "" {
							text = values[block]
						}
						i = j
						break
					}
				}
			}
		} else if !strings.HasPrefix(key, "/") {
			if value, ok := values[key]; ok && strings.TrimSpace(value) != "" {
				text = value
			}
		}
		if !appendText(text) {
			return "", tooLong
		}
		position = end
	}
	if !appendText(original[position:]) || utf8.RuneCountInString(out.String()) > 20000 {
		return "", tooLong
	}
	return out.String(), nil
}
