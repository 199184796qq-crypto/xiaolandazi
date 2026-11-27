package humanfission

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxNameRunes = 32

func ValidName(name string) bool {
	return strings.TrimSpace(name) != "" && utf8.RuneCountInString(name) <= MaxNameRunes &&
		!strings.ContainsFunc(name, unicode.IsControl)
}

func (p Part) DisplayName(number int) string {
	if ValidName(p.Name) {
		return p.Name
	}
	text := strings.Join(strings.Fields(p.Text), " ")
	text = strings.TrimSpace(strings.SplitN(strings.NewReplacer("，", "。", "？", "。", "！", "。", "；", "。").Replace(text), "。", 2)[0])
	if text == "" {
		return fmt.Sprintf("语音片 %d", number)
	}
	runes := []rune(text)
	return string(runes[:min(len(runes), MaxNameRunes)])
}

func (s State) RecipeName(recipe Recipe, number int) string {
	if ValidName(recipe.Name) {
		return recipe.Name
	}
	return fmt.Sprintf("组合 %d", number)
}

func (s State) NeedsNames() bool {
	for _, part := range s.Parts {
		if !ValidName(part.Name) {
			return true
		}
	}
	for _, recipe := range s.Recipes {
		if !ValidName(recipe.Name) {
			return true
		}
	}
	return false
}

func (s State) RecipeIndex(id string) int {
	for i, recipe := range s.Recipes {
		if recipe.ID == id {
			return i
		}
	}
	return -1
}

func (s State) RecipeVersion() string {
	return Digest([]string{s.Input, s.Generation})
}

type NamedPlan struct {
	Name  string `json:"name"`
	Order []int  `json:"order"`
}

type ExactRequest struct {
	Recipe    string `json:"recipe"`
	Version   string `json:"version"`
	Stamp     string `json:"stamp"`
	RequestID string `json:"request_id"`
}
