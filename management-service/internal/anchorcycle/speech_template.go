package anchorcycle

import (
	"errors"
	"strings"
)

// A template is content returned by a model, not a new source fact. Binding and
// approval receipts are made by the caller, never filled by that model.
type SpeechTemplateBinding struct {
	OwnerKey  string `json:"owner_key"`
	SourceSHA string `json:"source_sha"`
	P0SHA     string `json:"p0_sha"`
	MotherSHA string `json:"mother_sha"`
}
type SpeechTemplateReceipt struct {
	TextSHA     string `json:"text_sha"`
	BindingSHA  string `json:"binding_sha"`
	Accepted    bool   `json:"accepted"`
	RawFeedback string `json:"raw_feedback"`
}
type SpeechTemplateAsset struct {
	GenerationID string                           `json:"generation_id"`
	Binding      SpeechTemplateBinding            `json:"binding"`
	Cards        []AngleHook                      `json:"cards"`
	Raw          string                           `json:"raw"`
	Reviews      map[string]SpeechTemplateReceipt `json:"reviews"`
}

func NewSpeechTemplateAsset(raw, generation string, binding SpeechTemplateBinding) (*SpeechTemplateAsset, error) {
	if generation == "" || binding.OwnerKey == "" || binding.SourceSHA == "" || binding.P0SHA == "" || binding.MotherSHA == "" {
		return nil, errors.New("口播模板的程序绑定不完整")
	}
	parsed := ParseDirectionContent(raw, generation, nil, nil, "")
	cards := DistinctDirectionCards(parsed.Cards)
	if len(cards) == 0 || len(cards) > 3 {
		return nil, errors.New("口播模板内容未完成")
	}
	return &SpeechTemplateAsset{GenerationID: generation, Binding: binding, Cards: cards, Raw: raw, Reviews: map[string]SpeechTemplateReceipt{}}, nil
}

func (a *SpeechTemplateAsset) RecordReview(cardID string, receipt SpeechTemplateReceipt) error {
	for _, card := range a.Cards {
		if card.ID != cardID {
			continue
		}
		if receipt.TextSHA != ProductDigest(card.Action) || receipt.BindingSHA != ProductDigest(a.Binding) {
			return errors.New("模板核对回执不能绑定到其他文字或资料")
		}
		if a.Reviews == nil {
			a.Reviews = map[string]SpeechTemplateReceipt{}
		}
		a.Reviews[cardID] = receipt
		return nil
	}
	return errors.New("模板条目不属于本次程序请求")
}

func (a *SpeechTemplateAsset) Select(binding SpeechTemplateBinding, rotation int) (string, error) {
	if binding != a.Binding {
		return "", errors.New("不能复用其他商品、任务或资料版本的模板")
	}
	usable := []string{}
	for _, c := range a.Cards {
		if r, ok := a.Reviews[c.ID]; ok && r.Accepted && r.TextSHA == ProductDigest(c.Action) && r.BindingSHA == ProductDigest(binding) {
			usable = append(usable, c.Action)
		}
	}
	if len(usable) == 0 {
		return "", errors.New("没有完成核对的口播模板")
	}
	if rotation < 0 {
		rotation = 0
	}
	return usable[rotation%len(usable)], nil
}

// The locked sentence is an explicit output requirement; it never replaces
// P0 or final fact review and cannot prove that the rest of a paragraph is safe.
func SpeechTemplateRetained(text, template string) bool {
	return strings.TrimSpace(template) != "" && strings.Contains(text, template)
}

// The fixed factual clause is composed by the program. This does not accept a
// model fragment as safe: the complete result still requires final review.
func ComposeSpeechTemplate(template, fragment string) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", errors.New("风险事实句未完成")
	}
	return strings.TrimSpace(template) + " " + strings.TrimSpace(fragment), nil
}
