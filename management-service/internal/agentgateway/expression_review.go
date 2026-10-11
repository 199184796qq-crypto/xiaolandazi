package agentgateway

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type ExpressionReview struct {
	Passed      *bool           `json:"passed"`
	Quote       string          `json:"quote"`
	Reason      string          `json:"reason"`
	Suggestions json.RawMessage `json:"suggestions,omitempty"`
}

var ErrExpressionReviewQuote = errors.New("审核引用未对应当前正文")

func ParseExpressionReview(raw, text string) (ExpressionReview, error) {
	var out ExpressionReview
	body := strings.TrimSpace(raw)
	if strings.HasPrefix(body, "```") {
		if at := strings.IndexByte(body, '\n'); at >= 0 && strings.HasSuffix(body, "```") {
			body = strings.TrimSpace(body[at+1 : len(body)-3])
		}
	}
	d := json.NewDecoder(strings.NewReader(body))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF || out.Passed == nil || !*out.Passed && strings.TrimSpace(out.Reason) == "" {
		return out, errors.New("表达约束审核结果未完成")
	}
	if out.Quote != "" && !strings.Contains(text, out.Quote) {
		return out, ErrExpressionReviewQuote
	}
	// Optional advice may be strings or objects. It never changes the verdict.
	return out, nil
}
